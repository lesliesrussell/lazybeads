// lb-58x
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// StatusCounts is the work-state snapshot shown by `lb status`.
type StatusCounts struct {
	Ready      int `json:"ready"`
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Closed     int `json:"closed"`
}

// AttentionItem is one operator-facing warning on the status dashboard.
type AttentionItem struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	IDs     []string `json:"ids,omitempty"`
}

// StatusReport is the workspace-health overview. The CLI and TUI both render
// this structure; neither invents counts of its own.
type StatusReport struct {
	Workspace   domain.WorkspaceRef `json:"workspace"`
	BDVersion   string              `json:"bd_version,omitempty"`
	Storage     string              `json:"storage"`
	Actor       string              `json:"actor,omitempty"`
	UpdatedAt   *time.Time          `json:"updated_at,omitempty"`
	Counts      StatusCounts        `json:"counts"`
	Attention   []AttentionItem     `json:"attention"`
	Health      domain.Health       `json:"health"`
	GeneratedAt time.Time           `json:"generated_at"`
}

// Status gathers counts, attention items and a reduced health report from Beads.
func (s *Service) Status(ctx context.Context) (*StatusReport, error) {
	now := Now()
	storage := string(s.Workspace.StorageMode)
	if storage == "" {
		storage = string(workspace.StorageUnknown)
	}
	report := &StatusReport{
		Workspace:   s.WorkspaceRef(),
		BDVersion:   s.Workspace.BDVersion,
		Storage:     storage,
		Actor:       s.Actor,
		Attention:   []AttentionItem{},
		GeneratedAt: now.UTC(),
	}
	if !s.Workspace.LastRefreshedAt.IsZero() {
		t := s.Workspace.LastRefreshedAt
		report.UpdatedAt = &t
	}

	stats, err := s.Client.Stats(ctx, s.Scope())
	if err != nil {
		return nil, err
	}
	if stats.Available {
		report.Counts = StatusCounts{
			Ready:      stats.Ready,
			Open:       stats.Open,
			InProgress: stats.InProgress,
			Blocked:    stats.Blocked,
			Closed:     stats.Closed,
		}
	} else if err := s.fillCountsFromList(ctx, &report.Counts); err != nil {
		return nil, err
	}

	stale, err := s.staleClaims(ctx, now)
	if err != nil {
		return nil, err
	}
	if n := len(stale); n > 0 {
		report.Attention = append(report.Attention, AttentionItem{
			Kind:    "stale_claims",
			Summary: fmt.Sprintf("%d stale claim%s older than %s", n, plural(n), compactDuration(s.Config.General.StaleAfter.Duration())),
			IDs:     issueIDs(stale),
		})
	}

	blockedHigh, err := s.highPriorityBlockedByUnclaimedReady(ctx)
	if err != nil {
		return nil, err
	}
	if n := len(blockedHigh); n > 0 {
		report.Attention = append(report.Attention, AttentionItem{
			Kind:    "blocked_by_unclaimed_ready",
			Summary: fmt.Sprintf("%d P1-or-higher task%s blocked by unclaimed ready work", n, plural(n)),
			IDs:     issueIDs(blockedHigh),
		})
	}

	report.Health = s.statusHealth(stale, blockedHigh)
	return report, nil
}

func (s *Service) fillCountsFromList(ctx context.Context, counts *StatusCounts) error {
	issues, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), All: true})
	if err != nil {
		return err
	}
	ready, err := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope()})
	if err != nil {
		return err
	}
	readySet := map[string]bool{}
	for _, i := range ready {
		readySet[i.ID] = true
		counts.Ready++
	}
	for _, i := range issues {
		if i.IsClosed() {
			counts.Closed++
			continue
		}
		counts.Open++
		if i.IsInProgress() {
			counts.InProgress++
		}
		if !readySet[i.ID] {
			counts.Blocked++
		}
	}
	return nil
}

