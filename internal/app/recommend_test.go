// lb-rd7
package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestNextRecommendsTheOnlyReadyIssueAndExplainsWhy(t *testing.T) {
	freezeNow(t)
	svc := newTestService(fixtureChain())

	got, err := svc.Next(context.Background(), NextRequest{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation == nil {
		t.Fatal("expected a recommendation")
	}
	rec := got.Recommendation
	if rec.Issue.ID != "lb-1" {
		t.Errorf("recommended %s, want lb-1 (the only ready issue)", rec.Issue.ID)
	}
	if rec.Strategy != "balanced" {
		t.Errorf("strategy = %q, want balanced", rec.Strategy)
	}
	if rec.Score == 0 {
		t.Error("score must be visible")
	}
	if len(rec.Factors) == 0 {
		t.Fatal("every recommendation must explain its factors")
	}
	foundPriority := false
	for _, f := range rec.Factors {
		if f.Kind == "priority" {
			foundPriority = true
			if f.Contribution == 0 {
				t.Error("priority must contribute to the score")
			}
			if f.Explanation == "" {
				t.Error("priority factor needs an explanation")
			}
		}
	}
	if !foundPriority {
		t.Errorf("factors = %+v, want a priority factor", rec.Factors)
	}
}

func TestNextBalancedPrefersP0OverHigherLeverageP2(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-p0", Title: "Critical", Priority: 0, CreatedAt: hoursAgo(1)})
	f.add(domain.Issue{ID: "lb-p2", Title: "Unlocker", Priority: 2, CreatedAt: hoursAgo(1)})
	f.add(domain.Issue{ID: "lb-a", Title: "Waiter A", Priority: 1})
	f.add(domain.Issue{ID: "lb-b", Title: "Waiter B", Priority: 1})
	f.dep("lb-a", "lb-p2")
	f.dep("lb-b", "lb-p2")

	got, err := newTestService(f).Next(context.Background(), NextRequest{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation.Issue.ID != "lb-p0" {
		t.Errorf("recommended %s, want lb-p0 (priority weight dominates leverage)", got.Recommendation.Issue.ID)
	}
}

func TestNextLeverageStrategyPrefersImpact(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-p0", Title: "Critical", Priority: 0})
	f.add(domain.Issue{ID: "lb-p2", Title: "Unlocker", Priority: 2})
	f.add(domain.Issue{ID: "lb-a", Title: "Waiter A", Priority: 1})
	f.add(domain.Issue{ID: "lb-b", Title: "Waiter B", Priority: 1})
	f.dep("lb-a", "lb-p2")
	f.dep("lb-b", "lb-p2")

	got, err := newTestService(f).Next(context.Background(), NextRequest{Strategy: "leverage"})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation.Issue.ID != "lb-p2" {
		t.Errorf("leverage strategy recommended %s, want lb-p2", got.Recommendation.Issue.ID)
	}
}

func TestNextHonorsExcludeLabelsAndIgnoreIDs(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-keep", Title: "Keep", Priority: 2})
	f.add(domain.Issue{ID: "lb-skip", Title: "Parking", Priority: 0, Labels: []string{"parking-lot"}})
	f.add(domain.Issue{ID: "lb-ignore", Title: "Ignored", Priority: 0})
	svc := newTestService(f)
	svc.Config.Workspace.ExcludeLabels = []string{"parking-lot"}
	svc.Config.Workspace.IgnoreIssues = []string{"lb-ignore"}

	got, err := svc.Next(context.Background(), NextRequest{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation.Issue.ID != "lb-keep" {
		t.Errorf("recommended %s, want lb-keep after exclusions", got.Recommendation.Issue.ID)
	}
	if len(got.Excluded) != 2 {
		t.Errorf("excluded = %+v, want two dropped candidates", got.Excluded)
	}
}

func TestNextEmptyReadyReportsBlockedCount(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-1", Title: "Blocked root", Priority: 1})
	f.add(domain.Issue{ID: "lb-2", Title: "Waiter", Priority: 1})
	f.dep("lb-1", "lb-2")
	f.dep("lb-2", "lb-1") // cycle: nothing is ready

	got, err := newTestService(f).Next(context.Background(), NextRequest{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation != nil {
		t.Errorf("recommendation = %+v, want none", got.Recommendation)
	}
	if got.BlockedCount == 0 {
		t.Error("empty ready should still report how much work is blocked")
	}
}

func TestNextRejectsUnknownStrategy(t *testing.T) {
	_, err := newTestService(newFakeClient()).Next(context.Background(), NextRequest{Strategy: "magic"})
	var usage *UsageError
	if err == nil || !errors.As(err, &usage) {
		t.Fatalf("err = %v, want UsageError", err)
	}
}

func TestNextLimitReturnsAlternatives(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-a", Title: "A", Priority: 1})
	f.add(domain.Issue{ID: "lb-b", Title: "B", Priority: 2})
	f.add(domain.Issue{ID: "lb-c", Title: "C", Priority: 3})

	got, err := newTestService(f).Next(context.Background(), NextRequest{Limit: 3})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation.Issue.ID != "lb-a" {
		t.Errorf("first = %s, want lb-a", got.Recommendation.Issue.ID)
	}
	if len(got.Alternatives) != 2 {
		t.Fatalf("alternatives = %d, want 2", len(got.Alternatives))
	}
}

func TestNextRandomIsDeterministicForADay(t *testing.T) {
	freezeNow(t)
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-a", Title: "A", Priority: 1})
	f.add(domain.Issue{ID: "lb-b", Title: "B", Priority: 1})
	f.add(domain.Issue{ID: "lb-c", Title: "C", Priority: 1})
	svc := newTestService(f)

	first, err := svc.Next(context.Background(), NextRequest{Strategy: "random"})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	second, err := svc.Next(context.Background(), NextRequest{Strategy: "random"})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first.Recommendation.Issue.ID != second.Recommendation.Issue.ID {
		t.Errorf("random shuffled %s then %s on the same day", first.Recommendation.Issue.ID, second.Recommendation.Issue.ID)
	}
}

func TestNextLeverageFactorNamesDirectUnlocks(t *testing.T) {
	freezeNow(t)
	svc := newTestService(fixtureChain())
	got, err := svc.Next(context.Background(), NextRequest{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	found := false
	for _, f := range got.Recommendation.Factors {
		if f.Kind != "leverage" {
			continue
		}
		found = true
		if f.Value == 0 || f.Contribution == 0 {
			t.Errorf("leverage factor must contribute, got %+v", f)
		}
		if !strings.Contains(f.Explanation, "unlock") {
			t.Errorf("leverage explanation = %q", f.Explanation)
		}
	}
	if !found {
		t.Errorf("lb-1 unlocks downstream work; factors = %+v", got.Recommendation.Factors)
	}
}

func TestNextRecencyRequiresAnUpdateAfterCreate(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	created := now.Add(-48 * time.Hour)
	updated := now.Add(-time.Hour)
	f.add(domain.Issue{ID: "lb-fresh", Title: "Just filed", Priority: 1, CreatedAt: hoursAgo(2), UpdatedAt: hoursAgo(2)})
	f.add(domain.Issue{ID: "lb-released", Title: "Just unblocked", Priority: 1, CreatedAt: &created, UpdatedAt: &updated})
	svc := newTestService(f)
	got, err := svc.Next(context.Background(), NextRequest{Limit: 2})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Recommendation.Issue.ID != "lb-released" {
		t.Errorf("recommended %s, want lb-released (recently unblocked boost)", got.Recommendation.Issue.ID)
	}
	if !hasFactor(got.Recommendation.Factors, "recently_unblocked") {
		t.Errorf("released issue factors = %+v, want recently_unblocked", got.Recommendation.Factors)
	}
	if len(got.Alternatives) == 1 && hasFactor(got.Alternatives[0].Factors, "recently_unblocked") {
		t.Error("a never-updated new issue must not get a recency boost")
	}
}

func hasFactor(factors []Factor, kind string) bool {
	for _, f := range factors {
		if f.Kind == kind {
			return true
		}
	}
	return false
}
