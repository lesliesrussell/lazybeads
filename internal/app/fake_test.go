// lb-cn6
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// fakeClient is an in-memory Beads stand-in. It models the parts of bd's
// behaviour LazyBeads depends on — the ready set, blocking semantics and
// dependency direction — so the application layer can be tested without a
// database or a subprocess.
type fakeClient struct {
	issues map[string]*domain.Issue
	// edges are blocked -> blocker, mirroring `bd dep add <blocked> <blocker>`.
	edges []edge

	calls    []string
	failShow map[string]error
}

type edge struct {
	blocked string
	blocker string
	relType domain.RelationType
}

func newFakeClient() *fakeClient {
	return &fakeClient{issues: map[string]*domain.Issue{}, failShow: map[string]error{}}
}

func (f *fakeClient) add(issue domain.Issue) *fakeClient {
	if issue.Status == "" {
		issue.Status = domain.StatusOpen
	}
	if issue.CreatedAt == nil {
		created := Now().Add(-time.Hour)
		issue.CreatedAt = &created
	}
	if issue.UpdatedAt == nil {
		issue.UpdatedAt = issue.CreatedAt
	}
	copied := issue
	f.issues[issue.ID] = &copied
	return f
}

// dep records that blocked waits on blocker.
func (f *fakeClient) dep(blocked, blocker string) *fakeClient {
	return f.rel(blocked, blocker, domain.RelBlocks)
}

func (f *fakeClient) rel(blocked, blocker string, t domain.RelationType) *fakeClient {
	f.edges = append(f.edges, edge{blocked: blocked, blocker: blocker, relType: t})
	f.recount()
	return f
}

// recount refreshes the dependency/dependent counters the way bd reports them.
func (f *fakeClient) recount() {
	for _, issue := range f.issues {
		issue.DependencyCount = 0
		issue.DependentCount = 0
	}
	for _, e := range f.edges {
		if i, ok := f.issues[e.blocked]; ok {
			i.DependencyCount++
		}
		if i, ok := f.issues[e.blocker]; ok {
			i.DependentCount++
		}
	}
}

// blockersOf returns the unresolved blocking dependencies of an issue.
func (f *fakeClient) blockersOf(id string) []domain.Dependency {
	var out []domain.Dependency
	for _, e := range f.edges {
		if e.blocked != id {
			continue
		}
		if blocker, ok := f.issues[e.blocker]; ok {
			out = append(out, domain.Dependency{Issue: *blocker, Type: e.relType})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Issue.ID < out[b].Issue.ID })
	return out
}

func (f *fakeClient) dependentsOf(id string) []domain.Dependency {
	var out []domain.Dependency
	for _, e := range f.edges {
		if e.blocker != id {
			continue
		}
		if dependent, ok := f.issues[e.blocked]; ok {
			out = append(out, domain.Dependency{Issue: *dependent, Type: e.relType})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Issue.ID < out[b].Issue.ID })
	return out
}

// isReady mirrors bd's ready semantics: open, and with no unresolved blocking
// dependency. Non-blocking relations never gate readiness.
func (f *fakeClient) isReady(issue *domain.Issue) bool {
	if issue.IsClosed() || issue.IsDeferred() {
		return false
	}
	for _, dep := range f.blockersOf(issue.ID) {
		if dep.Type.Blocking() && !dep.Issue.IsClosed() {
			return false
		}
	}
	return true
}

