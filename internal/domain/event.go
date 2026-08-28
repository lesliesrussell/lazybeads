// lb-1td
package domain

import (
	"encoding/json"
	"time"
)

// EventKind is open: unknown upstream kinds stay visible rather than being
// discarded during decoding.
type EventKind string

const (
	EventCreated        EventKind = "created"
	EventUpdated        EventKind = "updated"
	EventClaimed        EventKind = "claimed"
	EventUnclaimed      EventKind = "unclaimed"
	EventStatusChanged  EventKind = "status_changed"
	EventDependencyEdit EventKind = "dependency_changed"
	EventClosed         EventKind = "closed"
	EventReopened       EventKind = "reopened"
	EventMessage        EventKind = "message"
	EventMemory         EventKind = "memory"
	EventSync           EventKind = "sync"
	EventUnknown        EventKind = "unknown"
)

// Event is one entry in the operational feed.
type Event struct {
	ID        string          `json:"id,omitempty"`
	IssueID   *string         `json:"issue_id,omitempty"`
	Actor     *Actor          `json:"actor,omitempty"`
	Kind      EventKind       `json:"kind"`
	Timestamp time.Time       `json:"timestamp"`
	Summary   string          `json:"summary"`
	Before    map[string]any  `json:"before,omitempty"`
	After     map[string]any  `json:"after,omitempty"`
	Raw       json.RawMessage `json:"-"`
}
