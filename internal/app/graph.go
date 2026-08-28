// lb-cn6
package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// GraphDirection selects which way traversal walks.
type GraphDirection string

const (
	// DirBlockers walks toward the things that must finish first.
	DirBlockers GraphDirection = "blockers"
	// DirDependents walks toward the things this issue unblocks.
	DirDependents GraphDirection = "dependents"
	// DirBoth walks one level of context in each direction.
	DirBoth GraphDirection = "both"
)

// GraphRequest bounds a traversal.
type GraphRequest struct {
	RootID    string
	Direction GraphDirection
	MaxDepth  int
	MaxNodes  int
}

// BuildGraph walks the dependency graph outward from a root, bounded by depth
// and node count so a pathological graph cannot hang the tool.
//
// Traversal is breadth-first, which guarantees every node is recorded at its
// shortest distance from the root and makes truncation predictable.
func (s *Service) BuildGraph(ctx context.Context, req GraphRequest) (*domain.IssueGraph, error) {
	if req.MaxDepth <= 0 {
		req.MaxDepth = s.Config.Ranking.MaxGraphDepth
	}
	if req.MaxNodes <= 0 {
		req.MaxNodes = s.Config.Ranking.MaxGraphNodes
	}
	if req.Direction == "" {
		req.Direction = DirBoth
	}

	scope := s.Scope()
	root, err := s.Client.Show(ctx, req.RootID, scope)
	if err != nil {
		return nil, err
	}

	graph := domain.NewIssueGraph(root.ID)
	graph.AddNode(nodeFor(root.Issue, 0))

	type queued struct {
		id    string
		depth int
	}
	queue := []queued{{id: root.ID, depth: 0}}
	visited := map[string]bool{root.ID: true}
	// The root's detail is already loaded; avoid fetching it twice.
	details := map[string]domain.IssueDetail{root.ID: root}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= req.MaxDepth {
			// Only claim truncation when this node really does have relations
			// that were never walked, so a complete graph is never reported as
			// partial. The counts come from bd itself, so no extra fetch is
			// needed at the boundary.
			if hasUnexploredEdges(graph, current.id, req.Direction) {
				graph.MarkTruncated(domain.GraphTruncation{
					Reason:      fmt.Sprintf("traversal stopped at the configured depth of %d", req.MaxDepth),
					DepthLimit:  req.MaxDepth,
					VisitedNode: len(visited),
				})
			}
			continue
		}

		detail, ok := details[current.id]
		if !ok {
			detail, err = s.Client.Show(ctx, current.id, scope)
			if err != nil {
				// A node that cannot be read is recorded as a leaf rather than
				// failing the whole traversal.
				continue
			}
			details[current.id] = detail
		}

		var edges []domain.Dependency
		if req.Direction == DirBlockers || req.Direction == DirBoth {
			for _, dep := range detail.Dependencies {
				edges = append(edges, dep)
				graph.AddEdge(domain.GraphEdge{
					FromID: current.id,
					ToID:   dep.Issue.ID,
					Type:   dep.Type,
					Blocks: dep.Type.Blocking(),
				})
			}
		}
		if req.Direction == DirDependents || req.Direction == DirBoth {
			for _, dep := range detail.Dependents {
				edges = append(edges, dep)
				graph.AddEdge(domain.GraphEdge{
					FromID: dep.Issue.ID,
					ToID:   current.id,
					Type:   dep.Type,
					Blocks: dep.Type.Blocking(),
				})
			}
		}

		for _, dep := range edges {
			if dep.Issue.ID == "" {
				continue
			}
			if len(graph.Nodes) >= req.MaxNodes {
				graph.MarkTruncated(domain.GraphTruncation{
					Reason:      fmt.Sprintf("traversal stopped at the configured limit of %d nodes", req.MaxNodes),
					NodeLimit:   req.MaxNodes,
					VisitedNode: len(graph.Nodes),
				})
				break
			}
			graph.AddNode(nodeFor(dep.Issue, current.depth+1))
			if !visited[dep.Issue.ID] {
				visited[dep.Issue.ID] = true
				queue = append(queue, queued{id: dep.Issue.ID, depth: current.depth + 1})
			}
		}
	}

	graph.Cycles = domain.DetectCycles(graph.Edges)
	s.annotateGraph(graph)
	return graph, nil
}