func (s *Service) staleClaims(ctx context.Context, now time.Time) ([]domain.Issue, error) {
	issues, err := s.Client.List(ctx, beads.ListQuery{
		Scope:  s.Scope(),
		Status: string(domain.StatusInProgress),
		All:    true,
	})
	if err != nil {
		return nil, err
	}
	cutoff := s.Config.General.StaleAfter.Duration()
	var out []domain.Issue
	for _, i := range issues {
		if i.IsClosed() || i.Assignee == nil || i.Assignee.String() == "" {
			continue
		}
		if i.Idle(now) >= cutoff {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

func (s *Service) highPriorityBlockedByUnclaimedReady(ctx context.Context) ([]domain.Issue, error) {
	blocked, err := s.Client.Blocked(ctx, s.Scope())
	if err != nil {
		return nil, err
	}
	ready, err := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope()})
	if err != nil {
		return nil, err
	}
	unclaimedReady := map[string]bool{}
	for _, i := range ready {
		if i.Assignee == nil || i.Assignee.String() == "" {
			unclaimedReady[i.ID] = true
		}
	}
	var out []domain.Issue
	for _, i := range blocked {
		if i.Priority < 0 || i.Priority > 1 {
			continue
		}
		for _, blocker := range i.BlockedBy {
			if unclaimedReady[blocker] {
				out = append(out, i)
				break
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

func (s *Service) statusHealth(stale, blockedHigh []domain.Issue) domain.Health {
	h := domain.Health{}
	if s.Workspace.BDVersion != "" {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "bd_version",
			Level:   domain.HealthOK,
			Summary: "Beads " + s.Workspace.BDVersion,
		})
	}
	if s.Actor == "" {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "actor",
			Level:   domain.HealthWarning,
			Summary: "Current actor is not configured",
			Hint:    "Set LB_ACTOR or general.actor in config.",
		})
	} else {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "actor",
			Level:   domain.HealthOK,
			Summary: "Actor " + s.Actor,
		})
	}
	if n := len(stale); n > 0 {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "stale_claims",
			Level:   domain.HealthWarning,
			Summary: fmt.Sprintf("%d stale claim%s", n, plural(n)),
			Items:   issueIDs(stale),
		})
	}
	if n := len(blockedHigh); n > 0 {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "high_priority_blocked",
			Level:   domain.HealthWarning,
			Summary: fmt.Sprintf("%d high-priority blocked task%s", n, plural(n)),
			Items:   issueIDs(blockedHigh),
		})
	}
	if len(h.Checks) == 0 {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name:    "workspace",
			Level:   domain.HealthOK,
			Summary: "Workspace is reachable",
		})
	}
	h.Reduce()
	return h
}

