// lb-58x
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func freezeNow(t *testing.T) time.Time {
	t.Helper()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return now }
	t.Cleanup(func() { Now = time.Now })
	return now
}

func TestStatusCountsReadyOpenBlockedAndClosed(t *testing.T) {
	freezeNow(t)
	f := fixtureChain()
	closed := domain.Issue{ID: "lb-done", Title: "Shipped", Status: domain.StatusClosed, Priority: 2}
	f.add(closed)

	report, err := newTestService(f).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if report.Counts.Ready != 1 {
		t.Errorf("ready = %d, want 1 (only lb-1 is unblocked)", report.Counts.Ready)
	}
	if report.Counts.Blocked != 4 {
		t.Errorf("blocked = %d, want 4", report.Counts.Blocked)
	}
	if report.Counts.Closed != 1 {
		t.Errorf("closed = %d, want 1", report.Counts.Closed)
	}
	if report.Counts.Open != 5 {
		t.Errorf("open = %d, want 5 (the fixture chain, excluding closed)", report.Counts.Open)
	}
	if report.Health.Status == "" {
		t.Error("health status must be reduced from the check list")
	}
}

func TestStatusFlagsStaleClaims(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{
		ID: "lb-fresh", Title: "Active work", Status: domain.StatusInProgress, Priority: 1,
		Assignee:  &domain.Actor{Name: "operator"},
		UpdatedAt: hoursAgo(1),
	})
	staleAt := now.Add(-10 * time.Hour)
	f.add(domain.Issue{
		ID: "lb-stale", Title: "Abandoned work", Status: domain.StatusInProgress, Priority: 1,
		Assignee:  &domain.Actor{Name: "operator"},
		UpdatedAt: &staleAt,
	})

	report, err := newTestService(f).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	found := false
	for _, item := range report.Attention {
		if item.Kind == "stale_claims" {
			found = true
			if len(item.IDs) != 1 || item.IDs[0] != "lb-stale" {
				t.Errorf("stale ids = %v, want [lb-stale]", item.IDs)
			}
		}
	}
	if !found {
		t.Fatalf("expected a stale_claims attention item, got %+v", report.Attention)
	}
	if report.Health.Status != domain.HealthWarning {
		t.Errorf("health = %s, want warning", report.Health.Status)
	}
}

func TestStatusFlagsHighPriorityBlockedByUnclaimedReady(t *testing.T) {
	freezeNow(t)
	f := fixtureChain()
	report, err := newTestService(f).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	found := false
	for _, item := range report.Attention {
		if item.Kind != "blocked_by_unclaimed_ready" {
			continue
		}
		found = true
		if len(item.IDs) == 0 {
			t.Error("blocked-by-unclaimed-ready must name the blocked issues")
		}
	}
	if !found {
		t.Fatalf("lb-1 is ready and unclaimed and blocks P1 work; expected attention, got %+v", report.Attention)
	}
}

func readyFixture() *fakeClient {
	f := newFakeClient()
	parent := "lb-epic"
	f.add(domain.Issue{ID: "lb-p0", Title: "Critical", Priority: 0, CreatedAt: hoursAgo(2)})
	f.add(domain.Issue{ID: "lb-p1-impact", Title: "Adapter", Priority: 1, Labels: []string{"cli", "mvp"}, CreatedAt: hoursAgo(4)})
	f.add(domain.Issue{ID: "lb-p1-age", Title: "Oldest P1", Priority: 1, CreatedAt: hoursAgo(48)})
	f.add(domain.Issue{ID: "lb-p2", Title: "Shell completion", Priority: 2, Labels: []string{"cli"}, ParentID: &parent, CreatedAt: hoursAgo(3)})
	f.add(domain.Issue{ID: "lb-blocked", Title: "Blocked child", Priority: 1, CreatedAt: hoursAgo(1)})
	f.dep("lb-blocked", "lb-p1-impact")
	f.dep("lb-x", "lb-p1-impact") // recount still bumps dependent count even if x is missing
	return f
}

func TestReadyOrdersByPriorityThenImpactThenAge(t *testing.T) {
	freezeNow(t)
	result, err := newTestService(readyFixture()).Ready(context.Background(), ReadyRequest{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	got := idsOf(result.Issues)
	// lb-blocked is not ready. Among ready: P0 first, then the P1 with more
	// downstream impact, then the older remaining P1, then P2.
	want := []string{"lb-p0", "lb-p1-impact", "lb-p1-age", "lb-p2"}
	if len(got) != len(want) {
		t.Fatalf("ready ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ready[%d] = %s, want %s (full=%v)", i, got[i], want[i], got)
		}
	}
	if result.DownstreamKind != "direct" {
		t.Errorf("downstream kind = %q, want direct (bd's dependent count)", result.DownstreamKind)
	}
}

func idsOf(items []ReadyItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Issue.ID
	}
	return out
}

