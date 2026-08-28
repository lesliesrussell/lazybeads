// lb-1td
package domain

import "time"

// SchemaVersion is the LazyBeads JSON contract version. Adding fields is
// backward compatible; renaming or removing one requires incrementing this.
const SchemaVersion = 1

// WorkspaceRef is the compact workspace identity embedded in every envelope.
type WorkspaceRef struct {
	RootPath string  `json:"root_path"`
	Rig      *string `json:"rig"`
}

// Envelope is the stable wrapper around every successful JSON response.
type Envelope struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	Workspace     WorkspaceRef `json:"workspace"`
	Data          any          `json:"data"`
	Warnings      []string     `json:"warnings"`
	GeneratedAt   time.Time    `json:"generated_at"`
}

// ErrorPayload is the machine-readable body of a failed command.
type ErrorPayload struct {
	Code           string  `json:"code"`
	Message        string  `json:"message"`
	Hint           string  `json:"hint,omitempty"`
	UpstreamStderr *string `json:"upstream_stderr"`
}

// ErrorEnvelope is the stable wrapper around a failure.
type ErrorEnvelope struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command,omitempty"`
	Error         ErrorPayload `json:"error"`
}

// Exit codes defined by the specification. Anything outside this set is a bug.
const (
	ExitOK                = 0
	ExitRuntime           = 1
	ExitUsage             = 2
	ExitBDUnavailable     = 3
	ExitWorkspace         = 4
	ExitSchema            = 5
	ExitCapability        = 6
	ExitMutationRejected  = 7
	ExitDeclined          = 8
	ExitTimeout           = 9
	ExitInternalInvariant = 10
)

// HealthLevel expresses how confident LazyBeads is about a condition.
type HealthLevel string

const (
	HealthOK      HealthLevel = "ok"
	HealthInfo    HealthLevel = "info"
	HealthWarning HealthLevel = "warning"
	HealthError   HealthLevel = "error"
	HealthUnknown HealthLevel = "unknown"
)

// Rank orders levels from least to most severe so an overall status can be
// reduced from a check list.
func (h HealthLevel) Rank() int {
	switch h {
	case HealthOK:
		return 0
	case HealthInfo:
		return 1
	case HealthUnknown:
		return 2
	case HealthWarning:
		return 3
	case HealthError:
		return 4
	default:
		return 2
	}
}

// HealthCheck is one diagnosed condition.
type HealthCheck struct {
	Name    string      `json:"name"`
	Level   HealthLevel `json:"level"`
	Summary string      `json:"summary"`
	Detail  string      `json:"detail,omitempty"`
	Hint    string      `json:"hint,omitempty"`
	Items   []string    `json:"items,omitempty"`
}

// Health aggregates checks into a single reportable status.
type Health struct {
	Status HealthLevel   `json:"status"`
	Checks []HealthCheck `json:"checks"`
}

// Reduce recomputes the overall status as the most severe check level.
func (h *Health) Reduce() {
	worst := HealthOK
	for _, c := range h.Checks {
		if c.Level.Rank() > worst.Rank() {
			worst = c.Level
		}
	}
	h.Status = worst
}
