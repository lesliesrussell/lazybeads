// lb-lh5
package app

import (
	"context"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestFocusPutsClaimedWorkInActiveSection(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	claimedAt := now.Add(-30 * time.Minute)
	f.add(domain.Issue{
		ID: "lb-mine", Title: "My task", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &claimedAt,
	})
	f.add(domain.Issue{
		ID: "lb-theirs", Title: "Someone else", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "other"}, UpdatedAt: &claimedAt,
	})
	f.add(domain.Issue{ID: "lb-ready", Title: "Ready", Priority: 1, UpdatedAt: hoursAgo(1)})

	got, err := newTestService(f).Focus(context.Background(), FocusRequest{})
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if len(got.Active) != 1 || got.Active[0].ID != "lb-mine" {
		t.Errorf("active = %v, want [lb-mine]", issueIDs(got.Active))
	}
	for _, i := range got.Active {
		if i.ID == "lb-theirs" {
			t.Error("another actor's claim must not appear in MY ACTIVE WORK")
		}
	}
}

func TestStaleListsQuietClaimedWork(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	old := now.Add(-10 * time.Hour)
	fresh := now.Add(-time.Hour)
	f.add(domain.Issue{
		ID: "lb-stale", Title: "Abandoned", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &old,
	})
	f.add(domain.Issue{
		ID: "lb-fresh", Title: "Moving", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &fresh,
	})
	got, err := newTestService(f).Stale(context.Background(), StaleRequest{})
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].ID != "lb-stale" {
		t.Errorf("stale = %v, want [lb-stale]", issueIDs(got.Issues))
	}
}

func TestDoctorWarnsWhenActorMissing(t *testing.T) {
	svc := newTestService(newFakeClient())
	svc.Actor = ""
	got, err := svc.Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if got.Health.Status != domain.HealthWarning && got.Health.Status != domain.HealthError {
		t.Errorf("health = %s, want warning when actor is unset", got.Health.Status)
	}
	found := false
	for _, c := range got.Health.Checks {
		if c.Name == "actor" && c.Level == domain.HealthWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("checks = %+v, want an actor warning", got.Health.Checks)
	}
}
