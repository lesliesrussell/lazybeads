// lb-1td
package beads

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// ErrorKind is the stable machine-readable error taxonomy.
type ErrorKind string

const (
	ErrBDBinaryMissing   ErrorKind = "bd_not_found"
	ErrBDExecution       ErrorKind = "bd_execution_failed"
	ErrWorkspaceNotFound ErrorKind = "workspace_not_found"
	ErrSchemaMismatch    ErrorKind = "schema_mismatch"
	ErrUnsupported       ErrorKind = "unsupported_capability"
	ErrNotFound          ErrorKind = "issue_not_found"
	ErrConflict          ErrorKind = "mutation_conflict"
	ErrValidation        ErrorKind = "validation_error"
	ErrTimeout           ErrorKind = "timeout"
	ErrCancelled         ErrorKind = "cancelled"
	ErrDecode            ErrorKind = "invalid_bd_json"
)

// CommandError carries the full upstream context for a failed bd invocation.
// Args are already redacted by the runner before they reach here.
type CommandError struct {
	Kind      ErrorKind
	Operation string
	Args      []string
	ExitCode  int
	Stdout    string
	Stderr    string
	Cause     error
	Hint      string
}

func (e *CommandError) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Kind))
	if e.Operation != "" {
		b.WriteString(" during ")
		b.WriteString(e.Operation)
	}
	if msg := e.upstreamMessage(); msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	} else if e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

func (e *CommandError) Unwrap() error { return e.Cause }

// upstreamMessage extracts the most useful single line from bd's stderr.
func (e *CommandError) upstreamMessage() string {
	for _, line := range strings.Split(e.Stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.TrimPrefix(strings.TrimPrefix(line, "Error: "), "error: ")
	}
	return ""
}

// Message returns the concise LazyBeads diagnosis shown to the user.
func (e *CommandError) Message() string {
	switch e.Kind {
	case ErrBDBinaryMissing:
		return "Beads CLI (`bd`) was not found."
	case ErrBDExecution:
		if e.ExitCode == 0 {
			return "Found `bd`, but it could not execute."
		}
		if msg := e.upstreamMessage(); msg != "" {
			return msg
		}
		return "The `bd` command failed."
	case ErrWorkspaceNotFound:
		return "No Beads workspace was found from this directory."
	case ErrSchemaMismatch:
		return "Your `bd` binary cannot safely open this Beads database."
	case ErrUnsupported:
		return "This LazyBeads command requires a newer compatible Beads capability."
	case ErrNotFound:
		return "That issue was not found in this workspace."
	case ErrConflict:
		return "Beads rejected the requested change; refresh and retry."
	case ErrValidation:
		if msg := e.upstreamMessage(); msg != "" {
			return msg
		}
		return "Beads rejected the request as invalid."
	case ErrTimeout:
		return "The `bd` command timed out."
	case ErrCancelled:
		return "The command was cancelled."
	case ErrDecode:
		return "LazyBeads could not understand the JSON emitted by `bd`."
	default:
		return e.Error()
	}
}

// UserHint returns actionable recovery guidance.
func (e *CommandError) UserHint() string {
	if e.Hint != "" {
		return e.Hint
	}
	switch e.Kind {
	case ErrBDBinaryMissing:
		return "Install Beads or set LB_BD_BIN to the bd binary."
	case ErrWorkspaceNotFound:
		return "Run `bd init` here, or pass --project/--beads-dir."
	case ErrSchemaMismatch:
		return "Upgrade `bd`, then follow its documented migration steps. LazyBeads will not migrate for you."
	case ErrUnsupported:
		return "Upgrade Beads to a version that provides this capability."
	case ErrConflict:
		return "Re-read the issue with `lb show` and retry."
	case ErrDecode:
		return "Run with --debug to see the raw output, and check your `bd` version."
	case ErrTimeout:
		return "Increase --timeout, or check whether the Beads database is locked."
	}
	return ""
}

// ExitCode maps an error onto the LazyBeads exit code table.
func (e *CommandError) ExitCode2() int {
	switch e.Kind {
	case ErrBDBinaryMissing, ErrBDExecution:
		return domain.ExitBDUnavailable
	case ErrWorkspaceNotFound:
		return domain.ExitWorkspace
	case ErrSchemaMismatch:
		return domain.ExitSchema
	case ErrUnsupported:
		return domain.ExitCapability
	case ErrConflict:
		return domain.ExitMutationRejected
	case ErrValidation:
		return domain.ExitUsage
	case ErrNotFound:
		return domain.ExitRuntime
	case ErrTimeout, ErrCancelled:
		return domain.ExitTimeout
	case ErrDecode:
		return domain.ExitRuntime
	}
	return domain.ExitRuntime
}

// AsCommandError extracts a *CommandError from an error chain.
func AsCommandError(err error) (*CommandError, bool) {
	var ce *CommandError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

// classifyStderr maps bd's diagnostic text onto the error taxonomy. bd exits
// with 1 for most failures, so the text is the only available signal.
func classifyStderr(stderr string, exitCode int, ctxErr error) ErrorKind {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(ctxErr, context.Canceled) {
		return ErrCancelled
	}
	s := strings.ToLower(stderr)
	switch {
	case containsAny(s, "schema version", "schema skew", "schema mismatch", "migrate"):
		return ErrSchemaMismatch
	case containsAny(s, "no beads database", "not a beads", "no .beads", "beads not initialized", "could not find .beads", "no database found"):
		return ErrWorkspaceNotFound
	case containsAny(s, "not found", "no such issue", "does not exist", "unknown issue"):
		return ErrNotFound
	case containsAny(s, "unknown command", "unknown flag", "unknown shorthand"):
		return ErrUnsupported
	case containsAny(s, "cycle", "would create a cycle", "conflict", "already claimed", "concurrent"):
		return ErrConflict
	case containsAny(s, "invalid", "required", "must be", "cannot be empty"):
		return ErrValidation
	}
	return ErrBDExecution
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// UnsupportedError builds a capability error for a feature bd does not expose.
func UnsupportedError(operation, capability, hint string) *CommandError {
	return &CommandError{
		Kind:      ErrUnsupported,
		Operation: operation,
		Hint:      hint,
		Cause:     fmt.Errorf("capability %q unavailable in the installed bd", capability),
	}
}
