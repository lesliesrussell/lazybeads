// lb-lou
package output

import (
	"encoding/json"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// Clock lets golden tests freeze timestamps.
var Clock = time.Now

// EmitJSON writes a successful result inside the stable envelope.
//
// The payload is never styled: ANSI escapes must not appear in JSON output.
func (w *Writer) EmitJSON(command string, ws domain.WorkspaceRef, data any, warnings []string) error {
	if warnings == nil {
		warnings = []string{}
	}
	env := domain.Envelope{
		SchemaVersion: domain.SchemaVersion,
		Command:       command,
		Workspace:     ws,
		Data:          data,
		Warnings:      warnings,
		GeneratedAt:   Clock().UTC(),
	}
	enc := json.NewEncoder(w.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// EmitJSONError writes a failure envelope. Per the output contract this goes to
// stdout, because a caller that asked for --json is parsing stdout.
func (w *Writer) EmitJSONError(command string, payload domain.ErrorPayload) error {
	env := domain.ErrorEnvelope{
		SchemaVersion: domain.SchemaVersion,
		Command:       command,
		Error:         payload,
	}
	enc := json.NewEncoder(w.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// EmitJSONL writes one compact JSON object per line, for streaming consumers.
func (w *Writer) EmitJSONL(items []any) error {
	enc := json.NewEncoder(w.Out)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

// IssuesAsAny adapts a typed slice for JSONL emission.
func IssuesAsAny[T any](items []T) []any {
	out := make([]any, 0, len(items))
	for _, i := range items {
		out = append(out, i)
	}
	return out
}
