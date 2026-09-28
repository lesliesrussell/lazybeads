// lb-4gm.3
package beads

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ServeStartTimeout bounds how long LazyBeads waits for a spawned `bd serve`
// to print its address and answer a readiness probe.
var ServeStartTimeout = 15 * time.Second

// serveStopGrace is how long a spawned server gets to shut down after SIGTERM.
const serveStopGrace = 3 * time.Second

// WorkspaceContext is the subset of `bd context --json` LazyBeads uses to
// decide whether `bd serve` can run.
type WorkspaceContext struct {
	DoltMode  string `json:"dolt_mode"`
	Database  string `json:"database"`
	ProjectID string `json:"project_id"`
	RepoRoot  string `json:"repo_root"`
	BeadsDir  string `json:"beads_dir"`
}

// ServesHTTP reports whether this workspace's storage mode lets `bd serve`
// run. Embedded Dolt is refused by bd 1.3.0.
func (w WorkspaceContext) ServesHTTP() bool {
	switch w.DoltMode {
	case "server", "proxied-server":
		return true
	}
	return false
}

// WorkspaceContext asks bd how this workspace stores its data.
func (c *CLI) WorkspaceContext(ctx context.Context, scope Scope) (WorkspaceContext, error) {
	out, err := c.jsonCall(ctx, "context", scope, "context")
	if err != nil {
		return WorkspaceContext{}, err
	}
	var wc WorkspaceContext
	data := trimJSON(out)
	if err := json.Unmarshal(data, &wc); err != nil {
		return WorkspaceContext{}, decodeErr(err, data)
	}
	return wc, nil
}

// ServeProcess is a `bd serve` LazyBeads started and must stop.
type ServeProcess struct {
	URL  string
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

// StartServe launches `bd serve` on an ephemeral loopback port for scope and
// waits until it answers a real query. The token file, if any, is passed by
// path; a credential never appears in argv.
func (r *Runner) StartServe(ctx context.Context, scope Scope, tokenFile string) (*ServeProcess, error) {
	bin, err := r.Resolve()
	if err != nil {
		return nil, err
	}
	args := append(scopeArgs(scope), "serve", "--addr", "127.0.0.1:0")
	if tokenFile != "" {
		args = append(args, "--auth-token-file", tokenFile)
	}
	// The server outlives this call, so it is not bound to ctx; Stop ends it.
	cmd := exec.Command(bin, args...)
	cmd.Dir = scope.Project
	cmd.Env = commandEnv(scope)
	cmd.Stdin = nil
	// The server shares LazyBeads' process group on purpose: a terminal's
	// Ctrl-C or hangup then reaches it too, and bd shuts down cleanly on
	// SIGINT, SIGTERM and SIGHUP. Only a LazyBeads killed outright (SIGKILL,
	// out of memory) can leave it running.
	// A child of bd serve that inherits stderr must not hold Wait open.
	cmd.WaitDelay = serveStopGrace
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &CommandError{Kind: ErrBDExecution, Operation: "start bd serve", Cause: err}
	}
	stderr := &lockedBuffer{b: boundedBuffer{limit: MaxStderrCapture}}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, &CommandError{Kind: ErrBDExecution, Operation: "start bd serve", Args: redactArgs(args), Cause: err}
	}
	p := &ServeProcess{cmd: cmd, done: make(chan struct{})}
	addr := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sent := false
		for sc.Scan() {
			if u, ok := listeningURL(sc.Text()); ok && !sent {
				addr <- u
				sent = true
			}
		}
		// Keep draining (past an over-long line, too) so the server never
		// blocks on a full pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()

	wait, cancel := context.WithTimeout(ctx, ServeStartTimeout)
	defer cancel()
	fail := func(cause error) (*ServeProcess, error) {
		p.Stop()
		// A workspace or schema problem keeps its own kind; anything else
		// means "no server", and the caller carries on with the CLI.
		text := stderr.String()
		kind := classifyStderr(text, 1, nil)
		if kind != ErrSchemaMismatch && kind != ErrWorkspaceNotFound {
			kind = ErrUnavailable
		}
		return nil, &CommandError{Kind: kind, Operation: "start bd serve", Args: redactArgs(args),
			Stderr: text, Cause: cause}
	}
	select {
	case u := <-addr:
		p.URL = u
	case <-p.done:
		return fail(errors.New("bd serve exited before it was listening"))
	case <-wait.Done():
		return fail(fmt.Errorf("bd serve did not report an address within %s", ServeStartTimeout))
	}
	if err := WaitServeReady(wait, p.URL, tokenFile); err != nil {
		return fail(err)
	}
	return p, nil
}

// listeningURL extracts the address from bd serve's
// "bd serve: listening on http://127.0.0.1:NNNN" line.
func listeningURL(line string) (string, bool) {
	_, rest, ok := strings.Cut(line, "listening on ")
	if !ok {
		return "", false
	}
	u := strings.TrimSpace(rest)
	if i := strings.IndexByte(u, ' '); i >= 0 {
		u = u[:i]
	}
	if !IsLoopbackURL(u) {
		return "", false
	}
	return u, true
}

// Stop ends the server: SIGTERM, a short grace period, then SIGKILL. It is
// safe to call more than once.
func (p *ServeProcess) Stop() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.cmd == nil || p.cmd.Process == nil {
			return
		}
		terminate(p.cmd.Process)
		select {
		case <-p.done:
		case <-time.After(serveStopGrace):
			_ = p.cmd.Process.Kill()
			select {
			case <-p.done:
			case <-time.After(2 * serveStopGrace):
				// WaitDelay should have released Wait by now; never hang
				// the session's exit on it.
			}
		}
	})
}

// Done is closed when the server process exits.
func (p *ServeProcess) Done() <-chan struct{} { return p.done }

// WaitServeReady polls the readiness probe bd documents — a real query, not
// /healthz, which stays green while the database is unreachable.
func WaitServeReady(ctx context.Context, baseURL, tokenFile string) error {
	token, err := ReadTokenFile(tokenFile)
	if err != nil {
		return err
	}
	probe := strings.TrimRight(baseURL, "/") + "/v0/beads/ready?limit=1"
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("readiness probe answered %d", resp.StatusCode)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bd serve is not ready: %w", last)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// IsLoopbackURL reports whether u is an http URL on a loopback host.
// LazyBeads only talks to a `bd serve` on this machine: a project config file
// must not be able to point it at someone else's server.
func IsLoopbackURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ReadTokenFile returns the first non-empty line of a bd serve token file.
// An empty path means no token.
func ReadTokenFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", &CommandError{Kind: ErrValidation, Operation: "read bd serve token file", Cause: err}
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if t := strings.TrimSpace(string(line)); t != "" {
			return t, nil
		}
	}
	return "", &CommandError{Kind: ErrValidation, Operation: "read bd serve token file",
		Cause: fmt.Errorf("%s holds no token", path)}
}

// lockedBuffer lets the reader see stderr while the process still writes it.
type lockedBuffer struct {
	mu sync.Mutex
	b  boundedBuffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.b.Bytes())
}