func issueIDs(issues []domain.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, i := range issues {
		ids = append(ids, i.ID)
	}
	return ids
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ReadyRequest is the operator-facing filter for `lb ready`.
type ReadyRequest struct {
	PriorityMax *int
	Labels      []string
	Parent      string
	Sort        string
	Limit       int
}

// ReadyItem is one claimable issue plus the impact figure shown beside it.
type ReadyItem struct {
	Issue          domain.Issue `json:"issue"`
	Downstream     int          `json:"downstream"`
	DownstreamKind string       `json:"downstream_kind"`
}

// ReadyResult is the sorted, filtered ready set.
type ReadyResult struct {
	Issues         []ReadyItem `json:"issues"`
	Total          int         `json:"total"`
	Sort           string      `json:"sort"`
	DownstreamKind string      `json:"downstream_kind"`
}

const (
	readySortDefault  = "priority"
	readySortLeverage = "leverage"
	readySortAge      = "age"
	downstreamDirect  = "direct"
)

// Ready returns unblocked work, sorted for a human operator rather than in
// whatever order Beads happened to emit.
func (s *Service) Ready(ctx context.Context, req ReadyRequest) (*ReadyResult, error) {
	sortName := strings.ToLower(strings.TrimSpace(req.Sort))
	switch sortName {
	case "", readySortDefault:
		sortName = readySortDefault
	case readySortLeverage, readySortAge:
	default:
		return nil, &UsageError{Message: fmt.Sprintf("unknown ready sort %q (want priority, leverage or age)", req.Sort)}
	}

	q := beads.ReadyQuery{
		Scope:       s.Scope(),
		PriorityMax: req.PriorityMax,
		Labels:      req.Labels,
		Parent:      req.Parent,
	}
	issues, err := cachedIssues(ctx, s, readyCacheKey(q), func() ([]domain.Issue, error) {
		return s.Client.Ready(ctx, q)
	})
	if err != nil {
		return nil, err
	}

	sortReady(issues, sortName)
	total := len(issues)
	if req.Limit > 0 && len(issues) > req.Limit {
		issues = issues[:req.Limit]
	}

	items := make([]ReadyItem, 0, len(issues))
	for _, issue := range issues {
		items = append(items, ReadyItem{
			Issue:          issue,
			Downstream:     issue.DependentCount,
			DownstreamKind: downstreamDirect,
		})
	}
	return &ReadyResult{
		Issues:         items,
		Total:          total,
		Sort:           sortName,
		DownstreamKind: downstreamDirect,
	}, nil
}

func readyCacheKey(q beads.ReadyQuery) string {
	return fmt.Sprintf("ready:%v:%v:%s", q.PriorityMax, q.Labels, q.Parent)
}

func sortReady(issues []domain.Issue, sortName string) {
	switch sortName {
	case readySortLeverage:
		sort.SliceStable(issues, func(a, b int) bool {
			x, y := issues[a], issues[b]
			if x.DependentCount != y.DependentCount {
				return x.DependentCount > y.DependentCount
			}
			if x.Priority != y.Priority {
				return domainPriority(x.Priority) < domainPriority(y.Priority)
			}
			xc, yc := timeOrZero(x.CreatedAt), timeOrZero(y.CreatedAt)
			if !xc.Equal(yc) {
				return xc.Before(yc)
			}
			return x.ID < y.ID
		})
	case readySortAge:
		sort.SliceStable(issues, func(a, b int) bool {
			x, y := issues[a], issues[b]
			xc, yc := timeOrZero(x.CreatedAt), timeOrZero(y.CreatedAt)
			if !xc.Equal(yc) {
				return xc.Before(yc)
			}
			if x.Priority != y.Priority {
				return domainPriority(x.Priority) < domainPriority(y.Priority)
			}
			return x.ID < y.ID
		})
	default:
		domain.SortIssues(issues)
	}
}

func domainPriority(p domain.Priority) int {
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

// ShowRequest selects optional detail sections.
type ShowRequest struct {
	Events bool
}

// ShowResult is the action-oriented issue view.
type ShowResult struct {
	Detail    domain.IssueDetail `json:"issue"`
	Events    []domain.Event     `json:"events,omitempty"`
	Suggested []string           `json:"suggested_commands"`
	Ready     bool               `json:"ready"`
}

// Show fetches one issue and the commands that make sense in its current state.
func (s *Service) Show(ctx context.Context, id string, req ShowRequest) (*ShowResult, error) {
	detail, err := s.Client.Show(ctx, id, s.Scope())
	if err != nil {
		return nil, err
	}
	result := &ShowResult{
		Detail:    detail,
		Ready:     isClaimable(detail),
		Suggested: suggestCommands(detail),
	}
	if req.Events {
		events, err := s.Client.History(ctx, id, s.Scope())
		if err != nil {
			if ce, ok := beads.AsCommandError(err); ok && ce.Kind == beads.ErrUnsupported {
				result.Events = []domain.Event{}
			} else {
				return nil, err
			}
		} else {
			result.Events = events
		}
	}
	return result, nil
}

func isClaimable(d domain.IssueDetail) bool {
	return len(d.Blockers()) == 0 && !d.IsClosed() && !d.IsDeferred() && !d.IsInProgress()
}

func suggestCommands(d domain.IssueDetail) []string {
	id := d.ID
	var out []string
	switch {
	case d.IsClosed():
		out = append(out, "lb reopen "+id)
	case d.IsInProgress():
		out = append(out, "lb close "+id+" --reason \"...\"")
		out = append(out, "lb unclaim "+id)
	case len(d.Blockers()) > 0:
		out = append(out, "lb why "+id)
		out = append(out, "lb graph "+id)
	default:
		out = append(out, "lb claim "+id)
		out = append(out, "lb why "+id)
	}
	out = append(out, "lb show "+id+" --events")
	return out
}

const (
	defaultListLimit    = 50
	defaultSearchLimit  = 20
	maxSearchCandidates = 1000
)

// ListRequest is the operator-facing filter for `lb list`.
type ListRequest struct {
	Status        string
	Type          string
	Labels        []string
	LabelsAny     []string
	Assignee      string
	Query         string
	CreatedAfter  string
	CreatedBefore string
	UpdatedAfter  string
	UpdatedBefore string
	All           bool
	Limit         int
}

// ListResult is a filtered issue set.
type ListResult struct {
	Issues []domain.Issue `json:"issues"`
	Total  int            `json:"total"`
}

// List forwards compatible Beads constraints, then applies local query/time
// filters and the default safety limit.
func (s *Service) List(ctx context.Context, req ListRequest) (*ListResult, error) {
	assignee, err := s.resolveAssignee(req.Assignee)
	if err != nil {
		return nil, err
	}
	if req.Status == "" {
		if status, query := ParseListFilter(req.Query); status != "" {
			req.Status = status
			req.Query = query
		}
	}
	q := beads.ListQuery{
		Scope:         s.Scope(),
		Status:        req.Status,
		Type:          req.Type,
		Labels:        req.Labels,
		LabelsAny:     req.LabelsAny,
		Assignee:      assignee,
		CreatedAfter:  req.CreatedAfter,
		CreatedBefore: req.CreatedBefore,
		UpdatedAfter:  req.UpdatedAfter,
		UpdatedBefore: req.UpdatedBefore,
		All:           req.All,
	}
	limit := req.Limit
	if limit <= 0 && !req.All {
		limit = defaultListLimit
	}
	fetchLimit := limit
	if req.Query != "" && !req.All {
		fetchLimit = maxSearchCandidates
	}
	q.Limit = fetchLimit

	issues, err := s.Client.List(ctx, q)
	if err != nil {
		return nil, err
	}
	issues = filterLocal(issues, req)
	total := len(issues)
	if limit > 0 && len(issues) > limit {
		issues = issues[:limit]
	}
	return &ListResult{Issues: issues, Total: total}, nil
}

func (s *Service) resolveAssignee(value string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(value), "me") {
		return value, nil
	}
	if s.Actor == "" {
		return "", &UsageError{Message: "--assignee me requires a configured actor; set LB_ACTOR or --actor"}
	}
	return s.Actor, nil
}

func filterLocal(issues []domain.Issue, req ListRequest) []domain.Issue {
	createdAfter, hasCA := parseFlexibleTime(req.CreatedAfter)
	createdBefore, hasCB := parseFlexibleTime(req.CreatedBefore)
	updatedAfter, hasUA := parseFlexibleTime(req.UpdatedAfter)
	updatedBefore, hasUB := parseFlexibleTime(req.UpdatedBefore)
	query := strings.ToLower(strings.TrimSpace(req.Query))

	out := issues[:0]
	for _, issue := range issues {
		if hasCA && (issue.CreatedAt == nil || issue.CreatedAt.Before(createdAfter)) {
			continue
		}
		if hasCB && (issue.CreatedAt == nil || !issue.CreatedAt.Before(createdBefore)) {
			continue
		}
		if hasUA && (issue.UpdatedAt == nil || issue.UpdatedAt.Before(updatedAfter)) {
			continue
		}
		if hasUB && (issue.UpdatedAt == nil || !issue.UpdatedAt.Before(updatedBefore)) {
			continue
		}
		if query != "" && !matchesQuery(issue, query) {
			continue
		}
		out = append(out, issue)
	}
	return out
}

func parseFlexibleTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseListFilter splits a free-text filter into a status constraint and a
// remaining query. Bare tokens like "closed" or "status:open" are status
// filters; anything else is full-text.
func ParseListFilter(raw string) (status, query string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if status, ok := cutStatusFilter(raw); ok {
		return status, ""
	}
	return "", raw
}

func cutStatusFilter(q string) (string, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	for _, prefix := range []string{"status:", "is:"} {
		if rest, ok := strings.CutPrefix(q, prefix); ok {
			return normalizeStatusToken(rest), true
		}
	}
	switch normalizeStatusToken(q) {
	case "open", "closed", "in_progress", "blocked", "deferred":
		return normalizeStatusToken(q), true
	}
	return "", false
}

func normalizeStatusToken(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(strings.ToLower(s)), "-", "_")
}

