// lb-rc1
package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestClaimRefusesClosedWork(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-done", Title: "Shipped", Status: domain.StatusClosed, Priority: 2})

	_, err := newTestService(f).Claim(context.Background(), "lb-done", MutateOptions{Actor: "operator"})
	if err == nil {
		t.Fatal("claiming closed work must fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "closed") {
		t.Errorf("err = %v, want a closed-work diagnosis", err)
	}
	if calls := f.calls; containsCall(calls, "claim:lb-done") {
		t.Errorf("bd claim must not run for closed work, calls=%v", calls)
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestClaimDryRunDoesNotCallBeads(t *testing.T) {
	f := fixtureChain()
	got, err := newTestService(f).Claim(context.Background(), "lb-1", MutateOptions{DryRun: true, Actor: "operator"})
	if err != nil {
		t.Fatalf("Claim dry-run: %v", err)
	}
	if !got.DryRun {
		t.Fatal("expected dry-run result")
	}
	if len(got.Argv) == 0 || got.Argv[0] != "bd" {
		t.Errorf("argv = %v, want a bd command line", got.Argv)
	}
	if containsCall(f.calls, "claim:lb-1") {
		t.Error("dry-run must not execute bd claim")
	}
}

func TestClaimSetsActorAndStatus(t *testing.T) {
	f := fixtureChain()
	got, err := newTestService(f).Claim(context.Background(), "lb-1", MutateOptions{Actor: "operator"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Issue.Status != domain.StatusInProgress {
		t.Errorf("status = %s, want in_progress", got.Issue.Status)
	}
	if got.Issue.Assignee == nil || got.Issue.Assignee.Name != "operator" {
		t.Errorf("assignee = %+v", got.Issue.Assignee)
	}
}

func TestCloseRequiresReasonAndReportsNewlyReady(t *testing.T) {
	svc := newTestService(fixtureChain())
	_, err := svc.Close(context.Background(), "lb-1", "  ", MutateOptions{})
	var usage *UsageError
	if err == nil || !errors.As(err, &usage) {
		t.Fatalf("err = %v, want UsageError for empty reason", err)
	}

	got, err := svc.Close(context.Background(), "lb-1", "foundation landed", MutateOptions{})
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got.Issue.Status != domain.StatusClosed {
		t.Errorf("status = %s", got.Issue.Status)
	}
	found := false
	for _, i := range got.NewlyReady {
		if i.ID == "lb-2" {
			found = true
		}
	}
	if !found {
		t.Errorf("newly ready = %v, want lb-2 once lb-1 closes", issueIDs(got.NewlyReady))
	}
}

func TestDepAddNeverReordersAndRejectsSelf(t *testing.T) {
	svc := newTestService(fixtureChain())
	_, err := svc.DepAdd(context.Background(), "lb-1", "lb-1", domain.RelBlocks, MutateOptions{})
	var usage *UsageError
	if err == nil || !errors.As(err, &usage) {
		t.Fatalf("self-edge err = %v, want UsageError", err)
	}

	got, err := svc.DepAdd(context.Background(), "lb-1", "lb-2", domain.RelBlocks, MutateOptions{DryRun: true})
	if err != nil {
		t.Fatalf("DepAdd: %v", err)
	}
	joined := strings.Join(got.Argv, " ")
	// child then parent: blocked waits on blocker. Never swapped.
	if !strings.Contains(joined, "dep add lb-1 lb-2") {
		t.Errorf("argv = %v, want `dep add lb-1 lb-2` in that order", got.Argv)
	}
}

func TestCreateRejectsEmptyTitle(t *testing.T) {
	_, err := newTestService(newFakeClient()).Create(context.Background(), beads.CreateIssueInput{Title: "  "}, MutateOptions{})
	if err == nil {
		t.Fatal("empty title must fail")
	}
}

func TestUnclaimClearsAssigneeWhenSupported(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{
		ID: "lb-wip", Title: "In flight", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"},
	})
	got, err := newTestService(f).Unclaim(context.Background(), "lb-wip", MutateOptions{})
	if err != nil {
		t.Fatalf("Unclaim: %v", err)
	}
	if got.Issue.Assignee != nil && got.Issue.Assignee.String() != "" {
		t.Errorf("assignee still set: %+v", got.Issue.Assignee)
	}
	if got.Issue.Status != domain.StatusOpen {
		t.Errorf("status = %s, want open", got.Issue.Status)
	}
}
