// lb-1td
package beads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Bounds from the specification's safety section.
const (
	DefaultTimeout   = 30 * time.Second
	MaxStdoutCapture = 10 << 20 // 10 MiB
	MaxStderrCapture = 2 << 20  // 2 MiB
)

// Scope selects which workspace a command targets. An empty Scope means "let
// bd resolve from the working directory", which is the documented default.
type Scope struct {
	Project  string
	BeadsDir string
	Rig      string
	Timeout  time.Duration
}

// Trace records one bd invocation for --debug output.
type Trace struct {
	Args     []string
	Dir      string
	Env      []string
	Duration time.Duration
	ExitCode int
	Err      error
}

// Runner executes the bd binary. All execution is argv-based: no string is ever
// handed to a shell interpreter.
type Runner struct {
	Binary string
	// Debug enables trace collection. Traces redact environment values.
	Debug bool

	mu     sync.Mutex
	traces []Trace
}

// NewRunner returns a runner for the given binary name or path.
func NewRunner(binary string) *Runner {
	if binary == "" {
		binary = "bd"
	}
	return &Runner{Binary: binary}
}

// Result is the captured outcome of a bd invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
	Args     []string
}

// Resolve locates the bd binary, returning a typed error when it is missing.
func (r *Runner) Resolve() (string, error) {
	path, err := exec.LookPath(r.Binary)
	if err != nil {
		return "", &CommandError{
			Kind:      ErrBDBinaryMissing,
			Operation: "locate bd",
			Cause:     err,
		}
	}
	return path, nil
}

// Traces returns a copy of the collected execution traces.
func (r *Runner) Traces() []Trace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Trace(nil), r.traces...)
}

// Run executes bd with the supplied argv under scope, capturing stdout and
// stderr separately and bounding both.
//
// operation names the logical action for error reporting. args must already be
// split into individual argv elements; this function never performs word
// splitting or interpolation.
func (r *Runner) Run(ctx context.Context, operation string, scope Scope, args ...string) (*Result, error) {
	bin, err := r.Resolve()
	if err != nil {
		return nil, err
	}

	full := append(scopeArgs(scope), args...)

	timeout := scope.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, full...)
	cmd.Dir = scope.Project
	cmd.Env = commandEnv(scope)
	// bd prompts on a TTY for some operations; a closed stdin guarantees the
	// non-interactive path and prevents a hung subprocess.
	cmd.Stdin = nil

	var stdout, stderr boundedBuffer
	stdout.limit = MaxStdoutCapture
	stderr.limit = MaxStderrCapture
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	started := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(started)

	res := &Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: elapsed,
		Args:     full,
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
	}

	if r.Debug {
		r.mu.Lock()
		r.traces = append(r.traces, Trace{
			Args:     full,
			Dir:      cmd.Dir,
			Env:      redactEnv(cmd.Env),
			Duration: elapsed,
			ExitCode: res.ExitCode,
			Err:      runErr,
		})
		r.mu.Unlock()
	}

	if runErr == nil {
		return res, nil
	}

	ctxErr := runCtx.Err()
	// An exec failure with no exit status means the binary itself could not run.
	kind := ErrBDExecution
	if exitErr != nil || ctxErr != nil {
		kind = classifyStderr(string(res.Stderr), res.ExitCode, ctxErr)
	}

	return res, &CommandError{
		Kind:      kind,
		Operation: operation,
		Args:      redactArgs(full),
		ExitCode:  res.ExitCode,
		Stdout:    truncateForError(string(res.Stdout)),
		Stderr:    truncateForError(string(res.Stderr)),
		Cause:     runErr,
	}
}

// scopeArgs converts a scope into bd global flags. --directory is bd's
// documented equivalent of `git -C`.
func scopeArgs(scope Scope) []string {
	var args []string
	if scope.Project != "" {
		args = append(args, "--directory", scope.Project)
	}
	return args
}

// commandEnv builds the child environment. BEADS_DIR is set only when the user
// explicitly selected a workspace, so bd's own discovery is not overridden by
// accident.
func commandEnv(scope Scope) []string {
	env := os.Environ()
	if scope.BeadsDir != "" {
		env = append(env, "BEADS_DIR="+scope.BeadsDir)
	}
	if scope.Rig != "" {
		env = append(env, "BEADS_RIG="+scope.Rig)
	}
	// bd suppresses interactive wizards when it believes it is non-interactive.
	env = append(env, "BD_NON_INTERACTIVE=1")
	return env
}

// secretEnvMarkers identify environment variables whose values must never be
// written to a trace or an error.
var secretEnvMarkers = []string{"PASSWORD", "TOKEN", "SECRET", "KEY", "CREDENTIAL", "AUTH"}

// redactEnv keeps variable names but replaces sensitive values.
func redactEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		upper := strings.ToUpper(name)
		redact := false
		for _, marker := range secretEnvMarkers {
			if strings.Contains(upper, marker) {
				redact = true
				break
			}
		}
		if redact {
			out = append(out, name+"=<redacted>")
		} else {
			out = append(out, kv)
		}
	}
	return out
}

// secretFlags are argv flags whose adjacent value must be redacted. bd does not
// currently take credentials on the command line, but the abstraction has to be
// safe for future remote configuration.
var secretFlags = map[string]bool{
	"--password": true,
	"--token":    true,
	"--secret":   true,
}

// redactArgs replaces the values of credential-like flags.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		flag, value, inline := strings.Cut(out[i], "=")
		if secretFlags[flag] {
			if inline && value != "" {
				out[i] = flag + "=<redacted>"
			} else if i+1 < len(out) {
				out[i+1] = "<redacted>"
				i++
			}
		}
	}
	return out
}

const errorExcerptLimit = 8 << 10

func truncateForError(s string) string {
	if len(s) <= errorExcerptLimit {
		return s
	}
	return s[:errorExcerptLimit] + "\n... (truncated)"
}

// boundedBuffer accumulates output up to a fixed limit, then discards the rest
// so a pathological bd response cannot exhaust memory.
type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }

var _ io.Writer = (*boundedBuffer)(nil)
