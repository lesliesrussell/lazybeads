// lb-cn6
package app

import (
	"context"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// fixtureChain builds the dependency shape used throughout the specification:
// lb-3, lb-4 and lb-5 all wait on lb-2, which itself waits on lb-1.
func fixtureChain() *fakeClient {
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-1", Title: "Foundation", Priority: 0})
	f.add(domain.Issue{ID: "lb-2", Title: "Add typed bd adapter", Priority: 1})
	f.add(domain.Issue{ID: "lb-3", Title: "Implement lb status", Priority: 1})
	f.add(domain.Issue{ID: "lb-4", Title: "Implement lb ready", Priority: 1})
	f.add(domain.Issue{ID: "lb-5", Title: "Add shell completion", Priority: 2})
	f.dep("lb-2", "lb-1")
	f.dep("lb-3", "lb-2")
	f.dep("lb-4", "lb-2")
	f.dep("lb-5", "lb-2")
	return f
}

func TestWhyBlockedNamesNearestActionableWork(t *testing.T) {
	svc := newTestService(fixtureChain())

	exp, err := svc.Why(context.Background(), "lb-4", 0, false)
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if exp.Ready {
		t.Error("lb-4 waits on lb-2 and must not be reported ready")
	}
	if !exp.Blocked {
		t.Error("lb-4 should be reported blocked")
	}
	if len(exp.ImmediateBlockers) != 1 || exp.ImmediateBlockers[0].ID != "lb-2" {
		t.Fatalf("immediate blockers = %+v, want just lb-2", exp.ImmediateBlockers)
	}
	// lb-2 is itself blocked by lb-1, so the only actionable root is lb-1.
	if len(exp.RootBlockers) != 1 || exp.RootBlockers[0].ID != "lb-1" {
		t.Fatalf("root blockers = %+v, want lb-1", exp.RootBlockers)
	}
	if !exp.RootBlockers[0].Actionable {
		t.Error("the root blocker should be reported as actionable")
	}
	if got := exp.RootBlockers[0].Path; len(got) != 3 {
		t.Errorf("path = %v, want the full lb-4 → lb-2 → lb-1 chain", got)
	}
}

func TestWhyReadyReportsImpact(t *testing.T) {
	svc := newTestService(fixtureChain())

	exp, err := svc.Why(context.Background(), "lb-1", 0, false)
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if !exp.Ready {
		t.Errorf("lb-1 has no blockers and should be ready: %+v", exp)
	}
	if len(exp.ImmediateBlockers) != 0 {
		t.Errorf("a ready issue must report no blockers, got %+v", exp.ImmediateBlockers)
	}
	if len(exp.Unlocks) != 1 || exp.Unlocks[0] != "lb-2" {
		t.Errorf("unlocks = %v, want the direct dependent lb-2", exp.Unlocks)
	}
	// lb-2 plus the three issues waiting behind it.
	if exp.TransitiveUnlocks != 4 {
		t.Errorf("transitive unlocks = %d, want 4", exp.TransitiveUnlocks)
	}
}

// TestClosedBlockerStopsBlocking covers the core readiness rule: finishing work
// releases everything that was waiting on it.
func TestClosedBlockerStopsBlocking(t *testing.T) {
	f := fixtureChain()
	svc := newTestService(f)

	if _, err := f.Close(context.Background(), "lb-1", closeInput("done")); err != nil {
		t.Fatal(err)
	}
	svc.InvalidateCache()

	exp, err := svc.Why(context.Background(), "lb-2", 0, false)
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if !exp.Ready {
		t.Errorf("lb-2 should be ready once lb-1 is closed: %+v", exp.ImmediateBlockers)
	}
}

// TestNonBlockingRelationsDoNotGateReadiness guards the rule that only blocking
// relations may explain readiness.
func TestNonBlockingRelationsDoNotGateReadiness(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-1", Title: "Root", Priority: 1})
	f.add(domain.Issue{ID: "lb-2", Title: "Related", Priority: 1})
	f.rel("lb-1", "lb-2", domain.RelRelatesTo)

	svc := newTestService(f)
	exp, err := svc.Why(context.Background(), "lb-1", 0, false)
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if !exp.Ready {
		t.Errorf("a relates-to link must not block: %+v", exp.ImmediateBlockers)
	}
	if len(exp.ImmediateBlockers) != 0 {
		t.Errorf("blockers = %+v, want none", exp.ImmediateBlockers)
	}
}

// TestCycleIsReportedNotFatal covers the requirement to warn about cycles and
// keep going rather than hanging or failing.
func TestCycleIsReportedNotFatal(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{ID: "lb-1", Title: "One", Priority: 1})
	f.add(domain.Issue{ID: "lb-2", Title: "Two", Priority: 1})
	f.add(domain.Issue{ID: "lb-3", Title: "Three", Priority: 1})
	f.dep("lb-1", "lb-2")
	f.dep("lb-2", "lb-3")
	f.dep("lb-3", "lb-1")

	svc := newTestService(f)
	done := make(chan struct{})
	var exp *Explanation
	var err error
	go func() {
		exp, err = svc.Why(context.Background(), "lb-1", 0, false)
		close(done)
	}()
	<-done

	if err != nil {
		t.Fatalf("a cycle must not be fatal: %v", err)
	}
	if len(exp.Cycles) == 0 {
		t.Error("the cycle should be reported")
	}
	if len(exp.Warnings) == 0 {
		t.Error("a non-fatal warning should be surfaced")
	}
}

