// lb-1td
package beads

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// rawIssue mirrors the fields bd emits. Alternate spellings are accepted
// because the field names have drifted across upstream versions, and every
// unrecognized field survives in Raw.
type rawIssue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`

	Status string `json:"status"`

	IssueType string `json:"issue_type"`
	AltType   string `json:"type"`

	Priority *json.Number `json:"priority"`

	Owner       string `json:"owner"`
	Assignee    string `json:"assignee"`
	CreatedByNm string `json:"created_by"`

	Labels []string `json:"labels"`

	ParentID  string `json:"parent_id"`
	AltParent string `json:"parent"`

	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	ClosedAt   string `json:"closed_at"`
	DueAt      string `json:"due_at"`
	DeferUntil string `json:"defer_until"`
	AltDefer   string `json:"deferred_at"`

	CloseReason string `json:"close_reason"`

	Metadata map[string]any `json:"metadata"`

	DependencyCount int `json:"dependency_count"`
	DependentCount  int `json:"dependent_count"`
	CommentCount    int `json:"comment_count"`
	BlockedByCount  int `json:"blocked_by_count"`

	BlockedBy []string `json:"blocked_by"`

	// Present on `bd show` and `bd dep list` payloads.
	Dependencies   []rawDependency `json:"dependencies"`
	Dependents     []rawDependency `json:"dependents"`
	DependencyType string          `json:"dependency_type"`
}

type rawDependency struct {
	rawIssue
}

// decodeIssues parses a bd payload that may be a bare array, a bare object, or
// an envelope with an "issues"/"data" member.
func decodeIssues(data []byte) ([]domain.Issue, error) {
	raws, err := decodeRawIssues(data)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Issue, 0, len(raws))
	for _, r := range raws {
		out = append(out, r.toIssue())
	}
	return out, nil
}

// rawWithBytes pairs a decoded issue with the exact bytes it came from.
type rawWithBytes struct {
	rawIssue
	bytes json.RawMessage
}

func (r rawWithBytes) toIssue() domain.Issue {
	issue := r.rawIssue.toIssue()
	issue.Raw = r.bytes
	return issue
}

func decodeRawIssues(data []byte) ([]rawWithBytes, error) {
	trimmed := trimJSON(data)
	if len(trimmed) == 0 {
		return nil, nil
	}

	switch trimmed[0] {
	case '[':
		var msgs []json.RawMessage
		if err := json.Unmarshal(trimmed, &msgs); err != nil {
			return nil, decodeErr(err, trimmed)
		}
		out := make([]rawWithBytes, 0, len(msgs))
		for _, m := range msgs {
			var r rawIssue
			if err := json.Unmarshal(m, &r); err != nil {
				return nil, decodeErr(err, m)
			}
			out = append(out, rawWithBytes{rawIssue: r, bytes: m})
		}
		return out, nil
	case '{':
		// Either a single issue or an envelope containing a list.
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err != nil {
			return nil, decodeErr(err, trimmed)
		}
		for _, key := range []string{"issues", "data", "results", "items"} {
			if nested, ok := probe[key]; ok && len(trimJSON(nested)) > 0 && trimJSON(nested)[0] == '[' {
				return decodeRawIssues(nested)
			}
		}
		if _, hasID := probe["id"]; !hasID {
			// An envelope with no recognizable issue payload is an empty result,
			// not a decode failure: `bd memories --json` returns exactly this.
			return nil, nil
		}
		var r rawIssue
		if err := json.Unmarshal(trimmed, &r); err != nil {
			return nil, decodeErr(err, trimmed)
		}
		return []rawWithBytes{{rawIssue: r, bytes: append(json.RawMessage(nil), trimmed...)}}, nil
	default:
		return nil, decodeErr(fmt.Errorf("expected a JSON object or array"), trimmed)
	}
}

func decodeErr(cause error, payload []byte) error {
	return &CommandError{
		Kind:      ErrDecode,
		Operation: "decode bd JSON",
		Stdout:    truncateForError(string(payload)),
		Cause:     cause,
	}
}

// trimJSON strips whitespace and any non-JSON preamble bd may print before the
// payload (some builds emit advisory lines on stdout).
func trimJSON(data []byte) []byte {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return nil
	}
	if s[0] == '{' || s[0] == '[' {
		return []byte(s)
	}
	// Fall back to the first structural character on its own line.
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '[' {
			return []byte(strings.TrimSpace(s[i:]))
		}
	}
	return nil
}

func (r rawIssue) toIssue() domain.Issue {
	issue := domain.Issue{
		ID:              strings.TrimSpace(r.ID),
		Title:           r.Title,
		Description:     r.Description,
		Status:          domain.IssueStatus(strings.TrimSpace(r.Status)),
		Type:            domain.IssueType(firstNonEmpty(r.IssueType, r.AltType)),
		Priority:        parsePriority(r.Priority),
		Labels:          r.Labels,
		Metadata:        r.Metadata,
		DependencyCount: r.DependencyCount,
		DependentCount:  r.DependentCount,
		CommentCount:    r.CommentCount,
		BlockedBy:       r.BlockedBy,
	}

	if owner := firstNonEmpty(r.Assignee, r.Owner); owner != "" {
		issue.Assignee = actorFrom(owner)
	}
	if parent := firstNonEmpty(r.ParentID, r.AltParent); parent != "" {
		issue.ParentID = &parent
	}
	if r.CloseReason != "" {
		reason := r.CloseReason
		issue.CloseReason = &reason
	}

	issue.CreatedAt = parseTime(r.CreatedAt)
	issue.UpdatedAt = parseTime(r.UpdatedAt)
	issue.ClosedAt = parseTime(r.ClosedAt)
	issue.DueAt = parseTime(r.DueAt)
	issue.DeferredAt = parseTime(firstNonEmpty(r.DeferUntil, r.AltDefer))

	// When bd reports a blocker count but no list, keep the count meaningful.
	if len(issue.BlockedBy) == 0 && r.BlockedByCount > 0 {
		issue.DependencyCount = maxInt(issue.DependencyCount, r.BlockedByCount)
	}
	return issue
}

// actorFrom splits an "Name <email>" or bare identity into an Actor.
func actorFrom(s string) *domain.Actor {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if open := strings.Index(s, "<"); open >= 0 && strings.HasSuffix(s, ">") {
		return &domain.Actor{
			Name:  strings.TrimSpace(s[:open]),
			Email: strings.TrimSpace(s[open+1 : len(s)-1]),
		}
	}
	if strings.Contains(s, "@") {
		return &domain.Actor{Name: s, Email: s}
	}
	return &domain.Actor{Name: s}
}

func parsePriority(n *json.Number) domain.Priority {
	if n == nil {
		return domain.PriorityUnknown
	}
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(n.String(), "P"), "p"))
	if s == "" {
		return domain.PriorityUnknown
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return domain.PriorityUnknown
	}
	return domain.Priority(v)
}

// timeLayouts covers the formats bd has been observed to emit.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" || strings.HasPrefix(s, "0001-01-01") {
		return nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// decodeDetail parses a `bd show --json` payload into an issue with relations.
func decodeDetail(data []byte) (domain.IssueDetail, error) {
	raws, err := decodeRawIssues(data)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	if len(raws) == 0 {
		return domain.IssueDetail{}, &CommandError{
			Kind:      ErrNotFound,
			Operation: "show issue",
			Cause:     fmt.Errorf("bd returned no issue"),
		}
	}
	r := raws[0]
	detail := domain.IssueDetail{Issue: r.toIssue()}
	for _, dep := range r.Dependencies {
		detail.Dependencies = append(detail.Dependencies, domain.Dependency{
			Issue: dep.toIssue(),
			Type:  domain.NormalizeRelation(dep.DependencyType),
		})
	}
	for _, dep := range r.Dependents {
		detail.Dependents = append(detail.Dependents, domain.Dependency{
			Issue: dep.toIssue(),
			Type:  domain.NormalizeRelation(dep.DependencyType),
		})
	}
	return detail, nil
}

// decodeDependencies parses a `bd dep list` payload, where the relation type
// travels alongside each issue.
func decodeDependencies(data []byte) ([]domain.Dependency, error) {
	raws, err := decodeRawIssues(data)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Dependency, 0, len(raws))
	for _, r := range raws {
		out = append(out, domain.Dependency{
			Issue: r.toIssue(),
			Type:  domain.NormalizeRelation(r.DependencyType),
		})
	}
	return out, nil
}
