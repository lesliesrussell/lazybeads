// lb-4gm.4
package beads

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// MaxStreamLine bounds one line of streamed output. A journal record carries
// a whole issue, so this matches the whole-output cap of a normal call.
const MaxStreamLine = MaxStdoutCapture

// errStopStream lets a callback end a stream early without it being reported
// as a failure.
var errStopStream = errors.New("stop stream")

// StreamOptions shape a long-running bd invocation.
type StreamOptions struct {
	// Timeout bounds the whole call; zero means none, for --follow.
	Timeout time.Duration
	// Stdout receives each stdout line in order; returning an error stops bd.
	Stdout func(line []byte) error
	// Stderr receives each stderr line; returning an error stops bd.
	Stderr func(line string) error
}

// Stream runs bd like Run, but hands its output to callbacks line by line as
// it arrives instead of buffering it, and applies no timeout unless asked.
// Cancelling ctx stops bd. The argv-only and redaction rules of Run apply.
func (r *Runner) Stream(ctx context.Context, operation string, scope Scope, opts StreamOptions, args ...string) error {
	bin, err := r.Resolve()
	if err != nil {
		return err
	}
	full := append(scopeArgs(scope), args...)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, bin, full...)
	cmd.Dir = scope.Project
	cmd.Env = commandEnv(scope)
	cmd.Stdin = nil
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &CommandError{Kind: ErrBDExecution, Operation: operation, Cause: err}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &CommandError{Kind: ErrBDExecution, Operation: operation, Cause: err}
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return &CommandError{Kind: ErrBDExecution, Operation: operation, Args: redactArgs(full), Cause: err}
	}

	var (
		mu        sync.Mutex
		stopErr   error
		errOutput strings.Builder
	)
	stop := func(err error) {
		mu.Lock()
		if stopErr == nil {
			stopErr = err
		}
		mu.Unlock()
		cancel()
	}
	// Cancellation kills bd, but a child of bd holding the pipes open would
	// keep the readers blocked: close them after a grace period.
	readersDone := make(chan struct{})
	go func() {
		select {
		case <-readersDone:
		case <-runCtx.Done():
			select {
			case <-readersDone:
			case <-time.After(cmd.WaitDelay):
				_ = stdout.Close()
				_ = stderrPipe.Close()
			}
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), MaxStreamLine)
		for sc.Scan() {
			if opts.Stdout != nil {
				if err := opts.Stdout(sc.Bytes()); err != nil {
					stop(err)
					break
				}
			}
		}
		if err := sc.Err(); err != nil {
			stop(&CommandError{Kind: ErrDecode, Operation: operation, Cause: err})
		}
		drain(stdout)
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			if errOutput.Len() < MaxStderrCapture {
				errOutput.WriteString(line)
				errOutput.WriteByte('\n')
			}
			mu.Unlock()
			if opts.Stderr != nil {
				if err := opts.Stderr(line); err != nil {
					stop(err)
					break
				}
			}
		}
		drain(stderrPipe)
	}()
	wg.Wait()
	close(readersDone)
	runErr := cmd.Wait()

	mu.Lock()
	callbackErr, stderrText := stopErr, errOutput.String()
	mu.Unlock()
	var exitErr *exec.ExitError
	exitCode := 0
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	if r.Debug {
		r.mu.Lock()
		r.traces = append(r.traces, Trace{Args: full, Dir: cmd.Dir, Env: redactEnv(cmd.Env),
			Duration: time.Since(started), ExitCode: exitCode, Err: runErr})
		r.mu.Unlock()
	}
	switch {
	case errors.Is(callbackErr, errStopStream):
		return nil
	case callbackErr != nil:
		return callbackErr
	case runErr == nil:
		return nil
	case ctx.Err() != nil:
		return &CommandError{Kind: classifyStderr("", 0, ctx.Err()), Operation: operation, Args: redactArgs(full), Cause: ctx.Err()}
	}
	return &CommandError{
		Kind:      classifyStderr(stderrText, exitCode, runCtx.Err()),
		Operation: operation,
		Args:      redactArgs(full),
		ExitCode:  exitCode,
		Stderr:    truncateForError(stderrText),
		Cause:     runErr,
	}
}

// drain discards whatever a stopped reader left, so bd never blocks writing.
func drain(r interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 32<<10)
	for {
		if _, err := r.Read(buf); err != nil {
			return
		}
	}
}