// hasUnexploredEdges reports whether a node has relations that traversal never
// walked, by comparing the counts bd reported against the edges actually
// recorded in the graph.
func hasUnexploredEdges(g *domain.IssueGraph, id string, dir GraphDirection) bool {
	node, ok := g.Nodes[id]
	if !ok {
		return false
	}
	var recordedBlockers, recordedDependents int
	for _, e := range g.Edges {
		if e.FromID == id {
			recordedBlockers++
		}
		if e.ToID == id {
			recordedDependents++
		}
	}
	if dir == DirBlockers || dir == DirBoth {
		if node.DependencyCount > recordedBlockers {
			return true
		}
	}
	if dir == DirDependents || dir == DirBoth {
		if node.Issue.DependentCount > recordedDependents {
			return true
		}
	}
	return false
}

func nodeFor(issue domain.Issue, depth int) domain.GraphNode {
	return domain.GraphNode{
		Issue:            issue,
		DirectDependents: issue.DependentCount,
		Depth:            depth,
	}
}

// annotateGraph fills in readiness and impact for every node using only the
// edges actually present in the traversal.
func (s *Service) annotateGraph(g *domain.IssueGraph) {
	// blockersOf[x] = the open, blocking issues x waits on.
	blockersOf := map[string][]string{}
	dependentsOf := map[string][]string{}
	for _, e := range g.Edges {
		if !e.Blocks {
			continue
		}
		blocker, ok := g.Nodes[e.ToID]
		if ok && blocker.IsClosed() {
			continue
		}
		blockersOf[e.FromID] = append(blockersOf[e.FromID], e.ToID)
		dependentsOf[e.ToID] = append(dependentsOf[e.ToID], e.FromID)
	}

	for id, node := range g.Nodes {
		node.IsBlocked = len(blockersOf[id]) > 0
		node.IsReady = !node.IsBlocked && !node.IsClosed()
		node.IsActionable = node.IsReady
		if direct := dependentsOf[id]; len(direct) > 0 {
			node.DirectDependents = len(direct)
		}
		node.TransitiveImpact = transitiveImpact(id, dependentsOf, g)
		g.Nodes[id] = node
	}
}

// transitiveImpact counts the distinct open issues reachable downstream of id.
// Traversal is bounded by the node set, and each node is counted once even when
// several paths reach it.
func transitiveImpact(id string, dependentsOf map[string][]string, g *domain.IssueGraph) int {
	seen := map[string]bool{id: true}
	queue := append([]string(nil), dependentsOf[id]...)
	count := 0
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if seen[next] {
			continue
		}
		seen[next] = true
		if node, ok := g.Nodes[next]; ok && node.IsClosed() {
			continue
		}
		count++
		queue = append(queue, dependentsOf[next]...)
	}
	return count
}

