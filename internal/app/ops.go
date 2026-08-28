// lb-lh5
package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// FocusRequest selects whose work `lb focus` should emphasize.
type FocusRequest struct {
	Actor     string
	AllActors bool
	Recent    time.Duration
}

// FocusReport is the operator dashboard: mine, recent, unblocked, stale.
type FocusReport struct {
	Actor             string         `json:"actor,omitempty"`
	ActorConfigured   bool           `json:"actor_configured"`
	Active            []domain.Issue `json:"active"`
	RecentlyTouched   []domain.Issue `json:"recently_touched"`
	RecentlyUnblocked []domain.Issue `json:"recently_unblocked"`
	NeedsAttention    []domain.Issue `json:"needs_attention"`
}

// Focus gathers the work most relevant to the current operator.
func (s *Service) Focus(ctx context.Context, req FocusRequest) (*FocusReport, error) {
	actor := req.Actor
	if actor == "" {
		actor = s.Actor
	}
	recent := req.Recent
	if recent <= 0 {
		recent = 24 * time.Hour
	}
	now := Now()
	issues, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), All: true, Limit: maxSearchCandidates})
	if err != nil {
		return nil, err
	}
	ready, err := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope()})
	if err != nil {
		return nil, err
	}
	readySet := map[string]bool{}
	for _, i := range ready {
		readySet[i.ID] = true
	}
	stale, err := s.staleClaims(ctx, now)
	if err != nil {
		return nil, err
	}

	report := &FocusReport{Actor: actor, ActorConfigured: actor != ""}
	seen := map[string]bool{}
	take := func(list *[]domain.Issue, issue domain.Issue) {
		if seen[issue.ID] {
			return
		}
		seen[issue.ID] = true
		*list = append(*list, issue)
	}

	for _, issue := range issues {
		if issue.IsClosed() {
			continue
		}
		if issue.IsInProgress() && issue.Assignee != nil {
			if req.AllActors || (actor != "" && issue.Assignee.Equal(actor)) {
				take(&report.Active, issue)
			}
		}
	}
	for _, issue := range issues {
		if issue.IsClosed() || seen[issue.ID] {
			continue
		}
		if issue.Idle(now) <= recent && issue.UpdatedAt != nil {
			take(&report.RecentlyTouched, issue)
		}
	}
	for _, issue := range ready {
		if seen[issue.ID] {
			continue
		}
		if issue.CreatedAt != nil && issue.UpdatedAt != nil && issue.UpdatedAt.After(issue.CreatedAt.Add(time.Minute)) && issue.Idle(now) <= recent {
			take(&report.RecentlyUnblocked, issue)
		}
	}
	for _, issue := range stale {
		if !seen[issue.ID] {
			report.NeedsAttention = append(report.NeedsAttention, issue)
		} else if req.AllActors || (actor != "" && issue.Assignee != nil && issue.Assignee.Equal(actor)) {
			// Stale claims already shown as active still belong in attention.
			report.NeedsAttention = append(report.NeedsAttention, issue)
		}
	}
	return report, nil
}

// StaleRequest filters the abandoned-work list.
type StaleRequest struct {
	Since       time.Duration
	PriorityMax *int
	ClaimedOnly bool
}

// StaleResult is the `lb stale` payload.
type StaleResult struct {
	Issues []domain.Issue `json:"issues"`
	Since  string         `json:"since"`
}

// Stale lists in-progress claimed work that has gone quiet.
func (s *Service) Stale(ctx context.Context, req StaleRequest) (*StaleResult, error) {
	since := req.Since
	if since <= 0 {
		since = s.Config.General.StaleAfter.Duration()
	}
	now := Now()
	issues, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), Status: string(domain.StatusInProgress), All: true})
	if err != nil {
		return nil, err
	}
	var out []domain.Issue
	for _, i := range issues {
		if i.IsClosed() || i.IsDeferred() {
			continue
		}
		if req.ClaimedOnly && (i.Assignee == nil || i.Assignee.String() == "") {
			continue
		}
		if i.Assignee == nil || i.Assignee.String() == "" {
			continue
		}
		if req.PriorityMax != nil && (i.Priority < 0 || int(i.Priority) > *req.PriorityMax) {
			continue
		}
		if i.Idle(now) >= since {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Idle(now) > out[b].Idle(now) })
	return &StaleResult{Issues: out, Since: compactDuration(since)}, nil
}