func matchesQuery(issue domain.Issue, q string) bool {
	// lb-ank
	if status, ok := cutStatusFilter(q); ok {
		return strings.EqualFold(string(issue.Status), status)
	}
	if strings.Contains(strings.ToLower(string(issue.Status)), q) {
		return true
	}
	if strings.Contains(strings.ToLower(issue.ID), q) {
		return true
	}
	if strings.Contains(strings.ToLower(issue.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(issue.Description), q) {
		return true
	}
	for _, l := range issue.Labels {
		if strings.Contains(strings.ToLower(l), q) {
			return true
		}
	}
	if issue.Assignee != nil && strings.Contains(strings.ToLower(issue.Assignee.String()), q) {
		return true
	}
	if issue.ParentID != nil && strings.Contains(strings.ToLower(*issue.ParentID), q) {
		return true
	}
	return false
}

// SearchRequest is the operator-facing filter for `lb search`.
type SearchRequest struct {
	Query string
	All   bool
	Limit int
}

// SearchResult is a ranked discovery list.
type SearchResult struct {
	Issues []domain.Issue `json:"issues"`
	Total  int            `json:"total"`
	Query  string         `json:"query"`
}

// Search ranks currently available issues by how closely they match the query.
func (s *Service) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, &UsageError{Message: "search requires a query; for example: lb search adapter"}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	status, text := ParseListFilter(query)
	all := req.All
	if status != "" {
		all = true
	}

	candidates, err := s.Client.List(ctx, beads.ListQuery{
		Scope:  s.Scope(),
		Status: status,
		All:    all,
		Limit:  maxSearchCandidates,
	})
	if err != nil {
		return nil, err
	}

	type ranked struct {
		issue domain.Issue
		score int
	}
	q := strings.ToLower(text)
	var hits []ranked
	for _, issue := range candidates {
		if status != "" {
			hits = append(hits, ranked{issue: issue, score: 800})
			continue
		}
		score, ok := searchScore(issue, q)
		if !ok {
			continue
		}
		hits = append(hits, ranked{issue: issue, score: score})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].issue.ID < hits[b].issue.ID
	})
	total := len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	issues := make([]domain.Issue, 0, len(hits))
	for _, h := range hits {
		issues = append(issues, h.issue)
	}
	return &SearchResult{Issues: issues, Total: total, Query: query}, nil
}

