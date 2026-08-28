// lb-1td
package domain

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// IssueStatus, IssueType and RelationType are intentionally open string types.
// Beads may introduce new values at any time; unknown values are preserved and
// rendered as-is rather than coerced into a closed enum.
type IssueStatus string

type IssueType string

type RelationType string

const (
	StatusOpen       IssueStatus = "open"
	StatusInProgress IssueStatus = "in_progress"
	StatusBlocked    IssueStatus = "blocked"
	StatusDeferred   IssueStatus = "deferred"
	StatusClosed     IssueStatus = "closed"
)

const (
	TypeBug      IssueType = "bug"
	TypeFeature  IssueType = "feature"
	TypeTask     IssueType = "task"
	TypeEpic     IssueType = "epic"
	TypeChore    IssueType = "chore"
	TypeDecision IssueType = "decision"
)

const (
	RelBlocks         RelationType = "blocks"
	RelParentChild    RelationType = "parent-child"
	RelRelatesTo      RelationType = "relates-to"
	RelDiscoveredFrom RelationType = "discovered-from"
	RelDuplicates     RelationType = "duplicates"
	RelSupersedes     RelationType = "supersedes"
	RelRepliesTo      RelationType = "replies-to"
)

// Blocking reports whether a relation type gates readiness. Only blocking
// relations may be used to explain why work is not actionable.
func (r RelationType) Blocking() bool {
	switch RelationType(normalizeRelation(string(r))) {
	case RelBlocks, "waits-for":
		return true
	default:
		return false
	}
}

func normalizeRelation(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
}

// NormalizeRelation canonicalizes an upstream relation label without discarding
// unknown values.
func NormalizeRelation(s string) RelationType {
	if s == "" {
		return RelBlocks
	}
	return RelationType(normalizeRelation(s))
}

// Priority is the numeric Beads priority where 0 is the most urgent.
type Priority int

const PriorityUnknown Priority = -1

// Label renders a priority as P0..P4, or "P?" when unknown.
func (p Priority) Label() string {
	if p < 0 {
		return "P?"
	}
	return "P" + itoa(int(p))
}

// Score maps a priority onto the normalization table in the specification.
func (p Priority) Score() float64 {
	switch p {
	case 0:
		return 5
	case 1:
		return 4
	case 2:
		return 3
	case 3:
		return 2
	case 4:
		return 1
	default:
		return 0
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Actor identifies a human or agent operating on the workspace.
type Actor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

func (a *Actor) String() string {
	if a == nil {
		return ""
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

// Equal compares actors case-insensitively on either identity field, because
// Beads records whichever of name/email the local git or env configuration
// supplied.
func (a *Actor) Equal(other string) bool {
	if a == nil || other == "" {
		return false
	}
	other = strings.ToLower(strings.TrimSpace(other))
	return strings.ToLower(a.Name) == other || strings.ToLower(a.Email) == other
}

// Issue is the LazyBeads view of a Beads issue. Raw retains the upstream
// payload so unknown fields survive round-trips and debugging.
type Issue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`

	Type     IssueType   `json:"type"`
	Status   IssueStatus `json:"status"`
	Priority Priority    `json:"priority"`

	Assignee *Actor         `json:"assignee,omitempty"`
	Labels   []string       `json:"labels,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`

	ParentID    *string    `json:"parent_id,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	DeferredAt  *time.Time `json:"deferred_at,omitempty"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	CloseReason *string    `json:"close_reason,omitempty"`

	// Counts reported directly by Beads.
	DependencyCount int `json:"dependency_count"`
	DependentCount  int `json:"dependent_count"`
	CommentCount    int `json:"comment_count,omitempty"`

	// BlockedBy lists blocker IDs when the upstream command reports them.
	BlockedBy []string `json:"blocked_by,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// IsClosed reports whether the issue is in a terminal state.
func (i Issue) IsClosed() bool {
	return strings.EqualFold(string(i.Status), string(StatusClosed))
}

// IsInProgress reports whether work is actively claimed.
func (i Issue) IsInProgress() bool {
	return strings.EqualFold(string(i.Status), string(StatusInProgress))
}

// IsDeferred reports whether the issue is hidden from ready work.
func (i Issue) IsDeferred() bool {
	if strings.EqualFold(string(i.Status), string(StatusDeferred)) {
		return true
	}
	return i.DeferredAt != nil && i.DeferredAt.After(time.Now())
}

// HasLabel reports a case-insensitive label match.
func (i Issue) HasLabel(label string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	for _, l := range i.Labels {
		if strings.ToLower(l) == label {
			return true
		}
	}
	return false
}

// HasAnyLabel reports whether any of the supplied labels is present.
func (i Issue) HasAnyLabel(labels []string) bool {
	for _, l := range labels {
		if i.HasLabel(l) {
			return true
		}
	}
	return false
}

// HasAllLabels reports whether every supplied label is present.
func (i Issue) HasAllLabels(labels []string) bool {
	for _, l := range labels {
		if !i.HasLabel(l) {
			return false
		}
	}
	return true
}

// Age returns how long ago the issue was created. Beads occasionally reports
// timestamps in mixed zones, so a negative age is clamped to zero rather than
// rendered as a future date.
func (i Issue) Age(now time.Time) time.Duration {
	if i.CreatedAt == nil {
		return 0
	}
	d := now.Sub(*i.CreatedAt)
	if d < 0 {
		return 0
	}
	return d
}

// Idle returns how long since the issue was last updated, clamped at zero.
func (i Issue) Idle(now time.Time) time.Duration {
	if i.UpdatedAt == nil {
		return 0
	}
	d := now.Sub(*i.UpdatedAt)
	if d < 0 {
		return 0
	}
	return d
}

// IssueDetail augments an issue with its immediate relations.
type IssueDetail struct {
	Issue
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Dependents   []Dependency `json:"dependents,omitempty"`
	Events       []Event      `json:"events,omitempty"`
}

// Blockers returns the unresolved blocking dependencies of the issue.
func (d IssueDetail) Blockers() []Dependency {
	out := make([]Dependency, 0, len(d.Dependencies))
	for _, dep := range d.Dependencies {
		if dep.Type.Blocking() && !dep.Issue.IsClosed() {
			out = append(out, dep)
		}
	}
	return out
}

// Dependency is one edge plus the resolved issue on the far side.
type Dependency struct {
	Issue Issue        `json:"issue"`
	Type  RelationType `json:"type"`
}

// SortIssues applies the documented default ordering: lower priority number
// first, then higher downstream impact, then older, then a stable ID tiebreak.
func SortIssues(issues []Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		x, y := issues[a], issues[b]
		if x.Priority != y.Priority {
			return normPriority(x.Priority) < normPriority(y.Priority)
		}
		if x.DependentCount != y.DependentCount {
			return x.DependentCount > y.DependentCount
		}
		xc, yc := timeOrZero(x.CreatedAt), timeOrZero(y.CreatedAt)
		if !xc.Equal(yc) {
			return xc.Before(yc)
		}
		return x.ID < y.ID
	})
}

// normPriority pushes unknown priorities to the end of a priority-ascending sort.
func normPriority(p Priority) int {
	if p < 0 {
		return 1 << 20
	}
	return int(p)
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
