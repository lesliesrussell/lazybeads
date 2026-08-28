// lb-rc1
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// MutateOptions are the non-interactive controls shared by every write.
type MutateOptions struct {
	DryRun bool
	Actor  string
}

// MutationResult is the confirmed outcome of a write, or the argv a dry-run
// would have executed.
type MutationResult struct {
	Action     string         `json:"action"`
	Issue      domain.Issue   `json:"issue,omitempty"`
	Before     *domain.Issue  `json:"before,omitempty"`
	Argv       []string       `json:"argv"`
	DryRun     bool           `json:"dry_run"`
	Warnings   []string       `json:"warnings,omitempty"`
	NewlyReady []domain.Issue `json:"newly_ready,omitempty"`
	Message    string         `json:"message"`
}

// DeclinedError means the operator cancelled a confirmation prompt.
type DeclinedError struct{}

func (e *DeclinedError) Error() string { return "confirmation declined" }

func (s *Service) actor(opts MutateOptions) string {
	if opts.Actor != "" {
		return opts.Actor
	}
	return s.Actor
}

func (s *Service) withWrite(ctx context.Context, fn func(context.Context) error) error {
	return s.Locks.WithWriteLock(ctx, s.Workspace.ID(), fn)
}

func (s *Service) showIssue(ctx context.Context, id string) (domain.IssueDetail, error) {
	return s.Client.Show(ctx, id, s.Scope())
}

// Claim atomically takes ownership of an issue through Beads.
func (s *Service) Claim(ctx context.Context, id string, opts MutateOptions) (*MutationResult, error) {
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if before.IsClosed() {
		return nil, &beads.CommandError{
			Kind:      beads.ErrConflict,
			Operation: "claim",
			Cause:     fmt.Errorf("issue %s is closed", id),
			Hint:      "Reopen it before claiming.",
		}
	}
	actor := s.actor(opts)
	result := &MutationResult{
		Action:  "claim",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("claim", beads.ClaimArgs(id, actor)...),
		Message: fmt.Sprintf("Claim %s", id),
	}
	if before.Assignee != nil && before.Assignee.String() != "" && actor != "" && !before.Assignee.Equal(actor) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("already claimed by %s", before.Assignee.String()))
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	var claimed domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		claimed, e = s.Client.Claim(ctx, id, beads.ClaimInput{Scope: s.Scope(), Actor: actor})
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	after, err := s.showIssue(ctx, id)
	if err == nil {
		claimed = after.Issue
	}
	result.Issue = claimed
	result.Message = fmt.Sprintf("Claimed %s", claimed.ID)
	return result, nil
}

// Unclaim releases active ownership using the Beads-supported update path.
func (s *Service) Unclaim(ctx context.Context, id string, opts MutateOptions) (*MutationResult, error) {
	caps := s.Client.Capabilities(ctx, s.Scope())
	if !caps.Unclaim {
		return nil, beads.UnsupportedError("unclaim", "unclaim",
			"This Beads build has no supported unclaim. Try `bd update "+id+" --assignee \"\" --status open` after upgrading, or wait for a dedicated flag.")
	}
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	empty := ""
	open := string(domain.StatusOpen)
	in := beads.UpdateIssueInput{Scope: s.Scope(), Assignee: &empty, Status: &open}
	result := &MutationResult{
		Action:  "unclaim",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("unclaim", beads.UpdateArgs(id, in, s.actor(opts))...),
		Message: fmt.Sprintf("Unclaim %s", id),
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	var issue domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		issue, e = s.Client.Update(ctx, id, in)
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, id); e == nil {
		issue = after.Issue
	}
	result.Issue = issue
	result.Message = fmt.Sprintf("Unclaimed %s", issue.ID)
	return result, nil
}