func TestGraphRespectsDepthLimit(t *testing.T) {
	f := newFakeClient()
	// A ten-deep chain, walked with a depth limit of two.
	for i := 1; i <= 10; i++ {
		f.add(domain.Issue{ID: id(i), Title: "node", Priority: 1})
	}
	for i := 1; i < 10; i++ {
		f.dep(id(i), id(i+1))
	}

	svc := newTestService(f)
	graph, err := svc.BuildGraph(context.Background(), GraphRequest{
		RootID: id(1), Direction: DirBlockers, MaxDepth: 2,
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if !graph.Truncated {
		t.Error("hitting the depth limit should be disclosed")
	}
	if graph.Truncation == nil || graph.Truncation.DepthLimit != 2 {
		t.Errorf("truncation = %+v, want the depth limit named", graph.Truncation)
	}
	// Root plus two levels of blockers.
	if len(graph.Nodes) > 3 {
		t.Errorf("visited %d nodes, want at most 3 within depth 2", len(graph.Nodes))
	}
}

func TestGraphRespectsNodeLimit(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{ID: "root", Title: "root", Priority: 1})
	for i := 1; i <= 50; i++ {
		f.add(domain.Issue{ID: id(i), Title: "leaf", Priority: 2})
		f.dep("root", id(i))
	}

	svc := newTestService(f)
	graph, err := svc.BuildGraph(context.Background(), GraphRequest{
		RootID: "root", Direction: DirBlockers, MaxNodes: 10,
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if !graph.Truncated {
		t.Error("hitting the node limit should be disclosed")
	}
	if len(graph.Nodes) > 11 {
		t.Errorf("collected %d nodes despite a limit of 10", len(graph.Nodes))
	}
}

// TestSharedNodesAreDeduplicated covers a diamond, where one node is reachable
// by two paths but must be counted once.
func TestSharedNodesAreDeduplicated(t *testing.T) {
	f := newFakeClient()
	f.add(domain.Issue{ID: "top", Title: "top", Priority: 1})
	f.add(domain.Issue{ID: "left", Title: "left", Priority: 1})
	f.add(domain.Issue{ID: "right", Title: "right", Priority: 1})
	f.add(domain.Issue{ID: "bottom", Title: "bottom", Priority: 1})
	f.dep("top", "left")
	f.dep("top", "right")
	f.dep("left", "bottom")
	f.dep("right", "bottom")

	svc := newTestService(f)
	graph, err := svc.BuildGraph(context.Background(), GraphRequest{RootID: "top", Direction: DirBlockers})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if len(graph.Nodes) != 4 {
		t.Errorf("nodes = %d, want 4 distinct nodes", len(graph.Nodes))
	}
	if graph.Nodes["bottom"].Depth != 2 {
		t.Errorf("shared node depth = %d, want the shortest path (2)", graph.Nodes["bottom"].Depth)
	}

	exp, err := svc.Why(context.Background(), "bottom", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	// left, right and top: each counted once despite two routes to top.
	if exp.TransitiveUnlocks != 3 {
		t.Errorf("transitive unlocks = %d, want 3 distinct issues", exp.TransitiveUnlocks)
	}
}

func TestWhyOnMissingIssueIsTypedError(t *testing.T) {
	svc := newTestService(fixtureChain())
	if _, err := svc.Why(context.Background(), "lb-nope", 0, false); err == nil {
		t.Fatal("an unknown ID should fail")
	}
}

func id(n int) string {
	return "lb-" + string(rune('a'+n%26)) + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