func (f *fakeClient) sortedIssues() []*domain.Issue {
	out := make([]*domain.Issue, 0, len(f.issues))
	for _, i := range f.issues {
		out = append(out, i)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func (f *fakeClient) record(op string) { f.calls = append(f.calls, op) }

func (f *fakeClient) Version(context.Context, beads.Scope) (beads.VersionInfo, error) {
	return beads.VersionInfo{Version: "1.0.5"}, nil
}

func (f *fakeClient) Info(context.Context, beads.Scope) (beads.Info, error) {
	return beads.Info{BeadsDir: "/fake/.beads", Prefix: "lb", Mode: "direct"}, nil
}

func (f *fakeClient) Ready(_ context.Context, q beads.ReadyQuery) ([]domain.Issue, error) {
	f.record("ready")
	var out []domain.Issue
	for _, issue := range f.sortedIssues() {
		if !f.isReady(issue) {
			continue
		}
		if q.PriorityMax != nil && (issue.Priority < 0 || int(issue.Priority) > *q.PriorityMax) {
			continue
		}
		if len(q.Labels) > 0 && !issue.HasAllLabels(q.Labels) {
			continue
		}
		if len(q.ExcludeLabels) > 0 && issue.HasAnyLabel(q.ExcludeLabels) {
			continue
		}
		if q.Parent != "" && (issue.ParentID == nil || *issue.ParentID != q.Parent) {
			continue
		}
		out = append(out, *issue)
	}
	return out, nil
}

func (f *fakeClient) List(_ context.Context, q beads.ListQuery) ([]domain.Issue, error) {
	f.record("list")
	var out []domain.Issue
	for _, issue := range f.sortedIssues() {
		if !q.All && issue.IsClosed() && q.Status == "" {
			continue
		}
		if q.Status != "" && !strings.EqualFold(string(issue.Status), q.Status) {
			continue
		}
		if q.Type != "" && !strings.EqualFold(string(issue.Type), q.Type) {
			continue
		}
		if len(q.Labels) > 0 && !issue.HasAllLabels(q.Labels) {
			continue
		}
		if len(q.LabelsAny) > 0 && !issue.HasAnyLabel(q.LabelsAny) {
			continue
		}
		if q.Assignee != "" && !issue.Assignee.Equal(q.Assignee) {
			continue
		}
		out = append(out, *issue)
	}
	return out, nil
}

func (f *fakeClient) Show(_ context.Context, id string, _ beads.Scope) (domain.IssueDetail, error) {
	f.record("show:" + id)
	if err, ok := f.failShow[id]; ok {
		return domain.IssueDetail{}, err
	}
	issue, ok := f.issues[id]
	if !ok {
		return domain.IssueDetail{}, &beads.CommandError{
			Kind:      beads.ErrNotFound,
			Operation: "show",
			Cause:     fmt.Errorf("issue %s not found", id),
		}
	}
	return domain.IssueDetail{
		Issue:        *issue,
		Dependencies: f.blockersOf(id),
		Dependents:   f.dependentsOf(id),
	}, nil
}

func (f *fakeClient) Blocked(context.Context, beads.Scope) ([]domain.Issue, error) {
	f.record("blocked")
	var out []domain.Issue
	for _, issue := range f.sortedIssues() {
		if issue.IsClosed() || f.isReady(issue) {
			continue
		}
		copied := *issue
		copied.BlockedBy = nil
		for _, dep := range f.blockersOf(issue.ID) {
			if dep.Type.Blocking() && !dep.Issue.IsClosed() {
				copied.BlockedBy = append(copied.BlockedBy, dep.Issue.ID)
			}
		}
		out = append(out, copied)
	}
	return out, nil
}

func (f *fakeClient) Search(_ context.Context, query string, _ beads.Scope) ([]domain.Issue, error) {
	f.record("search")
	q := strings.ToLower(query)
	var out []domain.Issue
	for _, issue := range f.sortedIssues() {
		if strings.Contains(strings.ToLower(issue.Title), q) ||
			strings.Contains(strings.ToLower(issue.Description), q) ||
			strings.Contains(strings.ToLower(issue.ID), q) {
			out = append(out, *issue)
		}
	}
	return out, nil
}

func (f *fakeClient) Stale(_ context.Context, days int, _ beads.Scope) ([]domain.Issue, error) {
	f.record("stale")
	cutoff := Now().Add(-time.Duration(days) * 24 * time.Hour)
	var out []domain.Issue
	for _, issue := range f.sortedIssues() {
		if issue.UpdatedAt != nil && issue.UpdatedAt.Before(cutoff) && !issue.IsClosed() {
			out = append(out, *issue)
		}
	}
	return out, nil
}

func (f *fakeClient) Stats(context.Context, beads.Scope) (beads.Stats, error) {
	stats := beads.Stats{Available: true}
	for _, issue := range f.issues {
		stats.Total++
		switch {
		case issue.IsClosed():
			stats.Closed++
		case issue.IsInProgress():
			stats.InProgress++
			stats.Open++
		default:
			stats.Open++
		}
		if !issue.IsClosed() {
			if f.isReady(issue) {
				stats.Ready++
			} else {
				stats.Blocked++
			}
		}
	}
	return stats, nil
}

func (f *fakeClient) Create(_ context.Context, in beads.CreateIssueInput) (domain.Issue, error) {
	f.record("create")
	if err := beads.ValidateTitle(in.Title); err != nil {
		return domain.Issue{}, err
	}
	id := fmt.Sprintf("lb-new%d", len(f.issues)+1)
	priority := domain.PriorityUnknown
	if in.Priority != nil {
		priority = domain.Priority(*in.Priority)
	}
	now := Now()
	issue := domain.Issue{
		ID: id, Title: in.Title, Description: in.Description,
		Type: domain.IssueType(in.Type), Status: domain.StatusOpen,
		Priority: priority, Labels: in.Labels, CreatedAt: &now, UpdatedAt: &now,
	}
	f.add(issue)
	return issue, nil
}

func (f *fakeClient) Update(_ context.Context, id string, in beads.UpdateIssueInput) (domain.Issue, error) {
	f.record("update:" + id)
	issue, ok := f.issues[id]
	if !ok {
		return domain.Issue{}, &beads.CommandError{Kind: beads.ErrNotFound, Operation: "update"}
	}
	if in.Title != nil {
		issue.Title = *in.Title
	}
	if in.Description != nil {
		issue.Description = *in.Description
	}
	if in.Priority != nil {
		issue.Priority = domain.Priority(*in.Priority)
	}
	if in.Status != nil {
		issue.Status = domain.IssueStatus(*in.Status)
	}
	if in.Assignee != nil {
		if *in.Assignee == "" {
			issue.Assignee = nil
		} else {
			issue.Assignee = &domain.Actor{Name: *in.Assignee}
		}
	}
	now := Now()
	issue.UpdatedAt = &now
	return *issue, nil
}

func (f *fakeClient) Claim(_ context.Context, id string, in beads.ClaimInput) (domain.Issue, error) {
	f.record("claim:" + id)
	issue, ok := f.issues[id]
	if !ok {
		return domain.Issue{}, &beads.CommandError{Kind: beads.ErrNotFound, Operation: "claim"}
	}
	if issue.IsClosed() {
		return domain.Issue{}, &beads.CommandError{
			Kind: beads.ErrConflict, Operation: "claim",
			Cause: fmt.Errorf("issue %s is closed", id),
		}
	}
	issue.Assignee = &domain.Actor{Name: in.Actor}
	issue.Status = domain.StatusInProgress
	now := Now()
	issue.UpdatedAt = &now
	return *issue, nil
}

func (f *fakeClient) Close(_ context.Context, id string, in beads.CloseInput) (domain.Issue, error) {
	f.record("close:" + id)
	issue, ok := f.issues[id]
	if !ok {
		return domain.Issue{}, &beads.CommandError{Kind: beads.ErrNotFound, Operation: "close"}
	}
	issue.Status = domain.StatusClosed
	now := Now()
	issue.ClosedAt = &now
	issue.UpdatedAt = &now
	reason := in.Reason
	issue.CloseReason = &reason
	return *issue, nil
}

func (f *fakeClient) Reopen(_ context.Context, id string, _ beads.ReopenInput) (domain.Issue, error) {
	f.record("reopen:" + id)
	issue, ok := f.issues[id]
	if !ok {
		return domain.Issue{}, &beads.CommandError{Kind: beads.ErrNotFound, Operation: "reopen"}
	}
	issue.Status = domain.StatusOpen
	issue.ClosedAt = nil
	issue.CloseReason = nil
	return *issue, nil
}

func (f *fakeClient) Assign(_ context.Context, id string, actor string, _ beads.Scope) (domain.Issue, error) {
	f.record("assign:" + id)
	issue, ok := f.issues[id]
	if !ok {
		return domain.Issue{}, &beads.CommandError{Kind: beads.ErrNotFound, Operation: "assign"}
	}
	if actor == "" {
		issue.Assignee = nil
	} else {
		issue.Assignee = &domain.Actor{Name: actor}
	}
	return *issue, nil
}

func (f *fakeClient) AddDependency(_ context.Context, in beads.DependencyInput) error {
	f.record("dep add")
	f.rel(in.Blocked, in.Blocker, in.Type)
	return nil
}

func (f *fakeClient) RemoveDependency(_ context.Context, in beads.DependencyInput) error {
	f.record("dep remove")
	for i, e := range f.edges {
		if e.blocked == in.Blocked && e.blocker == in.Blocker {
			f.edges = append(f.edges[:i], f.edges[i+1:]...)
			f.recount()
			return nil
		}
	}
	return nil
}

func (f *fakeClient) ListDependencies(_ context.Context, id string, dir beads.Direction, _ beads.Scope) ([]domain.Dependency, error) {
	if dir == beads.DirectionUp {
		return f.dependentsOf(id), nil
	}
	return f.blockersOf(id), nil
}

func (f *fakeClient) Cycles(context.Context, beads.Scope) ([][]string, error) {
	var edges []domain.GraphEdge
	for _, e := range f.edges {
		edges = append(edges, domain.GraphEdge{
			FromID: e.blocked, ToID: e.blocker, Type: e.relType, Blocks: e.relType.Blocking(),
		})
	}
	return domain.DetectCycles(edges), nil
}

func (f *fakeClient) Prime(context.Context, beads.Scope) (domain.PrimeResult, error) {
	return domain.PrimeResult{Text: "workflow context"}, nil
}

func (f *fakeClient) Remember(_ context.Context, in beads.RememberInput) (domain.Memory, error) {
	return domain.Memory{Content: in.Content}, nil
}

func (f *fakeClient) Memories(context.Context, string, beads.Scope) ([]domain.Memory, error) {
	return nil, nil
}

func (f *fakeClient) Forget(context.Context, string, beads.Scope) error { return nil }

func (f *fakeClient) History(context.Context, string, beads.Scope) ([]domain.Event, error) {
	return nil, nil
}

func (f *fakeClient) SyncStatus(context.Context, beads.Scope) (domain.SyncStatus, error) {
	return domain.SyncStatus{Available: false, Detail: "no remote configured"}, nil
}

func (f *fakeClient) Capabilities(context.Context, beads.Scope) beads.Capabilities {
	return beads.Capabilities{
		JSONOutput: true, AtomicClaim: true, DependencyRelations: true,
		EventHistory: true, MemoryCommands: true, Reopen: true, Unclaim: true,
		CustomMetadata: true, Version: "1.0.5",
	}
}

func (f *fakeClient) Runner() *beads.Runner { return beads.NewRunner("bd") }

func (f *fakeClient) ArgvFor(_ string, args ...string) []string {
	return append([]string{"bd"}, args...)
}

// newTestService wires a service around a fake client with default config.
func newTestService(f *fakeClient) *Service {
	cfg := config.Default()
	ws := workspace.Workspace{RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.0.5"}
	return NewService(f, cfg, ws, "operator", workspace.NewInProcessLocks())
}

// hoursAgo is a helper for building fixtures with explicit ages.
func hoursAgo(h int) *time.Time {
	t := Now().Add(-time.Duration(h) * time.Hour)
	return &t
}

var _ beads.Client = (*fakeClient)(nil)

// closeInput is a small helper for tests that close an issue directly.
func closeInput(reason string) beads.CloseInput {
	return beads.CloseInput{Reason: reason}
}
