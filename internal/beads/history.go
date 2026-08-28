// lb-1td
package beads

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// decodeHistory converts `bd history` output into events. bd reports a series
// of Dolt commits carrying the issue snapshot at each point, so the event kind
// is inferred by diffing consecutive snapshots.
func decodeHistory(data []byte, issueID string) ([]domain.Event, error) {
	trimmed := trimJSON(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var entries []struct {
		CommitHash string          `json:"CommitHash"`
		Committer  string          `json:"Committer"`
		CommitDate string          `json:"CommitDate"`
		Issue      json.RawMessage `json:"Issue"`
	}
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, decodeErr(err, trimmed)
	}

	type snapshot struct {
		issue domain.Issue
		hash  string
		when  string
		who   string
	}
	snaps := make([]snapshot, 0, len(entries))
	for _, e := range entries {
		raws, err := decodeRawIssues(e.Issue)
		if err != nil || len(raws) == 0 {
			continue
		}
		snaps = append(snaps, snapshot{
			issue: raws[0].toIssue(),
			hash:  e.CommitHash,
			when:  e.CommitDate,
			who:   e.Committer,
		})
	}
	// bd emits newest first; process oldest first so diffs read forwards.
	sort.SliceStable(snaps, func(a, b int) bool { return snaps[a].when < snaps[b].when })

	events := make([]domain.Event, 0, len(snaps))
	for i, s := range snaps {
		id := issueID
		ev := domain.Event{
			ID:      s.hash,
			IssueID: &id,
			Kind:    domain.EventUpdated,
			Summary: "updated",
		}
		if t := parseTime(s.when); t != nil {
			ev.Timestamp = *t
		}
		// Prefer the issue's own assignee over the Dolt committer, which is an
		// implementation detail of the storage engine rather than an operator.
		if s.issue.Assignee != nil {
			ev.Actor = s.issue.Assignee
		} else if s.who != "" && s.who != "root" {
			ev.Actor = &domain.Actor{Name: s.who}
		}

		if i == 0 {
			ev.Kind = domain.EventCreated
			ev.Summary = "created " + s.issue.Title
		} else {
			prev := snaps[i-1].issue
			ev.Kind, ev.Summary, ev.Before, ev.After = diffIssues(prev, s.issue)
		}
		events = append(events, ev)
	}
	// Present newest first, which is what an operational feed wants.
	for l, r := 0, len(events)-1; l < r; l, r = l+1, r-1 {
		events[l], events[r] = events[r], events[l]
	}
	return events, nil
}

// diffIssues classifies the transition between two consecutive snapshots.
func diffIssues(prev, next domain.Issue) (domain.EventKind, string, map[string]any, map[string]any) {
	before := map[string]any{}
	after := map[string]any{}
	kind := domain.EventUpdated
	var parts []string

	if prev.Status != next.Status {
		before["status"] = string(prev.Status)
		after["status"] = string(next.Status)
		parts = append(parts, "status "+string(prev.Status)+" → "+string(next.Status))
		switch {
		case next.IsClosed():
			kind = domain.EventClosed
		case prev.IsClosed() && !next.IsClosed():
			kind = domain.EventReopened
		case next.IsInProgress():
			kind = domain.EventClaimed
		default:
			kind = domain.EventStatusChanged
		}
	}
	if prevA, nextA := prev.Assignee.String(), next.Assignee.String(); prevA != nextA {
		before["assignee"] = prevA
		after["assignee"] = nextA
		switch {
		case nextA == "":
			kind = domain.EventUnclaimed
			parts = append(parts, "unassigned")
		case prevA == "":
			parts = append(parts, "assigned to "+nextA)
		default:
			parts = append(parts, "reassigned "+prevA+" → "+nextA)
		}
	}
	if prev.Priority != next.Priority {
		before["priority"] = int(prev.Priority)
		after["priority"] = int(next.Priority)
		parts = append(parts, "priority "+prev.Priority.Label()+" → "+next.Priority.Label())
	}
	if prev.Title != next.Title {
		before["title"] = prev.Title
		after["title"] = next.Title
		parts = append(parts, "retitled")
	}
	if prev.Description != next.Description {
		parts = append(parts, "description edited")
	}
	if !equalStrings(prev.Labels, next.Labels) {
		before["labels"] = prev.Labels
		after["labels"] = next.Labels
		parts = append(parts, "labels changed")
	}
	if prev.DependencyCount != next.DependencyCount || prev.DependentCount != next.DependentCount {
		kind = domain.EventDependencyEdit
		parts = append(parts, "dependencies changed")
	}

	if len(parts) == 0 {
		return kind, "updated", nil, nil
	}
	return kind, strings.Join(parts, "; "), nilIfEmpty(before), nilIfEmpty(after)
}

func nilIfEmpty(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
