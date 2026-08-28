// lb-1td
package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// Client is the only path between LazyBeads and Beads data. Every method maps
// to an explicit argv invocation of bd.
type Client interface {
	Version(ctx context.Context, scope Scope) (VersionInfo, error)
	Info(ctx context.Context, scope Scope) (Info, error)

	Ready(ctx context.Context, q ReadyQuery) ([]domain.Issue, error)
	List(ctx context.Context, q ListQuery) ([]domain.Issue, error)
	Show(ctx context.Context, id string, scope Scope) (domain.IssueDetail, error)
	Blocked(ctx context.Context, scope Scope) ([]domain.Issue, error)
	Search(ctx context.Context, query string, scope Scope) ([]domain.Issue, error)
	Stale(ctx context.Context, days int, scope Scope) ([]domain.Issue, error)
	Stats(ctx context.Context, scope Scope) (Stats, error)

	Create(ctx context.Context, in CreateIssueInput) (domain.Issue, error)
	Update(ctx context.Context, id string, in UpdateIssueInput) (domain.Issue, error)
	Claim(ctx context.Context, id string, in ClaimInput) (domain.Issue, error)
	Close(ctx context.Context, id string, in CloseInput) (domain.Issue, error)
	Reopen(ctx context.Context, id string, in ReopenInput) (domain.Issue, error)
	Assign(ctx context.Context, id string, actor string, scope Scope) (domain.Issue, error)

	AddDependency(ctx context.Context, in DependencyInput) error
	RemoveDependency(ctx context.Context, in DependencyInput) error
	ListDependencies(ctx context.Context, id string, direction Direction, scope Scope) ([]domain.Dependency, error)
	Cycles(ctx context.Context, scope Scope) ([][]string, error)

	Prime(ctx context.Context, scope Scope) (domain.PrimeResult, error)
	Remember(ctx context.Context, in RememberInput) (domain.Memory, error)
	Memories(ctx context.Context, query string, scope Scope) ([]domain.Memory, error)
	Forget(ctx context.Context, id string, scope Scope) error

	History(ctx context.Context, id string, scope Scope) ([]domain.Event, error)
	SyncStatus(ctx context.Context, scope Scope) (domain.SyncStatus, error)

	Capabilities(ctx context.Context, scope Scope) Capabilities
	Runner() *Runner
	// ArgvFor renders the argv a mutation would run, for --dry-run.
	ArgvFor(op string, args ...string) []string
}

// Direction selects which side of the dependency graph to list.
type Direction string

const (
	DirectionDown Direction = "down" // blockers of the issue
	DirectionUp   Direction = "up"   // issues this one blocks
)

// VersionInfo is what bd reports about itself.
type VersionInfo struct {
	Version       string `json:"version"`
	Build         string `json:"build,omitempty"`
	Branch        string `json:"branch,omitempty"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	Raw           string `json:"-"`
}

// Info describes the resolved database.
type Info struct {
	BeadsDir     string `json:"beads_dir,omitempty"`
	DatabasePath string `json:"database_path,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Raw          string `json:"-"`
}

// Stats is the count summary from `bd status`.
type Stats struct {
	Total      int  `json:"total"`
	Open       int  `json:"open"`
	InProgress int  `json:"in_progress"`
	Blocked    int  `json:"blocked"`
	Ready      int  `json:"ready"`
	Closed     int  `json:"closed"`
	Deferred   int  `json:"deferred"`
	Available  bool `json:"available"`
}

// ReadyQuery filters the ready set.
type ReadyQuery struct {
	Scope         Scope
	PriorityMax   *int
	Priority      *int
	Labels        []string
	LabelsAny     []string
	ExcludeLabels []string
	Type          string
	Assignee      string
	Unassigned    bool
	Parent        string
	Limit         int
}