// ActivityRequest filters the operational event feed.
type ActivityRequest struct {
	Since   time.Duration
	IssueID string
	Actor   string
	Type    string
	Limit   int
}

// ActivityResult is a merged event feed.
type ActivityResult struct {
	Events []domain.Event `json:"events"`
	Since  string         `json:"since,omitempty"`
}

// Activity merges per-issue history into one feed. When Beads has no history
// capability, it synthesizes coarse events from issue timestamps.
func (s *Service) Activity(ctx context.Context, req ActivityRequest) (*ActivityResult, error) {
	since := req.Since
	if since <= 0 {
		since = 24 * time.Hour
	}
	cutoff := Now().Add(-since)
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	var events []domain.Event
	if req.IssueID != "" {
		ev, err := s.issueEvents(ctx, req.IssueID)
		if err != nil {
			return nil, err
		}
		events = ev
	} else {
		issues, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), All: true, Limit: maxSearchCandidates})
		if err != nil {
			return nil, err
		}
		sort.SliceStable(issues, func(a, b int) bool {
			return timeOrZero(issues[a].UpdatedAt).After(timeOrZero(issues[b].UpdatedAt))
		})
		if len(issues) > 40 {
			issues = issues[:40]
		}
		for _, issue := range issues {
			ev, err := s.issueEvents(ctx, issue.ID)
			if err != nil {
				continue
			}
			events = append(events, ev...)
		}
	}

	kind := strings.ToLower(strings.TrimSpace(req.Type))
	actor := strings.ToLower(strings.TrimSpace(req.Actor))
	var out []domain.Event
	for _, ev := range events {
		if !ev.Timestamp.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		if kind != "" && !strings.Contains(strings.ToLower(string(ev.Kind)), kind) && !strings.Contains(strings.ToLower(ev.Summary), kind) {
			continue
		}
		if actor != "" && (ev.Actor == nil || !ev.Actor.Equal(actor)) {
			continue
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Timestamp.After(out[b].Timestamp) })
	if len(out) > limit {
		out = out[:limit]
	}
	return &ActivityResult{Events: out, Since: compactDuration(since)}, nil
}

func (s *Service) issueEvents(ctx context.Context, id string) ([]domain.Event, error) {
	ev, err := s.Client.History(ctx, id, s.Scope())
	if err != nil {
		if ce, ok := beads.AsCommandError(err); ok && (ce.Kind == beads.ErrUnsupported || ce.Kind == beads.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(ev) > 0 {
		return ev, nil
	}
	detail, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, nil
	}
	ts := timeOrZero(detail.UpdatedAt)
	if ts.IsZero() {
		ts = timeOrZero(detail.CreatedAt)
	}
	return []domain.Event{{
		IssueID:   &detail.ID,
		Kind:      domain.EventUpdated,
		Timestamp: ts,
		Summary:   detail.Title,
		Actor:     detail.Assignee,
	}}, nil
}

// MemoryAdd stores a durable note via `bd remember`.
func (s *Service) MemoryAdd(ctx context.Context, content string, opts MutateOptions) (*MutationResult, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, &UsageError{Message: "memory content is required"}
	}
	in := beads.RememberInput{Scope: s.Scope(), Content: content}
	result := &MutationResult{
		Action:  "memory add",
		Argv:    s.Client.ArgvFor("remember", "remember", content),
		Message: "Remember",
	}
	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}
	var mem domain.Memory
	err := s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		mem, e = s.Client.Remember(ctx, in)
		return e
	})
	if err != nil {
		return nil, err
	}
	result.Message = "Remembered"
	if mem.ID != "" {
		result.Message = "Remembered " + mem.ID
	}
	return result, nil
}

