// lb-1td
package domain

import (
	"encoding/json"
	"time"
)

// Memory is a project-scoped durable operational note stored by Beads.
type Memory struct {
	ID        string          `json:"id,omitempty"`
	Content   string          `json:"content"`
	Tags      []string        `json:"tags,omitempty"`
	CreatedAt *time.Time      `json:"created_at,omitempty"`
	Retired   bool            `json:"retired,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// PrimeResult is the workflow context emitted by `bd prime`.
type PrimeResult struct {
	Text     string          `json:"text"`
	Memories []Memory        `json:"memories,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// SyncStatus reports remote visibility. Available is false whenever LazyBeads
// could not obtain real evidence, so callers never render "clean" by default.
type SyncStatus struct {
	Available bool            `json:"available"`
	Remote    string          `json:"remote,omitempty"`
	Ahead     *int            `json:"ahead,omitempty"`
	Behind    *int            `json:"behind,omitempty"`
	Detail    string          `json:"detail,omitempty"`
	Raw       json.RawMessage `json:"-"`
}