// ListQuery filters the full issue set.
type ListQuery struct {
	Scope         Scope
	Status        string
	Type          string
	Labels        []string
	LabelsAny     []string
	ExcludeLabels []string
	Assignee      string
	NoAssignee    bool
	Parent        string
	Priority      *int
	PriorityMax   *int
	PriorityMin   *int
	CreatedAfter  string
	CreatedBefore string
	UpdatedAfter  string
	UpdatedBefore string
	IDs           []string
	Limit         int
	All           bool
	Sort          string
}

// CreateIssueInput describes a new issue.
type CreateIssueInput struct {
	Scope           Scope
	Title           string
	Description     string
	DescriptionFile string
	Type            string
	Priority        *int
	Labels          []string
	Assignee        string
	Parent          string
	Deps            []string
	Due             string
	Defer           string
	Metadata        map[string]string
	DryRun          bool
}

// UpdateIssueInput describes a partial edit. Nil pointers mean "leave alone".
type UpdateIssueInput struct {
	Scope        Scope
	Title        *string
	Description  *string
	Type         *string
	Priority     *int
	Status       *string
	Assignee     *string
	Parent       *string
	AddLabels    []string
	RemoveLabels []string
	SetLabels    []string
	Metadata     map[string]string
	Due          *string
	Defer        *string
}

// ClaimInput carries the actor for an atomic claim.
type ClaimInput struct {
	Scope Scope
	Actor string
}

// CloseInput carries the mandatory close reason.
type CloseInput struct {
	Scope  Scope
	Reason string
	Actor  string
}

// ReopenInput carries the mandatory reopen reason.
type ReopenInput struct {
	Scope  Scope
	Reason string
	Actor  string
}

// DependencyInput describes one edge. Blocker blocks Blocked.
type DependencyInput struct {
	Scope   Scope
	Blocked string
	Blocker string
	Type    domain.RelationType
}

// RememberInput stores a durable project memory.
type RememberInput struct {
	Scope   Scope
	Content string
	Tags    []string
}

// CLI is the production Client backed by the bd binary.
type CLI struct {
	runner *Runner
	actor  string

	capsOnce  time.Time
	capsCache *Capabilities
	capsTTL   time.Duration
}

// NewCLI builds a client over the given runner.
func NewCLI(runner *Runner, actor string) *CLI {
	return &CLI{runner: runner, actor: actor, capsTTL: 5 * time.Minute}
}

// Runner exposes the underlying process runner for tracing.
func (c *CLI) Runner() *Runner { return c.runner }

// ArgvFor renders the full argv LazyBeads would execute, for --dry-run output.
func (c *CLI) ArgvFor(op string, args ...string) []string {
	return append([]string{c.runner.Binary}, args...)
}

// json runs a bd command with --json and returns raw stdout.
func (c *CLI) jsonCall(ctx context.Context, op string, scope Scope, args ...string) ([]byte, error) {
	res, err := c.runner.Run(ctx, op, scope, append(args, "--json")...)
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}

// actorArgs adds the audit-trail actor when one is configured.
func (c *CLI) actorArgs(actor string) []string {
	if actor == "" {
		actor = c.actor
	}
	if actor == "" {
		return nil
	}
	return []string{"--actor", actor}
}

func (c *CLI) Version(ctx context.Context, scope Scope) (VersionInfo, error) {
	out, err := c.jsonCall(ctx, "version", scope, "version")
	if err != nil {
		return VersionInfo{}, err
	}
	var v VersionInfo
	if data := trimJSON(out); len(data) > 0 {
		if err := json.Unmarshal(data, &v); err != nil {
			return VersionInfo{}, decodeErr(err, data)
		}
	}
	v.Raw = strings.TrimSpace(string(out))
	return v, nil
}

func (c *CLI) Info(ctx context.Context, scope Scope) (Info, error) {
	// `bd where` reports the resolved workspace as JSON; `bd info` is prose.
	out, err := c.jsonCall(ctx, "info", scope, "where")
	if err != nil {
		return Info{}, err
	}
	var payload struct {
		Path         string `json:"path"`
		DatabasePath string `json:"database_path"`
		Prefix       string `json:"prefix"`
		Mode         string `json:"mode"`
	}
	if data := trimJSON(out); len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return Info{}, decodeErr(err, data)
		}
	}
	return Info{
		BeadsDir:     payload.Path,
		DatabasePath: payload.DatabasePath,
		Prefix:       payload.Prefix,
		Mode:         payload.Mode,
		Raw:          strings.TrimSpace(string(out)),
	}, nil
}