func searchScore(issue domain.Issue, q string) (int, bool) {
	best := 0
	id := strings.ToLower(issue.ID)
	title := strings.ToLower(issue.Title)
	switch {
	case id == q:
		best = 1000
	case strings.HasPrefix(id, q):
		best = maxScore(best, 900)
	case strings.Contains(id, q):
		best = maxScore(best, 650)
	}
	switch {
	case strings.HasPrefix(title, q):
		best = maxScore(best, 800)
	case strings.Contains(title, q):
		best = maxScore(best, 700)
	}
	for _, l := range issue.Labels {
		if strings.EqualFold(l, q) {
			best = maxScore(best, 600)
		} else if strings.Contains(strings.ToLower(l), q) {
			best = maxScore(best, 550)
		}
	}
	if issue.Assignee != nil && strings.Contains(strings.ToLower(issue.Assignee.String()), q) {
		best = maxScore(best, 500)
	}
	if issue.ParentID != nil && strings.Contains(strings.ToLower(*issue.ParentID), q) {
		best = maxScore(best, 400)
	}
	if strings.Contains(strings.ToLower(issue.Description), q) {
		best = maxScore(best, 100)
	}
	return best, best > 0
}

func maxScore(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// UsageError is a user-facing invocation mistake (exit 2).
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string { return e.Message }

func compactDuration(d time.Duration) string {
	if d < time.Hour {
		m := int(d.Minutes())
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dm", m)
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return d.String()
}