// Close finishes an issue and reports work that became ready.
func (s *Service) Close(ctx context.Context, id, reason string, opts MutateOptions) (*MutationResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &UsageError{Message: "a close reason is required"}
	}
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	actor := s.actor(opts)
	result := &MutationResult{
		Action:  "close",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("close", beads.CloseArgs(id, reason, actor)...),
		Message: fmt.Sprintf("Close %s", id),
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	readyBefore, _ := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope()})
	beforeSet := map[string]bool{}
	for _, i := range readyBefore {
		beforeSet[i.ID] = true
	}
	var closed domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		closed, e = s.Client.Close(ctx, id, beads.CloseInput{Scope: s.Scope(), Reason: reason, Actor: actor})
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, id); e == nil {
		closed = after.Issue
	}
	readyAfter, _ := s.Client.Ready(ctx, beads.ReadyQuery{Scope: s.Scope()})
	for _, i := range readyAfter {
		if !beforeSet[i.ID] && i.ID != id {
			result.NewlyReady = append(result.NewlyReady, i)
		}
	}
	result.Issue = closed
	result.Message = fmt.Sprintf("Closed %s", closed.ID)
	return result, nil
}

// Reopen restores a closed issue. A reason is required.
func (s *Service) Reopen(ctx context.Context, id, reason string, opts MutateOptions) (*MutationResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &UsageError{Message: "a reopen reason is required"}
	}
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	actor := s.actor(opts)
	result := &MutationResult{
		Action:  "reopen",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("reopen", beads.ReopenArgs(id, actor)...),
		Message: fmt.Sprintf("Reopen %s", id),
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	var issue domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		issue, e = s.Client.Reopen(ctx, id, beads.ReopenInput{Scope: s.Scope(), Reason: reason, Actor: actor})
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, id); e == nil {
		issue = after.Issue
	}
	result.Issue = issue
	result.Message = fmt.Sprintf("Reopened %s", issue.ID)
	return result, nil
}

// Create inserts a new Beads issue.
func (s *Service) Create(ctx context.Context, in beads.CreateIssueInput, opts MutateOptions) (*MutationResult, error) {
	if err := beads.ValidateTitle(in.Title); err != nil {
		return nil, err
	}
	in.Scope = s.Scope()
	actor := s.actor(opts)
	result := &MutationResult{
		Action:  "create",
		Argv:    s.Client.ArgvFor("create", beads.CreateArgs(in, actor)...),
		Message: "Create issue",
	}
	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}
	var issue domain.Issue
	err := s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		issue, e = s.Client.Create(ctx, in)
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	result.Issue = issue
	result.Message = fmt.Sprintf("Created %s", issue.ID)
	return result, nil
}

// Edit applies a partial update and re-reads the confirmed state.
func (s *Service) Edit(ctx context.Context, id string, in beads.UpdateIssueInput, opts MutateOptions) (*MutationResult, error) {
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Scope = s.Scope()
	actor := s.actor(opts)
	result := &MutationResult{
		Action:  "edit",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("edit", beads.UpdateArgs(id, in, actor)...),
		Message: fmt.Sprintf("Edit %s", id),
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	var issue domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		issue, e = s.Client.Update(ctx, id, in)
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, id); e == nil {
		issue = after.Issue
	}
	result.Issue = issue
	result.Message = fmt.Sprintf("Updated %s", issue.ID)
	return result, nil
}