func (c *CLI) Ready(ctx context.Context, q ReadyQuery) ([]domain.Issue, error) {
	args := []string{"ready"}
	if q.PriorityMax != nil {
		// bd ready has no --priority-max; the narrower --priority is applied
		// upstream only for an exact match, so the ceiling is enforced locally.
		args = append(args, "--limit", "0")
	}
	if q.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*q.Priority))
	}
	for _, l := range q.Labels {
		args = append(args, "--label", l)
	}
	for _, l := range q.LabelsAny {
		args = append(args, "--label-any", l)
	}
	for _, l := range q.ExcludeLabels {
		args = append(args, "--exclude-label", l)
	}
	if q.Type != "" {
		args = append(args, "--type", q.Type)
	}
	if q.Assignee != "" {
		args = append(args, "--assignee", q.Assignee)
	}
	if q.Unassigned {
		args = append(args, "--unassigned")
	}
	if q.Parent != "" {
		args = append(args, "--parent", q.Parent)
	}
	if q.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(q.Limit))
	}
	out, err := c.jsonCall(ctx, "ready", q.Scope, args...)
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(out)
	if err != nil {
		return nil, err
	}
	if q.PriorityMax != nil {
		issues = filterPriorityMax(issues, *q.PriorityMax)
	}
	return issues, nil
}

func filterPriorityMax(issues []domain.Issue, max int) []domain.Issue {
	out := issues[:0]
	for _, i := range issues {
		if i.Priority >= 0 && int(i.Priority) <= max {
			out = append(out, i)
		}
	}
	return out
}

func (c *CLI) List(ctx context.Context, q ListQuery) ([]domain.Issue, error) {
	args := []string{"list", "--flat"}
	if q.Status != "" {
		args = append(args, "--status", q.Status)
	}
	if q.Type != "" {
		args = append(args, "--type", q.Type)
	}
	for _, l := range q.Labels {
		args = append(args, "--label", l)
	}
	for _, l := range q.LabelsAny {
		args = append(args, "--label-any", l)
	}
	for _, l := range q.ExcludeLabels {
		args = append(args, "--exclude-label", l)
	}
	if q.Assignee != "" {
		args = append(args, "--assignee", q.Assignee)
	}
	if q.NoAssignee {
		args = append(args, "--no-assignee")
	}
	if q.Parent != "" {
		args = append(args, "--parent", q.Parent)
	}
	if q.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*q.Priority))
	}
	if q.PriorityMax != nil {
		args = append(args, "--priority-max", strconv.Itoa(*q.PriorityMax))
	}
	if q.PriorityMin != nil {
		args = append(args, "--priority-min", strconv.Itoa(*q.PriorityMin))
	}
	if q.CreatedAfter != "" {
		args = append(args, "--created-after", q.CreatedAfter)
	}
	if q.CreatedBefore != "" {
		args = append(args, "--created-before", q.CreatedBefore)
	}
	if q.UpdatedAfter != "" {
		args = append(args, "--updated-after", q.UpdatedAfter)
	}
	if q.UpdatedBefore != "" {
		args = append(args, "--updated-before", q.UpdatedBefore)
	}
	if len(q.IDs) > 0 {
		args = append(args, "--id", strings.Join(q.IDs, ","))
	}
	if q.Sort != "" {
		args = append(args, "--sort", q.Sort)
	}
	if q.All {
		args = append(args, "--all")
	}
	args = append(args, "--limit", strconv.Itoa(q.Limit))
	out, err := c.jsonCall(ctx, "list", q.Scope, args...)
	if err != nil {
		return nil, err
	}
	return decodeIssues(out)
}

