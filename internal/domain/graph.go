// lb-1td
package domain

import "sort"

// GraphNode is an issue enriched with the readiness and impact facts derived
// from traversal.
type GraphNode struct {
	Issue
	IsReady          bool `json:"is_ready"`
	IsBlocked        bool `json:"is_blocked"`
	IsActionable     bool `json:"is_actionable"`
	DirectDependents int  `json:"direct_dependents"`
	TransitiveImpact int  `json:"transitive_impact"`
	Depth            int  `json:"depth"`
}

// GraphEdge is a directed relation. From depends on / is blocked by To when
// Blocks is true.
type GraphEdge struct {
	FromID string       `json:"from_id"`
	ToID   string       `json:"to_id"`
	Type   RelationType `json:"type"`
	Blocks bool         `json:"blocks"`
}

// GraphTruncation records why traversal stopped, so output can say so honestly
// instead of presenting a partial graph as complete.
type GraphTruncation struct {
	Reason      string `json:"reason"`
	DepthLimit  int    `json:"depth_limit,omitempty"`
	NodeLimit   int    `json:"node_limit,omitempty"`
	VisitedNode int    `json:"visited_nodes,omitempty"`
}

// IssueGraph is a bounded neighbourhood around a root issue.
type IssueGraph struct {
	RootID     string               `json:"root_id"`
	Nodes      map[string]GraphNode `json:"nodes"`
	Edges      []GraphEdge          `json:"edges"`
	Cycles     [][]string           `json:"cycles,omitempty"`
	Truncated  bool                 `json:"truncated"`
	Truncation *GraphTruncation     `json:"truncation,omitempty"`
}

// NewIssueGraph returns an empty graph rooted at id.
func NewIssueGraph(rootID string) *IssueGraph {
	return &IssueGraph{RootID: rootID, Nodes: map[string]GraphNode{}}
}

// AddNode inserts a node, keeping the shallowest depth seen for it.
func (g *IssueGraph) AddNode(n GraphNode) {
	if existing, ok := g.Nodes[n.ID]; ok && existing.Depth <= n.Depth {
		return
	}
	g.Nodes[n.ID] = n
}

// AddEdge appends an edge, skipping exact duplicates so shared nodes in
// multi-path graphs are not double counted.
func (g *IssueGraph) AddEdge(e GraphEdge) {
	for _, existing := range g.Edges {
		if existing == e {
			return
		}
	}
	g.Edges = append(g.Edges, e)
}

// OutEdges returns edges leaving id in a stable order.
func (g *IssueGraph) OutEdges(id string) []GraphEdge {
	var out []GraphEdge
	for _, e := range g.Edges {
		if e.FromID == id {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Type != out[b].Type {
			return out[a].Type < out[b].Type
		}
		return out[a].ToID < out[b].ToID
	})
	return out
}

// MarkTruncated records the first truncation reason encountered.
func (g *IssueGraph) MarkTruncated(t GraphTruncation) {
	if g.Truncated {
		return
	}
	g.Truncated = true
	g.Truncation = &t
}

// DetectCycles returns the distinct cycles reachable through blocking edges.
// It is iterative-free but depth-bounded by the size of the node set, so a
// pathological graph cannot exhaust the stack in practice.
func DetectCycles(edges []GraphEdge) [][]string {
	adj := map[string][]string{}
	nodes := map[string]bool{}
	for _, e := range edges {
		if !e.Blocks {
			continue
		}
		adj[e.FromID] = append(adj[e.FromID], e.ToID)
		nodes[e.FromID] = true
		nodes[e.ToID] = true
	}
	for k := range adj {
		sort.Strings(adj[k])
	}

	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // fully explored
	)
	color := map[string]int{}
	var stack []string
	var cycles [][]string
	seen := map[string]bool{}

	var visit func(string)
	visit = func(n string) {
		color[n] = grey
		stack = append(stack, n)
		for _, next := range adj[n] {
			switch color[next] {
			case white:
				visit(next)
			case grey:
				// Re-slice the current path from the point of re-entry.
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == next {
						cycle := append([]string(nil), stack[i:]...)
						if key := cycleKey(cycle); !seen[key] {
							seen[key] = true
							cycles = append(cycles, cycle)
						}
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}

	ordered := make([]string, 0, len(nodes))
	for n := range nodes {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)
	for _, n := range ordered {
		if color[n] == white {
			visit(n)
		}
	}
	return cycles
}

// cycleKey produces a rotation-independent identity for a cycle so the same
// loop discovered from different entry points is reported once.
func cycleKey(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	min := 0
	for i, v := range cycle {
		if v < cycle[min] {
			min = i
		}
	}
	key := ""
	for i := range cycle {
		key += cycle[(min+i)%len(cycle)] + ">"
	}
	return key
}