// MemoryList lists or searches project memories.
func (s *Service) MemoryList(ctx context.Context, query string) ([]domain.Memory, error) {
	return s.Client.Memories(ctx, query, s.Scope())
}

// MemoryPrime returns Beads workflow context.
func (s *Service) MemoryPrime(ctx context.Context) (domain.PrimeResult, error) {
	return s.Client.Prime(ctx, s.Scope())
}

// MemoryRetire forgets a memory.
func (s *Service) MemoryRetire(ctx context.Context, id string, opts MutateOptions) (*MutationResult, error) {
	if strings.TrimSpace(id) == "" {
		return nil, &UsageError{Message: "a memory id is required"}
	}
	result := &MutationResult{
		Action:  "memory retire",
		Argv:    s.Client.ArgvFor("forget", "forget", id),
		Message: "Retire " + id,
	}
	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}
	err := s.withWrite(ctx, func(ctx context.Context) error {
		return s.Client.Forget(ctx, id, s.Scope())
	})
	if err != nil {
		return nil, err
	}
	result.Message = "Retired " + id
	return result, nil
}

// SyncInspect is read-only remote visibility. It never claims "clean".
func (s *Service) SyncInspect(ctx context.Context) (domain.SyncStatus, error) {
	st, err := s.Client.SyncStatus(ctx, s.Scope())
	if err != nil {
		return domain.SyncStatus{Available: false, Detail: "LazyBeads could not determine remote status"}, nil
	}
	if !st.Available && st.Detail == "" {
		st.Detail = "LazyBeads could not determine remote status"
	}
	return st, nil
}

// BlockedResult groups blocked work by closest unresolved blocker.
type BlockedResult struct {
	Groups []BlockedGroup `json:"groups"`
	Total  int            `json:"total"`
}

// BlockedGroup is one blocker and the issues waiting on it.
type BlockedGroup struct {
	Blocker domain.Issue   `json:"blocker"`
	Issues  []domain.Issue `json:"issues"`
}

// BlockedRequest filters the blocked-work view.
type BlockedRequest struct {
	PriorityMax *int
	GroupBy     string
}

// Blocked lists blocked issues grouped by their closest unresolved blocker.
func (s *Service) Blocked(ctx context.Context, req BlockedRequest) (*BlockedResult, error) {
	blocked, err := s.Client.Blocked(ctx, s.Scope())
	if err != nil {
		return nil, err
	}
	var filtered []domain.Issue
	for _, i := range blocked {
		if req.PriorityMax != nil && (i.Priority < 0 || int(i.Priority) > *req.PriorityMax) {
			continue
		}
		filtered = append(filtered, i)
	}
	groups := map[string]*BlockedGroup{}
	var order []string
	for _, i := range filtered {
		key := "unknown"
		var blocker domain.Issue
		if len(i.BlockedBy) > 0 {
			key = i.BlockedBy[0]
			if req.GroupBy == "parent" && i.ParentID != nil {
				key = *i.ParentID
			}
			if detail, err := s.showIssue(ctx, key); err == nil {
				blocker = detail.Issue
			} else {
				blocker = domain.Issue{ID: key, Title: key}
			}
		} else if req.GroupBy == "parent" && i.ParentID != nil {
			key = *i.ParentID
			if detail, err := s.showIssue(ctx, key); err == nil {
				blocker = detail.Issue
			}
		} else {
			blocker = domain.Issue{ID: "unknown", Title: "unresolved blocker"}
		}
		g, ok := groups[key]
		if !ok {
			g = &BlockedGroup{Blocker: blocker}
			groups[key] = g
			order = append(order, key)
		}
		g.Issues = append(g.Issues, i)
	}
	result := &BlockedResult{Total: len(filtered)}
	for _, key := range order {
		result.Groups = append(result.Groups, *groups[key])
	}
	return result, nil
}