func (c *CLI) Show(ctx context.Context, id string, scope Scope) (domain.IssueDetail, error) {
	if err := validateID(id); err != nil {
		return domain.IssueDetail{}, err
	}
	out, err := c.jsonCall(ctx, "show", scope, "show", id)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	detail, err := decodeDetail(out)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	// bd reports a dependent count on show but not the dependent list; fetch it
	// separately so callers can name what this issue unblocks.
	if detail.DependentCount > 0 && len(detail.Dependents) == 0 {
		if ups, err := c.ListDependencies(ctx, id, DirectionUp, scope); err == nil {
			detail.Dependents = ups
		}
	}
	return detail, nil
}

func (c *CLI) Blocked(ctx context.Context, scope Scope) ([]domain.Issue, error) {
	out, err := c.jsonCall(ctx, "blocked", scope, "blocked")
	if err != nil {
		return nil, err
	}
	return decodeIssues(out)
}

func (c *CLI) Search(ctx context.Context, query string, scope Scope) ([]domain.Issue, error) {
	if strings.TrimSpace(query) == "" {
		return nil, &CommandError{Kind: ErrValidation, Operation: "search", Cause: fmt.Errorf("empty query")}
	}
	out, err := c.jsonCall(ctx, "search", scope, "search", query)
	if err != nil {
		return nil, err
	}
	return decodeIssues(out)
}

func (c *CLI) Stale(ctx context.Context, days int, scope Scope) ([]domain.Issue, error) {
	if days < 0 {
		days = 0
	}
	out, err := c.jsonCall(ctx, "stale", scope, "stale", "--days", strconv.Itoa(days))
	if err != nil {
		return nil, err
	}
	return decodeIssues(out)
}

func (c *CLI) Stats(ctx context.Context, scope Scope) (Stats, error) {
	out, err := c.jsonCall(ctx, "status", scope, "status")
	if err != nil {
		return Stats{}, err
	}
	var payload struct {
		Summary struct {
			Total      int `json:"total_issues"`
			Open       int `json:"open_issues"`
			InProgress int `json:"in_progress_issues"`
			Blocked    int `json:"blocked_issues"`
			Ready      int `json:"ready_issues"`
			Closed     int `json:"closed_issues"`
			Deferred   int `json:"deferred_issues"`
		} `json:"summary"`
	}
	data := trimJSON(out)
	if len(data) == 0 {
		return Stats{}, nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Stats{}, decodeErr(err, data)
	}
	return Stats{
		Total:      payload.Summary.Total,
		Open:       payload.Summary.Open,
		InProgress: payload.Summary.InProgress,
		Blocked:    payload.Summary.Blocked,
		Ready:      payload.Summary.Ready,
		Closed:     payload.Summary.Closed,
		Deferred:   payload.Summary.Deferred,
		Available:  true,
	}, nil
}

// CreateArgs renders the argv for a create, so --dry-run and the confirmation
// modal can display exactly what will run.
func CreateArgs(in CreateIssueInput, actor string) []string {
	args := []string{"create", in.Title}
	if in.Type != "" {
		args = append(args, "--type", in.Type)
	}
	if in.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*in.Priority))
	}
	if in.Description != "" {
		args = append(args, "--description", in.Description)
	}
	if in.DescriptionFile != "" {
		args = append(args, "--body-file", in.DescriptionFile)
	}
	for _, l := range in.Labels {
		args = append(args, "--labels", l)
	}
	if in.Assignee != "" {
		args = append(args, "--assignee", in.Assignee)
	}
	if in.Parent != "" {
		args = append(args, "--parent", in.Parent)
	}
	for _, d := range in.Deps {
		args = append(args, "--deps", d)
	}
	if in.Due != "" {
		args = append(args, "--due", in.Due)
	}
	if in.Defer != "" {
		args = append(args, "--defer", in.Defer)
	}
	for _, kv := range sortedKV(in.Metadata) {
		args = append(args, "--metadata", kv)
	}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return append(args, "--json")
}