func TestReadyAppliesPriorityLabelParentAndLimit(t *testing.T) {
	freezeNow(t)
	svc := newTestService(readyFixture())
	one := 1
	got, err := svc.Ready(context.Background(), ReadyRequest{PriorityMax: &one, Labels: []string{"cli"}, Limit: 1})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("total after filters = %d, want 1 (only lb-p1-impact is P<=1 and labelled cli)", got.Total)
	}
	if len(got.Issues) != 1 || got.Issues[0].Issue.ID != "lb-p1-impact" {
		t.Errorf("issues = %v, want [lb-p1-impact]", idsOf(got.Issues))
	}

	parent, err := svc.Ready(context.Background(), ReadyRequest{Parent: "lb-epic"})
	if err != nil {
		t.Fatalf("Ready parent: %v", err)
	}
	if idsOf(parent.Issues) == nil || len(parent.Issues) != 1 || parent.Issues[0].Issue.ID != "lb-p2" {
		t.Errorf("parent filter = %v, want [lb-p2]", idsOf(parent.Issues))
	}
}

func TestReadyLeverageSortPrefersImpactOverPriority(t *testing.T) {
	freezeNow(t)
	got, err := newTestService(readyFixture()).Ready(context.Background(), ReadyRequest{Sort: "leverage"})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if got.Issues[0].Issue.ID != "lb-p1-impact" {
		t.Errorf("leverage winner = %s, want lb-p1-impact (highest downstream)", got.Issues[0].Issue.ID)
	}
}

func TestReadyRejectsUnknownSort(t *testing.T) {
	_, err := newTestService(newFakeClient()).Ready(context.Background(), ReadyRequest{Sort: "magic"})
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want UsageError", err)
	}
}