// Explanation is the answer to "why is this issue actionable, or not?".
type Explanation struct {
	IssueID  string `json:"issue_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	Closed   bool   `json:"closed"`
	Blocked  bool   `json:"blocked"`
	Deferred bool   `json:"deferred"`

	// ImmediateBlockers are the unresolved blocking dependencies.
	ImmediateBlockers []BlockerInfo `json:"immediate_blockers"`
	// RootBlockers are the deepest blockers that are themselves actionable:
	// the work that would actually move this issue forward.
	RootBlockers []BlockerInfo `json:"root_blockers"`
	// Unlocks lists the open issues this one is blocking.
	Unlocks []string `json:"unlocks"`
	// TransitiveUnlocks counts everything downstream, direct or not.
	TransitiveUnlocks int `json:"transitive_unlocks"`

	Cycles    [][]string `json:"cycles,omitempty"`
	Truncated bool       `json:"truncated"`
	Warnings  []string   `json:"warnings,omitempty"`
}

// BlockerInfo describes one blocking issue in operator terms.
type BlockerInfo struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Priority   int      `json:"priority"`
	Assignee   string   `json:"assignee,omitempty"`
	Actionable bool     `json:"actionable"`
	Depth      int      `json:"depth"`
	Path       []string `json:"path,omitempty"`
}

// Why explains an issue's readiness, naming the nearest actionable work.
func (s *Service) Why(ctx context.Context, id string, maxDepth int, allPaths bool) (*Explanation, error) {
	graph, err := s.BuildGraph(ctx, GraphRequest{
		RootID:    id,
		Direction: DirBoth,
		MaxDepth:  maxDepth,
	})
	if err != nil {
		return nil, err
	}

	root, ok := graph.Nodes[graph.RootID]
	if !ok {
		return nil, &beads.CommandError{
			Kind:      beads.ErrNotFound,
			Operation: "why",
			Cause:     fmt.Errorf("issue %s is not present in the resolved graph", id),
		}
	}

	exp := &Explanation{
		IssueID:   root.ID,
		Title:     root.Title,
		Status:    string(root.Status),
		Closed:    root.IsClosed(),
		Deferred:  root.IsDeferred(),
		Cycles:    graph.Cycles,
		Truncated: graph.Truncated,
	}

	for _, e := range graph.Edges {
		if e.FromID != root.ID || !e.Blocks {
			continue
		}
		blocker, ok := graph.Nodes[e.ToID]
		if !ok || blocker.IsClosed() {
			continue
		}
		exp.ImmediateBlockers = append(exp.ImmediateBlockers, blockerInfo(blocker, 1, nil))
	}
	sortBlockers(exp.ImmediateBlockers)

	exp.Blocked = len(exp.ImmediateBlockers) > 0
	exp.Ready = !exp.Blocked && !exp.Closed && !exp.Deferred
	exp.RootBlockers = s.findRootBlockers(graph, root.ID, allPaths)

	for _, e := range graph.Edges {
		if e.ToID != root.ID || !e.Blocks {
			continue
		}
		if dependent, ok := graph.Nodes[e.FromID]; ok && !dependent.IsClosed() {
			exp.Unlocks = append(exp.Unlocks, dependent.ID)
		}
	}
	sort.Strings(exp.Unlocks)
	exp.TransitiveUnlocks = root.TransitiveImpact

	if len(graph.Cycles) > 0 {
		exp.Warnings = append(exp.Warnings,
			fmt.Sprintf("this graph contains %d dependency cycle(s); traversal was bounded to avoid looping", len(graph.Cycles)))
	}
	if graph.Truncated && graph.Truncation != nil {
		exp.Warnings = append(exp.Warnings, graph.Truncation.Reason)
	}
	return exp, nil
}

// findRootBlockers walks the blocking graph from an issue to the deepest
// unresolved blockers that are themselves unblocked. Those are the only items
// an operator can actually start on.
func (s *Service) findRootBlockers(g *domain.IssueGraph, rootID string, allPaths bool) []BlockerInfo {
	blockersOf := map[string][]string{}
	for _, e := range g.Edges {
		if !e.Blocks {
			continue
		}
		if target, ok := g.Nodes[e.ToID]; ok && target.IsClosed() {
			continue
		}
		blockersOf[e.FromID] = append(blockersOf[e.FromID], e.ToID)
	}

	type frame struct {
		id   string
		path []string
	}
	var out []BlockerInfo
	seen := map[string]bool{rootID: true}
	queue := []frame{{id: rootID, path: []string{rootID}}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		next := blockersOf[current.id]
		if current.id != rootID && len(next) == 0 {
			// A blocker with no unresolved blockers of its own is actionable.
			if node, ok := g.Nodes[current.id]; ok {
				out = append(out, blockerInfo(node, len(current.path)-1, current.path))
			}
			continue
		}
		for _, blocker := range next {
			// A cycle would otherwise revisit nodes forever.
			if seen[blocker] && !allPaths {
				continue
			}
			seen[blocker] = true
			path := append(append([]string(nil), current.path...), blocker)
			if len(path) > s.Config.Ranking.MaxGraphDepth+1 {
				continue
			}
			queue = append(queue, frame{id: blocker, path: path})
		}
	}
	sortBlockers(out)
	return out
}

func blockerInfo(node domain.GraphNode, depth int, path []string) BlockerInfo {
	info := BlockerInfo{
		ID:         node.ID,
		Title:      node.Title,
		Status:     string(node.Status),
		Priority:   int(node.Priority),
		Actionable: !node.IsBlocked && !node.IsClosed(),
		Depth:      depth,
		Path:       path,
	}
	if node.Assignee != nil {
		info.Assignee = node.Assignee.String()
	}
	return info
}

// sortBlockers presents the most urgent, shallowest blockers first.
func sortBlockers(items []BlockerInfo) {
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		if x.Depth != y.Depth {
			return x.Depth < y.Depth
		}
		if x.Priority != y.Priority {
			return normalizePriority(x.Priority) < normalizePriority(y.Priority)
		}
		return x.ID < y.ID
	})
}

func normalizePriority(p int) int {
	if p < 0 {
		return 1 << 20
	}
	return p
}