func (c *CLI) Create(ctx context.Context, in CreateIssueInput) (domain.Issue, error) {
	if err := ValidateTitle(in.Title); err != nil {
		return domain.Issue{}, err
	}
	args := CreateArgs(in, c.actor)
	res, err := c.runner.Run(ctx, "create", in.Scope, args...)
	if err != nil {
		return domain.Issue{}, err
	}
	issues, err := decodeIssues(res.Stdout)
	if err != nil || len(issues) == 0 {
		if err == nil {
			err = &CommandError{Kind: ErrDecode, Operation: "create", Stdout: string(res.Stdout),
				Cause: fmt.Errorf("bd reported no created issue")}
		}
		return domain.Issue{}, err
	}
	return issues[0], nil
}

// UpdateArgs renders the argv for an update.
func UpdateArgs(id string, in UpdateIssueInput, actor string) []string {
	args := []string{"update", id}
	if in.Title != nil {
		args = append(args, "--title", *in.Title)
	}
	if in.Description != nil {
		args = append(args, "--description", *in.Description)
	}
	if in.Type != nil {
		args = append(args, "--type", *in.Type)
	}
	if in.Priority != nil {
		args = append(args, "--priority", strconv.Itoa(*in.Priority))
	}
	if in.Status != nil {
		args = append(args, "--status", *in.Status)
	}
	if in.Assignee != nil {
		args = append(args, "--assignee", *in.Assignee)
	}
	if in.Parent != nil {
		args = append(args, "--parent", *in.Parent)
	}
	for _, l := range in.AddLabels {
		args = append(args, "--add-label", l)
	}
	for _, l := range in.RemoveLabels {
		args = append(args, "--remove-label", l)
	}
	for _, l := range in.SetLabels {
		args = append(args, "--set-labels", l)
	}
	for _, kv := range sortedKV(in.Metadata) {
		args = append(args, "--set-metadata", kv)
	}
	if in.Due != nil {
		args = append(args, "--due", *in.Due)
	}
	if in.Defer != nil {
		args = append(args, "--defer", *in.Defer)
	}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return append(args, "--json")
}

func (c *CLI) Update(ctx context.Context, id string, in UpdateIssueInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if in.Title != nil {
		if err := ValidateTitle(*in.Title); err != nil {
			return domain.Issue{}, err
		}
	}
	if _, err := c.runner.Run(ctx, "update", in.Scope, UpdateArgs(id, in, c.actor)...); err != nil {
		return domain.Issue{}, err
	}
	detail, err := c.Show(ctx, id, in.Scope)
	return detail.Issue, err
}

// ClaimArgs renders the argv for an atomic claim.
func ClaimArgs(id string, actor string) []string {
	args := []string{"update", id, "--claim"}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return append(args, "--json")
}

func (c *CLI) Claim(ctx context.Context, id string, in ClaimInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	actor := in.Actor
	if actor == "" {
		actor = c.actor
	}
	if _, err := c.runner.Run(ctx, "claim", in.Scope, ClaimArgs(id, actor)...); err != nil {
		return domain.Issue{}, err
	}
	detail, err := c.Show(ctx, id, in.Scope)
	return detail.Issue, err
}

// CloseArgs renders the argv for a close.
func CloseArgs(id, reason, actor string) []string {
	args := []string{"close", id}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return append(args, "--json")
}

func (c *CLI) Close(ctx context.Context, id string, in CloseInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.Issue{}, &CommandError{Kind: ErrValidation, Operation: "close",
			Cause: fmt.Errorf("a close reason is required")}
	}
	actor := in.Actor
	if actor == "" {
		actor = c.actor
	}
	if _, err := c.runner.Run(ctx, "close", in.Scope, CloseArgs(id, in.Reason, actor)...); err != nil {
		return domain.Issue{}, err
	}
	detail, err := c.Show(ctx, id, in.Scope)
	return detail.Issue, err
}

