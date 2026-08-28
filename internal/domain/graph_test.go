// lb-1td
package domain

import "testing"

func TestDetectCyclesFindsSimpleLoop(t *testing.T) {
	edges := []GraphEdge{
		{FromID: "a", ToID: "b", Blocks: true},
		{FromID: "b", ToID: "c", Blocks: true},
		{FromID: "c", ToID: "a", Blocks: true},
	}
	cycles := DetectCycles(edges)
	if len(cycles) != 1 {
		t.Fatalf("got %d cycles, want 1: %v", len(cycles), cycles)
	}
	if len(cycles[0]) != 3 {
		t.Errorf("cycle = %v, want three nodes", cycles[0])
	}
}

func TestDetectCyclesIgnoresNonBlockingEdges(t *testing.T) {
	edges := []GraphEdge{
		{FromID: "a", ToID: "b", Type: RelRelatesTo, Blocks: false},
		{FromID: "b", ToID: "a", Type: RelRelatesTo, Blocks: false},
	}
	if got := DetectCycles(edges); len(got) != 0 {
		t.Errorf("relates-to edges must not form a blocking cycle: %v", got)
	}
}

func TestDetectCyclesDeduplicatesRotations(t *testing.T) {
	// The same loop discovered from two entry points is one cycle.
	edges := []GraphEdge{
		{FromID: "a", ToID: "b", Blocks: true},
		{FromID: "b", ToID: "a", Blocks: true},
		{FromID: "z", ToID: "a", Blocks: true},
	}
	if got := DetectCycles(edges); len(got) != 1 {
		t.Errorf("got %d cycles, want 1: %v", len(got), got)
	}
}

func TestDetectCyclesHandlesSelfReference(t *testing.T) {
	edges := []GraphEdge{{FromID: "a", ToID: "a", Blocks: true}}
	cycles := DetectCycles(edges)
	if len(cycles) != 1 || len(cycles[0]) != 1 {
		t.Errorf("self-dependency should be one single-node cycle: %v", cycles)
	}
}

func TestDetectCyclesOnAcyclicGraph(t *testing.T) {
	edges := []GraphEdge{
		{FromID: "a", ToID: "b", Blocks: true},
		{FromID: "a", ToID: "c", Blocks: true},
		{FromID: "b", ToID: "d", Blocks: true},
		{FromID: "c", ToID: "d", Blocks: true},
	}
	if got := DetectCycles(edges); len(got) != 0 {
		t.Errorf("diamond is acyclic, got %v", got)
	}
}

func TestAddEdgeDeduplicates(t *testing.T) {
	g := NewIssueGraph("a")
	e := GraphEdge{FromID: "a", ToID: "b", Type: RelBlocks, Blocks: true}
	g.AddEdge(e)
	g.AddEdge(e)
	if len(g.Edges) != 1 {
		t.Errorf("duplicate edges should collapse, got %d", len(g.Edges))
	}
}

func TestAddNodeKeepsShallowestDepth(t *testing.T) {
	g := NewIssueGraph("a")
	g.AddNode(GraphNode{Issue: Issue{ID: "b"}, Depth: 3})
	g.AddNode(GraphNode{Issue: Issue{ID: "b"}, Depth: 1})
	if g.Nodes["b"].Depth != 1 {
		t.Errorf("depth = %d, want the shallowest path (1)", g.Nodes["b"].Depth)
	}
}

func TestRelationBlocking(t *testing.T) {
	blocking := []RelationType{RelBlocks, "blocks", "BLOCKS", "waits_for"}
	for _, r := range blocking {
		if !r.Blocking() {
			t.Errorf("%q should be blocking", r)
		}
	}
	nonBlocking := []RelationType{RelParentChild, RelRelatesTo, RelDiscoveredFrom,
		RelDuplicates, RelSupersedes, RelRepliesTo, "mystery"}
	for _, r := range nonBlocking {
		if r.Blocking() {
			t.Errorf("%q must not gate readiness", r)
		}
	}
}

func TestSortIssuesDefaultOrder(t *testing.T) {
	issues := []Issue{
		{ID: "c", Priority: 2, DependentCount: 5},
		{ID: "a", Priority: 1, DependentCount: 0},
		{ID: "b", Priority: 1, DependentCount: 3},
		{ID: "d", Priority: PriorityUnknown, DependentCount: 9},
	}
	SortIssues(issues)
	got := []string{issues[0].ID, issues[1].ID, issues[2].ID, issues[3].ID}
	want := []string{"b", "a", "c", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (priority, then impact, then age, then ID)", got, want)
		}
	}
}

func TestHealthReduceTakesWorst(t *testing.T) {
	h := Health{Checks: []HealthCheck{
		{Level: HealthOK}, {Level: HealthWarning}, {Level: HealthInfo},
	}}
	h.Reduce()
	if h.Status != HealthWarning {
		t.Errorf("status = %q, want warning", h.Status)
	}
	h.Checks = append(h.Checks, HealthCheck{Level: HealthError})
	h.Reduce()
	if h.Status != HealthError {
		t.Errorf("status = %q, want error", h.Status)
	}
}