// Assign records responsibility without claiming. An empty actor clears it.
func (s *Service) Assign(ctx context.Context, id, actor string, opts MutateOptions) (*MutationResult, error) {
	before, err := s.showIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	result := &MutationResult{
		Action:  "assign",
		Before:  &before.Issue,
		Argv:    s.Client.ArgvFor("assign", beads.AssignArgs(id, actor, s.actor(opts))...),
		Message: fmt.Sprintf("Assign %s", id),
	}
	if opts.DryRun {
		result.DryRun = true
		result.Issue = before.Issue
		return result, nil
	}
	var issue domain.Issue
	err = s.withWrite(ctx, func(ctx context.Context) error {
		var e error
		issue, e = s.Client.Assign(ctx, id, actor, s.Scope())
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, id); e == nil {
		issue = after.Issue
	}
	result.Issue = issue
	if actor == "" {
		result.Message = fmt.Sprintf("Cleared assignee on %s", issue.ID)
	} else {
		result.Message = fmt.Sprintf("Assigned %s to %s", issue.ID, actor)
	}
	return result, nil
}

// DepAdd makes blocked wait on blocker. Arguments are never reordered.
func (s *Service) DepAdd(ctx context.Context, blocked, blocker string, rel domain.RelationType, opts MutateOptions) (*MutationResult, error) {
	if blocked == blocker {
		return nil, &UsageError{Message: "an issue cannot depend on itself"}
	}
	if rel == "" {
		rel = domain.RelBlocks
	}
	in := beads.DependencyInput{Scope: s.Scope(), Blocked: blocked, Blocker: blocker, Type: rel}
	result := &MutationResult{
		Action:  "dep add",
		Argv:    s.Client.ArgvFor("dep add", beads.DependencyArgs("add", in, s.actor(opts))...),
		Message: fmt.Sprintf("Make %s blocked by %s", blocked, blocker),
	}
	if rel == domain.RelBlocks {
		if warn, err := s.cycleWarning(ctx, blocked, blocker); err == nil && warn != "" {
			result.Warnings = append(result.Warnings, warn)
		}
	}
	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}
	err := s.withWrite(ctx, func(ctx context.Context) error {
		return s.Client.AddDependency(ctx, in)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	if after, e := s.showIssue(ctx, blocked); e == nil {
		result.Issue = after.Issue
	}
	result.Message = fmt.Sprintf("Added %s: %s waits on %s", rel, blocked, blocker)
	return result, nil
}

// DepRemove deletes one edge. Argument order matches add.
func (s *Service) DepRemove(ctx context.Context, blocked, blocker string, opts MutateOptions) (*MutationResult, error) {
	in := beads.DependencyInput{Scope: s.Scope(), Blocked: blocked, Blocker: blocker}
	result := &MutationResult{
		Action:  "dep remove",
		Argv:    s.Client.ArgvFor("dep remove", beads.DependencyArgs("remove", in, s.actor(opts))...),
		Message: fmt.Sprintf("Remove %s blocked by %s", blocked, blocker),
	}
	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}
	err := s.withWrite(ctx, func(ctx context.Context) error {
		return s.Client.RemoveDependency(ctx, in)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateCache()
	result.Message = fmt.Sprintf("Removed dependency: %s no longer waits on %s", blocked, blocker)
	return result, nil
}

// DepList is a read of one issue's relations.
func (s *Service) DepList(ctx context.Context, id string) ([]domain.Dependency, []domain.Dependency, error) {
	down, err := s.Client.ListDependencies(ctx, id, beads.DirectionDown, s.Scope())
	if err != nil {
		return nil, nil, err
	}
	up, err := s.Client.ListDependencies(ctx, id, beads.DirectionUp, s.Scope())
	if err != nil {
		return down, nil, err
	}
	return down, up, nil
}

// DepReport is the read-only output of `lb dep validate`.
type DepReport struct {
	Cycles     [][]string `json:"cycles"`
	Warnings   []string   `json:"warnings"`
	CycleCount int        `json:"cycle_count"`
}

// DepValidate reports cycles without mutating anything.
func (s *Service) DepValidate(ctx context.Context) (*DepReport, error) {
	cycles, err := s.Client.Cycles(ctx, s.Scope())
	if err != nil {
		return nil, err
	}
	report := &DepReport{Cycles: cycles, CycleCount: len(cycles), Warnings: []string{}}
	if len(cycles) > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d dependency cycle(s) detected", len(cycles)))
	}
	return report, nil
}

func (s *Service) cycleWarning(ctx context.Context, blocked, blocker string) (string, error) {
	// If blocker already waits (transitively) on blocked, adding this edge
	// would close a loop. Beads still decides validity.
	graph, err := s.BuildGraph(ctx, GraphRequest{RootID: blocker, Direction: DirBlockers})
	if err != nil {
		return "", err
	}
	for _, node := range graph.Nodes {
		if node.ID == blocked {
			return fmt.Sprintf("this may create a cycle: %s already waits on %s", blocker, blocked), nil
		}
	}
	return "", nil
}