// ReopenArgs renders the argv for a reopen. bd records the reason as a note,
// because `bd reopen` itself takes no reason flag.
func ReopenArgs(id, actor string) []string {
	args := []string{"reopen", id}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return append(args, "--json")
}

func (c *CLI) Reopen(ctx context.Context, id string, in ReopenInput) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return domain.Issue{}, &CommandError{Kind: ErrValidation, Operation: "reopen",
			Cause: fmt.Errorf("a reopen reason is required")}
	}
	actor := in.Actor
	if actor == "" {
		actor = c.actor
	}
	if _, err := c.runner.Run(ctx, "reopen", in.Scope, ReopenArgs(id, actor)...); err != nil {
		return domain.Issue{}, err
	}
	// Preserve the operator's stated reason as a durable note on the issue.
	noteArgs := []string{"note", id, "Reopened: " + in.Reason}
	if actor != "" {
		noteArgs = append(noteArgs, "--actor", actor)
	}
	_, _ = c.runner.Run(ctx, "reopen note", in.Scope, noteArgs...)

	detail, err := c.Show(ctx, id, in.Scope)
	return detail.Issue, err
}

// AssignArgs renders the argv for an assignment.
func AssignArgs(id, actorTarget, auditActor string) []string {
	args := []string{"update", id, "--assignee", actorTarget}
	if auditActor != "" {
		args = append(args, "--actor", auditActor)
	}
	return append(args, "--json")
}

func (c *CLI) Assign(ctx context.Context, id string, actor string, scope Scope) (domain.Issue, error) {
	if err := validateID(id); err != nil {
		return domain.Issue{}, err
	}
	if _, err := c.runner.Run(ctx, "assign", scope, AssignArgs(id, actor, c.actor)...); err != nil {
		return domain.Issue{}, err
	}
	detail, err := c.Show(ctx, id, scope)
	return detail.Issue, err
}

// DependencyArgs renders the argv for a dependency edit. The order is fixed:
// bd dep add <blocked> <blocker>. It is never inferred or reordered.
func DependencyArgs(action string, in DependencyInput, actor string) []string {
	args := []string{"dep", action, in.Blocked, in.Blocker}
	if action == "add" && in.Type != "" {
		args = append(args, "--type", string(in.Type))
	}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	return args
}

func (c *CLI) AddDependency(ctx context.Context, in DependencyInput) error {
	if err := validateID(in.Blocked); err != nil {
		return err
	}
	if err := validateID(in.Blocker); err != nil {
		return err
	}
	if in.Blocked == in.Blocker {
		return &CommandError{Kind: ErrValidation, Operation: "dep add",
			Cause: fmt.Errorf("an issue cannot depend on itself")}
	}
	_, err := c.runner.Run(ctx, "dep add", in.Scope, DependencyArgs("add", in, c.actor)...)
	return err
}

func (c *CLI) RemoveDependency(ctx context.Context, in DependencyInput) error {
	if err := validateID(in.Blocked); err != nil {
		return err
	}
	if err := validateID(in.Blocker); err != nil {
		return err
	}
	_, err := c.runner.Run(ctx, "dep remove", in.Scope, DependencyArgs("remove", in, c.actor)...)
	return err
}

func (c *CLI) ListDependencies(ctx context.Context, id string, direction Direction, scope Scope) ([]domain.Dependency, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	if direction == "" {
		direction = DirectionDown
	}
	out, err := c.jsonCall(ctx, "dep list", scope, "dep", "list", id, "--direction", string(direction))
	if err != nil {
		return nil, err
	}
	return decodeDependencies(out)
}

