// lb-rd7
package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// NextRequest selects how `lb next` ranks the ready set.
type NextRequest struct {
	Strategy string
	Limit    int
	Parent   string
}

// Factor is one inspectable term in a recommendation score. No factor may
// influence the ranking without appearing here.
type Factor struct {
	Kind         string  `json:"kind"`
	Value        float64 `json:"value"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
	Explanation  string  `json:"explanation"`
}

// Recommendation is one ranked ready issue plus the arithmetic that produced it.
type Recommendation struct {
	Issue    domain.Issue `json:"issue"`
	Strategy string       `json:"strategy"`
	Score    float64      `json:"score"`
	Factors  []Factor     `json:"factors"`
}

// ExcludedIssue records why a ready candidate was dropped.
type ExcludedIssue struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// NextResult is the operator-facing output of `lb next`.
type NextResult struct {
	Recommendation *Recommendation  `json:"recommendation"`
	Alternatives   []Recommendation `json:"alternatives,omitempty"`
	Excluded       []ExcludedIssue  `json:"excluded"`
	BlockedCount   int              `json:"blocked_count"`
	ReadyCount     int              `json:"ready_count"`
}

// Next recommends one or more ready issues and explains the ranking.
func (s *Service) Next(ctx context.Context, req NextRequest) (*NextResult, error) {
	strategy := strings.ToLower(strings.TrimSpace(req.Strategy))
	if strategy == "" {
		strategy = s.Config.Ranking.Strategy
	}
	if strategy == "" {
		strategy = "balanced"
	}
	if !containsString(config.ValidStrategies, strategy) {
		return nil, &UsageError{Message: fmt.Sprintf("unknown strategy %q (want %s)", req.Strategy, strings.Join(config.ValidStrategies, ", "))}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 1
	}

	ready, err := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope(), Parent: req.Parent})
	if err != nil {
		return nil, err
	}

	impact, err := s.readyImpact(ctx)
	if err != nil {
		return nil, err
	}

	var excluded []ExcludedIssue
	var candidates []scoredIssue
	now := Now()
	for _, issue := range ready {
		if reason, ok := s.ineligible(issue, req); ok {
			excluded = append(excluded, ExcludedIssue{ID: issue.ID, Reason: reason})
			continue
		}
		scored := s.scoreIssue(issue, strategy, now, impact)
		candidates = append(candidates, scored)
	}
	sortScored(candidates, strategy)

	result := &NextResult{
		Excluded:     excluded,
		ReadyCount:   len(candidates),
		BlockedCount: impact.blockedCount,
	}
	if len(candidates) == 0 {
		if result.BlockedCount == 0 {
			if stats, err := s.Client.Stats(ctx, s.Scope()); err == nil {
				result.BlockedCount = stats.Blocked
			}
		}
		return result, nil
	}
	recs := make([]Recommendation, 0, min(limit, len(candidates)))
	for i := 0; i < len(candidates) && i < limit; i++ {
		recs = append(recs, candidates[i].Recommendation)
	}
	result.Recommendation = &recs[0]
	if len(recs) > 1 {
		result.Alternatives = recs[1:]
	}
	return result, nil
}

type scoredIssue struct {
	Recommendation
	age      time.Duration
	leverage float64
	shuffle  uint64
}

func (s *Service) ineligible(issue domain.Issue, req NextRequest) (string, bool) {
	if issue.IsClosed() {
		return "closed", true
	}
	if issue.IsDeferred() {
		return "deferred", true
	}
	for _, id := range s.Config.Workspace.IgnoreIssues {
		if issue.ID == id {
			return "ignored by local configuration", true
		}
	}
	if len(s.Config.Workspace.ExcludeLabels) > 0 && issue.HasAnyLabel(s.Config.Workspace.ExcludeLabels) {
		return "excluded by label", true
	}
	parent := req.Parent
	if parent != "" && (issue.ParentID == nil || *issue.ParentID != parent) {
		return "outside requested parent", true
	}
	if issue.Assignee != nil && issue.Assignee.String() != "" && s.Actor != "" && !issue.Assignee.Equal(s.Actor) {
		return "claimed by " + issue.Assignee.String(), true
	}
	return "", false
}

func (s *Service) scoreIssue(issue domain.Issue, strategy string, now time.Time, impact readyImpact) scoredIssue {
	ranked := s.Config.Ranking
	factors := []Factor{}
	var score, leverage float64

	p := issue.Priority.Score()
	age := issue.Age(now)
	ageHours := age.Hours()

	direct, transitive, approx := impact.counts(issue)
	priorityBonus := impact.dependentPriority(issue.ID)
	extra := transitive - direct
	if extra < 0 {
		extra = 0
	}
	leverage = float64(direct) + 0.25*float64(extra) + priorityBonus

	switch strategy {
	case "priority":
		score += addFactor(&factors, "priority", p, ranked.PriorityWeight, issue.Priority.Label()+" priority")
		score += addFactor(&factors, "age", round1(ageHours), ranked.AgeWeight, "age "+outputRelative(age))
	case "leverage":
		if leverage > 0 {
			score += addFactor(&factors, "leverage", round1(leverage), ranked.LeverageWeight, leverageExplain(direct, extra, approx))
		}
		score += addFactor(&factors, "priority", p, ranked.PriorityWeight, issue.Priority.Label()+" priority")
	case "age":
		score += addFactor(&factors, "age", round1(ageHours), 1, "oldest first · "+outputRelative(age))
	case "random":
		score += addFactor(&factors, "shuffle", 1, 1, "deterministic daily shuffle")
	default: // balanced
		score += addFactor(&factors, "priority", p, ranked.PriorityWeight, issue.Priority.Label()+" priority")
		if leverage > 0 {
			score += addFactor(&factors, "leverage", round1(leverage), ranked.LeverageWeight, leverageExplain(direct, extra, approx))
		}
		if ranked.AgeWeight != 0 {
			score += addFactor(&factors, "age", round1(ageHours), ranked.AgeWeight, "age "+outputRelative(age))
		}
		if f, ok := s.focusScore(issue); ok {
			score += addFactor(&factors, "focus", f, ranked.FocusWeight, "matches configured focus")
		}
		if r, ok := recencyScore(issue, now); ok && ranked.RecentlyUnblockedWeight != 0 {
			score += addFactor(&factors, "recently_unblocked", r, ranked.RecentlyUnblockedWeight, "recently updated ready work")
		}
		if c, ok := complexityScore(issue); ok && ranked.ComplexityPenaltyWeight != 0 {
			contrib := round1(c * ranked.ComplexityPenaltyWeight)
			factors = append(factors, Factor{
				Kind: "complexity", Value: c, Weight: ranked.ComplexityPenaltyWeight,
				Contribution: -contrib, Explanation: "complexity penalty from metadata",
			})
			score -= contrib
		}
	}

	return scoredIssue{
		Recommendation: Recommendation{
			Issue:    issue,
			Strategy: strategy,
			Score:    round1(score),
			Factors:  factors,
		},
		age:      age,
		leverage: leverage,
		shuffle:  dailyShuffleKey(issue.ID, now),
	}
}

func addFactor(dst *[]Factor, kind string, value, weight float64, explanation string) float64 {
	contrib := round1(value * weight)
	*dst = append(*dst, Factor{
		Kind:         kind,
		Value:        value,
		Weight:       weight,
		Contribution: contrib,
		Explanation:  explanation,
	})
	return contrib
}

func (s *Service) focusScore(issue domain.Issue) (float64, bool) {
	ws := s.Config.Workspace
	if ws.FocusParent != "" && issue.ParentID != nil && *issue.ParentID == ws.FocusParent {
		return 1, true
	}
	if len(ws.FocusLabels) > 0 && issue.HasAnyLabel(ws.FocusLabels) {
		return 1, true
	}
	return 0, false
}

func recencyScore(issue domain.Issue, now time.Time) (float64, bool) {
	if issue.UpdatedAt == nil || issue.CreatedAt == nil {
		return 0, false
	}
	// Newly created work is already covered by the age factor. Recency is for
	// issues that became ready after a later update (typically an unblocking).
	if !issue.UpdatedAt.After(issue.CreatedAt.Add(time.Minute)) {
		return 0, false
	}
	if issue.Idle(now) > 24*time.Hour {
		return 0, false
	}
	return 1, true
}

func complexityScore(issue domain.Issue) (float64, bool) {
	if issue.Metadata == nil {
		return 0, false
	}
	for _, key := range []string{"complexity", "cost"} {
		if v, ok := issue.Metadata[key]; ok {
			switch n := v.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case string:
				var f float64
				if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

func leverageExplain(direct, extra int, approx bool) string {
	noun := "issue"
	n := direct + extra
	if n != 1 {
		noun = "issues"
	}
	msg := fmt.Sprintf("unlocks %d open %s", n, noun)
	if extra > 0 {
		msg = fmt.Sprintf("unlocks %d open %s (%d direct, %d transitive)", n, noun, direct, extra)
	} else if direct > 0 {
		msg = fmt.Sprintf("directly unlocks %d open %s", direct, noun)
	}
	if approx {
		msg += " (approximated from Beads counts)"
	}
	return msg
}

func outputRelative(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func sortScored(items []scoredIssue, strategy string) {
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		switch strategy {
		case "priority":
			if x.Issue.Priority != y.Issue.Priority {
				return domainPriority(x.Issue.Priority) < domainPriority(y.Issue.Priority)
			}
			if x.age != y.age {
				return x.age > y.age
			}
		case "leverage":
			if x.leverage != y.leverage {
				return x.leverage > y.leverage
			}
			if x.Issue.Priority != y.Issue.Priority {
				return domainPriority(x.Issue.Priority) < domainPriority(y.Issue.Priority)
			}
		case "age":
			if x.age != y.age {
				return x.age > y.age
			}
		case "random":
			if x.shuffle != y.shuffle {
				return x.shuffle < y.shuffle
			}
		default:
			if x.Score != y.Score {
				return x.Score > y.Score
			}
		}
		return x.Issue.ID < y.Issue.ID
	})
}

type readyImpact struct {
	dependentsOf map[string][]string
	byID         map[string]domain.Issue
	blockedCount int
}

func (s *Service) readyImpact(ctx context.Context) (readyImpact, error) {
	blocked, err := s.Client.Blocked(ctx, s.Scope())
	if err != nil {
		return readyImpact{}, err
	}
	imp := readyImpact{
		dependentsOf: map[string][]string{},
		byID:         map[string]domain.Issue{},
		blockedCount: len(blocked),
	}
	for _, issue := range blocked {
		imp.byID[issue.ID] = issue
		for _, blocker := range issue.BlockedBy {
			imp.dependentsOf[blocker] = append(imp.dependentsOf[blocker], issue.ID)
		}
	}
	return imp, nil
}

func (imp readyImpact) counts(issue domain.Issue) (direct, transitive int, approx bool) {
	directIDs := unique(imp.dependentsOf[issue.ID])
	direct = len(directIDs)
	if issue.DependentCount > direct {
		direct = issue.DependentCount
		approx = true
	}
	seen := map[string]bool{issue.ID: true}
	queue := append([]string(nil), imp.dependentsOf[issue.ID]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if other, ok := imp.byID[id]; ok && other.IsClosed() {
			continue
		}
		transitive++
		queue = append(queue, imp.dependentsOf[id]...)
	}
	if transitive < direct {
		transitive = direct
	}
	return direct, transitive, approx
}

func (imp readyImpact) dependentPriority(id string) float64 {
	var sum float64
	for _, depID := range unique(imp.dependentsOf[id]) {
		if issue, ok := imp.byID[depID]; ok && !issue.IsClosed() {
			sum += issue.Priority.Score() / 5
		}
	}
	return sum
}

func unique(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func dailyShuffleKey(id string, now time.Time) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(now.UTC().Format("2006-01-02")))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