func TestShowReadyIssueNamesBlockersDependentsAndNextCommands(t *testing.T) {
	freezeNow(t)
	svc := newTestService(fixtureChain())
	got, err := svc.Show(context.Background(), "lb-1", ShowRequest{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got.Detail.ID != "lb-1" {
		t.Fatalf("id = %s", got.Detail.ID)
	}
	if len(got.Detail.Blockers()) != 0 {
		t.Errorf("ready issue must have no open blockers, got %+v", got.Detail.Blockers())
	}
	if len(got.Detail.Dependents) != 1 || got.Detail.Dependents[0].Issue.ID != "lb-2" {
		t.Errorf("dependents = %+v, want lb-2", got.Detail.Dependents)
	}
	joined := strings.Join(got.Suggested, "\n")
	for _, want := range []string{"lb claim lb-1", "lb why lb-1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("suggested commands %v missing %q", got.Suggested, want)
		}
	}
}

func TestShowBlockedIssueSuggestsWhy(t *testing.T) {
	got, err := newTestService(fixtureChain()).Show(context.Background(), "lb-4", ShowRequest{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(got.Detail.Blockers()) != 1 || got.Detail.Blockers()[0].Issue.ID != "lb-2" {
		t.Errorf("blockers = %+v, want lb-2", got.Detail.Blockers())
	}
	joined := strings.Join(got.Suggested, "\n")
	if !strings.Contains(joined, "lb why lb-4") {
		t.Errorf("blocked issue should suggest why, got %v", got.Suggested)
	}
}

func TestShowInProgressIssueIsNotClaimable(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{
		ID: "lb-wip", Title: "In flight", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"},
	})
	got, err := newTestService(f).Show(context.Background(), "lb-wip", ShowRequest{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got.Ready {
		t.Error("an in-progress issue is owned work, not claimable ready work")
	}
	joined := strings.Join(got.Suggested, "\n")
	if !strings.Contains(joined, "lb close lb-wip") {
		t.Errorf("in-progress suggestions = %v, want close", got.Suggested)
	}
}

func TestShowMissingIssue(t *testing.T) {
	_, err := newTestService(newFakeClient()).Show(context.Background(), "nope", ShowRequest{})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	ce, ok := beads.AsCommandError(err)
	if !ok || ce.Kind != beads.ErrNotFound {
		t.Errorf("err = %v, want issue_not_found", err)
	}
}

func listFixture() *fakeClient {
	f := newFakeClient()
	created := hoursAgo(24 * 10)
	recent := hoursAgo(2)
	parent := "lb-epic"
	f.add(domain.Issue{
		ID: "lb-a", Title: "Schema mismatch", Type: domain.TypeBug, Status: domain.StatusOpen,
		Priority: 1, Labels: []string{"cli", "compat"}, Assignee: &domain.Actor{Name: "operator"},
		CreatedAt: created, UpdatedAt: recent, ParentID: &parent,
		Description: "Provide an explicit LazyBeads error.",
	})
	f.add(domain.Issue{
		ID: "lb-b", Title: "Add TUI", Type: domain.TypeTask, Status: domain.StatusOpen,
		Priority: 2, Labels: []string{"ui"}, CreatedAt: recent, UpdatedAt: recent,
	})
	f.add(domain.Issue{
		ID: "lb-c", Title: "Old chore", Type: domain.TypeChore, Status: domain.StatusClosed,
		Priority: 3, Labels: []string{"cli"}, CreatedAt: created, UpdatedAt: created,
	})
	return f
}

func TestListANDLabelsExcludeClosedAndHonorQuery(t *testing.T) {
	freezeNow(t)
	svc := newTestService(listFixture())

	labelled, err := svc.List(context.Background(), ListRequest{Labels: []string{"cli", "compat"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(labelled.Issues) != 1 || labelled.Issues[0].ID != "lb-a" {
		t.Errorf("AND labels = %v, want [lb-a]", issueIDs(labelled.Issues))
	}

	any, err := svc.List(context.Background(), ListRequest{LabelsAny: []string{"ui", "compat"}})
	if err != nil {
		t.Fatalf("List any: %v", err)
	}
	if len(any.Issues) != 2 {
		t.Errorf("OR labels = %v, want lb-a and lb-b", issueIDs(any.Issues))
	}

	closed, err := svc.List(context.Background(), ListRequest{})
	if err != nil {
		t.Fatalf("List default: %v", err)
	}
	for _, i := range closed.Issues {
		if i.IsClosed() {
			t.Errorf("default list leaked closed issue %s", i.ID)
		}
	}

	all, err := svc.List(context.Background(), ListRequest{All: true})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all.Issues) != 3 {
		t.Errorf("all = %v, want 3 including closed", issueIDs(all.Issues))
	}

	queried, err := svc.List(context.Background(), ListRequest{Query: "schema"})
	if err != nil {
		t.Fatalf("List query: %v", err)
	}
	if len(queried.Issues) != 1 || queried.Issues[0].ID != "lb-a" {
		t.Errorf("query = %v, want [lb-a]", issueIDs(queried.Issues))
	}

	byStatus, err := svc.List(context.Background(), ListRequest{Query: "closed"})
	if err != nil {
		t.Fatalf("List query closed: %v", err)
	}
	if len(byStatus.Issues) != 1 || byStatus.Issues[0].ID != "lb-c" {
		t.Errorf("query closed = %v, want [lb-c]", issueIDs(byStatus.Issues))
	}
	prefixed, err := svc.List(context.Background(), ListRequest{Query: "status:open", All: true})
	if err != nil {
		t.Fatalf("List status:open: %v", err)
	}
	for _, i := range prefixed.Issues {
		if i.IsClosed() {
			t.Errorf("status:open leaked closed %s", i.ID)
		}
	}
}

func TestListAssigneeMeRequiresActor(t *testing.T) {
	freezeNow(t)
	f := listFixture()
	svc := newTestService(f)
	got, err := svc.List(context.Background(), ListRequest{Assignee: "me"})
	if err != nil {
		t.Fatalf("List me: %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].ID != "lb-a" {
		t.Errorf("assignee me = %v, want [lb-a]", issueIDs(got.Issues))
	}

	svc.Actor = ""
	_, err = svc.List(context.Background(), ListRequest{Assignee: "me"})
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want UsageError when actor is unset", err)
	}
}

func TestSearchRanksExactIDThenTitlePrefixThenDescription(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-adapter", Title: "Something else", Description: "mentions adapter in body", Priority: 2})
	f.add(domain.Issue{ID: "lb-other", Title: "adapter helpers", Priority: 2})
	f.add(domain.Issue{ID: "adapter", Title: "Exact id match", Priority: 2})

	got, err := newTestService(f).Search(context.Background(), SearchRequest{Query: "adapter"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	ids := issueIDs(got.Issues)
	if len(ids) < 3 {
		t.Fatalf("results = %v, want at least the three fixtures", ids)
	}
	if ids[0] != "adapter" {
		t.Errorf("first = %s, want exact ID match", ids[0])
	}
	if ids[1] != "lb-other" {
		t.Errorf("second = %s, want title prefix match", ids[1])
	}
	if ids[2] != "lb-adapter" {
		t.Errorf("third = %s, want description-only match last", ids[2])
	}
}

func TestSearchEmptyQueryIsUsageError(t *testing.T) {
	_, err := newTestService(newFakeClient()).Search(context.Background(), SearchRequest{Query: "  "})
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want UsageError", err)
	}
}

func TestSearchIsCaseInsensitiveAndLimits(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	for i := 0; i < 25; i++ {
		f.add(domain.Issue{ID: fmt.Sprintf("lb-%02d", i), Title: "Dolt sync " + fmt.Sprintf("%02d", i), Priority: 2})
	}
	got, err := newTestService(f).Search(context.Background(), SearchRequest{Query: "dolt"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Issues) != 20 {
		t.Errorf("default limit = %d, want 20", len(got.Issues))
	}
	if got.Total != 25 {
		t.Errorf("total = %d, want 25", got.Total)
	}
}