func (c *CLI) Cycles(ctx context.Context, scope Scope) ([][]string, error) {
	out, err := c.jsonCall(ctx, "dep cycles", scope, "dep", "cycles")
	if err != nil {
		return nil, err
	}
	data := trimJSON(out)
	if len(data) == 0 {
		return nil, nil
	}
	// bd emits either a list of ID lists or a list of objects carrying a path.
	var direct [][]string
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}
	var wrapped []struct {
		Cycle []string `json:"cycle"`
		Path  []string `json:"path"`
		IDs   []string `json:"ids"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, decodeErr(err, data)
	}
	out2 := make([][]string, 0, len(wrapped))
	for _, w := range wrapped {
		switch {
		case len(w.Cycle) > 0:
			out2 = append(out2, w.Cycle)
		case len(w.Path) > 0:
			out2 = append(out2, w.Path)
		case len(w.IDs) > 0:
			out2 = append(out2, w.IDs)
		}
	}
	return out2, nil
}

func (c *CLI) Prime(ctx context.Context, scope Scope) (domain.PrimeResult, error) {
	res, err := c.runner.Run(ctx, "prime", scope, "prime")
	if err != nil {
		return domain.PrimeResult{}, err
	}
	return domain.PrimeResult{Text: strings.TrimSpace(string(res.Stdout))}, nil
}

func (c *CLI) Remember(ctx context.Context, in RememberInput) (domain.Memory, error) {
	if strings.TrimSpace(in.Content) == "" {
		return domain.Memory{}, &CommandError{Kind: ErrValidation, Operation: "remember",
			Cause: fmt.Errorf("memory content is required")}
	}
	args := []string{"remember", in.Content}
	if _, err := c.runner.Run(ctx, "remember", in.Scope, args...); err != nil {
		return domain.Memory{}, err
	}
	return domain.Memory{Content: in.Content, Tags: in.Tags}, nil
}

func (c *CLI) Memories(ctx context.Context, query string, scope Scope) ([]domain.Memory, error) {
	args := []string{"memories"}
	if strings.TrimSpace(query) != "" {
		args = append(args, query)
	}
	out, err := c.jsonCall(ctx, "memories", scope, args...)
	if err != nil {
		return nil, err
	}
	data := trimJSON(out)
	if len(data) == 0 {
		return nil, nil
	}
	var direct []struct {
		ID        string   `json:"id"`
		Content   string   `json:"content"`
		Text      string   `json:"text"`
		Memory    string   `json:"memory"`
		Tags      []string `json:"tags"`
		CreatedAt string   `json:"created_at"`
	}
	if err := json.Unmarshal(data, &direct); err != nil {
		// An envelope with no memories is a legitimate empty result.
		return nil, nil
	}
	out2 := make([]domain.Memory, 0, len(direct))
	for _, m := range direct {
		out2 = append(out2, domain.Memory{
			ID:        m.ID,
			Content:   firstNonEmpty(m.Content, m.Text, m.Memory),
			Tags:      m.Tags,
			CreatedAt: parseTime(m.CreatedAt),
		})
	}
	return out2, nil
}

func (c *CLI) Forget(ctx context.Context, id string, scope Scope) error {
	if strings.TrimSpace(id) == "" {
		return &CommandError{Kind: ErrValidation, Operation: "forget", Cause: fmt.Errorf("memory id is required")}
	}
	_, err := c.runner.Run(ctx, "forget", scope, "forget", id)
	return err
}

func (c *CLI) History(ctx context.Context, id string, scope Scope) ([]domain.Event, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	out, err := c.jsonCall(ctx, "history", scope, "history", id)
	if err != nil {
		return nil, err
	}
	return decodeHistory(out, id)
}

func (c *CLI) SyncStatus(ctx context.Context, scope Scope) (domain.SyncStatus, error) {
	res, err := c.runner.Run(ctx, "sync status", scope, "dolt", "status")
	if err != nil {
		// Sync visibility is optional. Report unknown rather than inventing a
		// clean state, which the specification forbids.
		return domain.SyncStatus{Available: false, Detail: "LazyBeads could not determine remote status"}, nil
	}
	text := strings.TrimSpace(string(res.Stdout))
	if text == "" {
		return domain.SyncStatus{Available: false, Detail: "bd reported no sync information"}, nil
	}
	return domain.SyncStatus{Available: true, Detail: text}, nil
}

// sortedKV renders a metadata map as deterministic key=value arguments.
func sortedKV(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
