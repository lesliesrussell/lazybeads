// lb-4gm.5

// Package mirror keeps a local copy of a Beads workspace current by applying
// the events journal, as the Beads maintainers recommend: read the workspace
// once, then apply each committed record — which carries the issue's full
// state after the change — instead of querying bd again.
//
// A Mirror is a beads.Client. It answers the list-shaped reads the views make
// from its copy and passes everything else, including every mutation, to the
// client it wraps. Whenever it cannot answer exactly (not yet synced, the
// journal is off, a filter it does not model, a mutation still settling) the
// read goes to bd, so the copy can make LazyBeads faster but never wrong.
package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// DefaultSafetyInterval is how often the copy is rebuilt from scratch even
// when the journal looks healthy. Syncs (`bd dolt pull`) and `bd sql` are not
// journaled, so this bounds how stale they can leave it.
const DefaultSafetyInterval = 5 * time.Minute

// settle is how long reads keep going to bd after a mutation's own record
// arrives, so the blocked-state records committed with it land too.
const settle = 150 * time.Millisecond

// pendingLimit bounds how long a mutation whose record never shows up keeps
// reads on bd.
const pendingLimit = 5 * time.Second

// readyDefaultLimit is bd ready's default page, which the copy reproduces.
const readyDefaultLimit = 100

// State says whether the copy is answering reads.
type State string

const (
	StateOff     State = "off"
	StateSyncing State = "syncing"
	StateLive    State = "live"
)

// Status describes the copy for the status bar and `lb doctor`.
type Status struct {
	State      State     `json:"state"`
	Reason     string    `json:"reason,omitempty"`
	Checkpoint int64     `json:"checkpoint"`
	Baseline   time.Time `json:"baseline,omitempty"`
}

// Mirror is a beads.Client answering reads from a journal-fed copy.
type Mirror struct {
	beads.Client
	scope beads.Scope

	// SafetyInterval overrides DefaultSafetyInterval; tests shorten it.
	SafetyInterval time.Duration
	// OnChange, if set, is called (from the follower goroutine) after each
	// batch of applied records and after each rebuild.
	OnChange func()

	mu     sync.RWMutex
	issues map[string]*node
	// deps[a][b] is an edge "a depends on b"; rdeps is the same, reversed.
	// They live apart from the issues so an edge to an issue the copy has
	// not seen yet still counts once that issue arrives.
	deps    map[string]map[string]domain.RelationType
	rdeps   map[string]map[string]domain.RelationType
	status  Status
	pending map[string]time.Time
	// liveAt is the head the copy must reach before answering reads, and
	// replaying is true until then.
	liveAt    int64
	replaying bool

	rebuild chan struct{}
}

type node struct {
	issue domain.Issue
	// assignee is the raw assignee; domain.Issue folds the owner in.
	assignee string
}

// New wraps client; call Run to build and follow the copy.
func New(client beads.Client, scope beads.Scope) *Mirror {
	return &Mirror{
		Client:  client,
		scope:   scope,
		issues:  map[string]*node{},
		deps:    map[string]map[string]domain.RelationType{},
		rdeps:   map[string]map[string]domain.RelationType{},
		status:  Status{State: StateSyncing},
		pending: map[string]time.Time{},
		rebuild: make(chan struct{}, 1),
	}
}

// Status reports the copy's current state.
func (m *Mirror) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// Rebaseline asks the follower to rebuild the copy from current state (the
// TUI's refresh key does this).
func (m *Mirror) Rebaseline() {
	select {
	case m.rebuild <- struct{}{}:
	default:
	}
}

// Run builds the copy and keeps it current until ctx ends; a stopped copy
// answers nothing more.
func (m *Mirror) Run(ctx context.Context) {
	defer func() {
		m.mu.Lock()
		m.status.State, m.status.Reason = StateOff, "stopped"
		m.mu.Unlock()
	}()
	backoff := time.Second
	for ctx.Err() == nil {
		if err := m.baseline(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			m.setOff(err)
			if !m.wait(ctx, backoff, isDisabled(err)) {
				return
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		m.notify()

		fctx, cancel := context.WithCancel(ctx)
		errc := make(chan error, 1)
		go func() { errc <- m.Client.JournalFollow(fctx, m.Status().Checkpoint, m.scope, m.apply) }()
		safety := m.SafetyInterval
		if safety <= 0 {
			safety = DefaultSafetyInterval
		}
		timer := time.NewTimer(safety)
		var err error
		stopped := false
		select {
		case err = <-errc:
		case <-m.rebuild:
			stopped = true
		case <-timer.C:
			stopped = true
		case <-ctx.Done():
			stopped = true
		}
		timer.Stop()
		cancel()
		if stopped {
			<-errc // collect the follower before rebuilding
		}
		if ctx.Err() != nil {
			return
		}
		if stopped {
			continue
		}
		if err == nil {
			// The follower ended by itself; rebuild, but not in a hot loop.
			if !m.wait(ctx, backoff, false) {
				return
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		var ce *beads.CommandError
		if errors.As(err, &ce) && ce.Kind == beads.ErrJournalTruncated {
			continue // the records we need are gone: rebuild
		}
		m.setOff(err)
		if !m.wait(ctx, backoff, isDisabled(err)) {
			return
		}
		backoff = min(backoff*2, time.Minute)
	}
}

// wait pauses after a failure. A disabled journal waits for an explicit
// rebuild request (it will not turn itself on); anything else retries.
func (m *Mirror) wait(ctx context.Context, d time.Duration, untilAsked bool) bool {
	var retry <-chan time.Time
	if !untilAsked {
		t := time.NewTimer(d)
		defer t.Stop()
		retry = t.C
	}
	select {
	case <-ctx.Done():
		return false
	case <-m.rebuild:
	case <-retry:
	}
	return true
}

func isDisabled(err error) bool {
	ce, ok := beads.AsCommandError(err)
	return ok && ce.Kind == beads.ErrJournalDisabled
}

func (m *Mirror) setOff(err error) {
	m.mu.Lock()
	m.status = Status{State: StateOff, Reason: err.Error(), Checkpoint: m.status.Checkpoint}
	m.mu.Unlock()
	m.notify()
}

func (m *Mirror) notify() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

// baseline reads the whole workspace. The journal head is taken first, so
// following from it replays anything that changed while the reads ran; every
// record carries full state, so a replay only ever converges.
func (m *Mirror) baseline(ctx context.Context) error {
	m.mu.Lock()
	m.status.State = StateSyncing
	m.mu.Unlock()
	var err error
	for attempt := 0; attempt < baselineAttempts; attempt++ {
		var settled bool
		if settled, err = m.readBaseline(ctx, attempt == baselineAttempts-1); err != nil || settled {
			return err
		}
	}
	return err
}

// baselineAttempts is how often a baseline is retried when the workspace
// changes while it is being read, before accepting a replayed one.
const baselineAttempts = 3

// readBaseline reads the workspace between two journal heads. When nothing
// was committed in between, the copy is exact and goes live at once. When
// something was, it retries (settled=false) — or, on the last attempt,
// accepts the copy and replays the gap, staying in "syncing" (reads go to
// bd) until the replay passes the second head, so no read ever sees the
// copy step backwards.
func (m *Mirror) readBaseline(ctx context.Context, last bool) (settled bool, err error) {
	head, err := beads.JournalHead(ctx, m.Client, m.scope)
	if err != nil {
		return false, err
	}
	issues, err := m.Client.List(ctx, beads.ListQuery{Scope: m.scope, All: true, Limit: 0})
	if err != nil {
		return false, err
	}
	blocked, err := m.Client.Blocked(ctx, m.scope)
	if err != nil {
		return false, err
	}
	after, err := beads.JournalHead(ctx, m.Client, m.scope)
	if err != nil {
		return false, err
	}
	if after != head && !last {
		return false, nil
	}
	isBlocked := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		isBlocked[b.ID] = true
	}

	fresh := make(map[string]*node, len(issues))
	deps := map[string]map[string]domain.RelationType{}
	rdeps := map[string]map[string]domain.RelationType{}
	for _, issue := range issues {
		issue.IsBlocked = isBlocked[issue.ID]
		fresh[issue.ID] = newNode(issue)
		for _, e := range rawEdges(issue.Raw) {
			link(deps, rdeps, issue.ID, e.DependsOnID, domain.NormalizeRelation(e.Type))
		}
	}
	state := StateLive
	if after != head {
		state = StateSyncing
	}
	m.mu.Lock()
	m.issues, m.deps, m.rdeps = fresh, deps, rdeps
	m.status = Status{State: state, Checkpoint: head, Baseline: time.Now()}
	m.liveAt = after
	// A replayed gap may hold comments the baseline already counted.
	m.replaying = after != head
	m.mu.Unlock()
	return true, nil
}

type rawFacts struct {
	Assignee     string    `json:"assignee"`
	Dependencies []rawEdge `json:"dependencies"`
}

type rawEdge struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}

func parseFacts(raw json.RawMessage) rawFacts {
	var f rawFacts
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

// rawEdges reads the dependency edges `bd list --json` and bd serve's
// issue list carry on each issue.
func rawEdges(raw json.RawMessage) []rawEdge {
	var out []rawEdge
	for _, e := range parseFacts(raw).Dependencies {
		if e.DependsOnID != "" {
			out = append(out, e)
		}
	}
	return out
}

func newNode(issue domain.Issue) *node {
	return &node{issue: issue, assignee: parseFacts(issue.Raw).Assignee}
}

func link(deps, rdeps map[string]map[string]domain.RelationType, from, to string, t domain.RelationType) {
	if deps[from] == nil {
		deps[from] = map[string]domain.RelationType{}
	}
	deps[from][to] = t
	if rdeps[to] == nil {
		rdeps[to] = map[string]domain.RelationType{}
	}
	rdeps[to][from] = t
}

func unlink(deps, rdeps map[string]map[string]domain.RelationType, from, to string) {
	delete(deps[from], to)
	delete(rdeps[to], from)
}

// apply folds one journal record into the copy. It is the JournalFunc the
// follower calls, in seq order.
func (m *Mirror) apply(rec beads.JournalRecord) error {
	m.mu.Lock()
	switch rec.Op {
	case beads.JournalOpDelete:
		for to := range m.deps[rec.IssueID] {
			unlink(m.deps, m.rdeps, rec.IssueID, to)
		}
		for from := range m.rdeps[rec.IssueID] {
			unlink(m.deps, m.rdeps, from, rec.IssueID)
		}
		delete(m.issues, rec.IssueID)
	default:
		if rec.Issue != nil {
			// Comment counts are the one field records do not carry whole;
			// during a replayed gap the baseline may already include them.
			m.upsert(*rec.Issue, rec.Op == beads.JournalOpComment && !m.replaying)
		}
		if rec.Dep != nil && rec.Dep.Target != "" {
			switch rec.Op {
			case beads.JournalOpDepAdd:
				link(m.deps, m.rdeps, rec.IssueID, rec.Dep.Target, domain.NormalizeRelation(rec.Dep.Kind))
			case beads.JournalOpDepRemove:
				unlink(m.deps, m.rdeps, rec.IssueID, rec.Dep.Target)
			}
		}
	}
	if rec.Seq > m.status.Checkpoint {
		m.status.Checkpoint = rec.Seq
	}
	if m.replaying && m.status.Checkpoint >= m.liveAt {
		m.replaying = false
		if m.status.State == StateSyncing {
			m.status.State = StateLive
		}
	}
	if until, ok := m.pending[rec.IssueID]; ok {
		if settled := time.Now().Add(settle); settled.Before(until) {
			m.pending[rec.IssueID] = settled
		}
	}
	m.mu.Unlock()
	m.notify()
	return nil
}

// upsert replaces an issue with the state a record carries. Records leave
// out what bd derives (counts, edges), so those are kept from the copy.
func (m *Mirror) upsert(issue domain.Issue, comment bool) {
	n, ok := m.issues[issue.ID]
	if !ok {
		m.issues[issue.ID] = newNode(issue)
		return
	}
	commentCount := n.issue.CommentCount
	n.issue = issue
	n.issue.CommentCount = commentCount
	if comment {
		n.issue.CommentCount++
	}
	n.assignee = parseFacts(issue.Raw).Assignee
}

// live reports whether reads may be answered locally right now.
func (m *Mirror) live() bool {
	if m.status.State != StateLive {
		return false
	}
	now := time.Now()
	for id, until := range m.pending {
		if now.After(until) {
			delete(m.pending, id)
		}
	}
	return len(m.pending) == 0
}

// hold sends reads to bd until the mutation's own record has been applied.
func (m *Mirror) hold(ids ...string) {
	m.mu.Lock()
	until := time.Now().Add(pendingLimit)
	for _, id := range ids {
		if id != "" {
			m.pending[id] = until
		}
	}
	m.mu.Unlock()
}

// view returns a copy of an issue with the fields bd derives filled in.
func (m *Mirror) view(n *node) domain.Issue {
	issue := n.issue
	// bd counts only "blocks" edges: parent-child, related, waits-for and
	// conditional-blocks links are not in dependency_count/dependent_count.
	issue.DependencyCount = countBlocks(m.deps[issue.ID])
	issue.DependentCount = countBlocks(m.rdeps[issue.ID])
	issue.ParentID = nil
	for to, t := range m.deps[issue.ID] {
		if t == domain.RelParentChild {
			parent := to
			issue.ParentID = &parent
		}
	}
	return issue
}

func (m *Mirror) readLocked(fn func()) bool {
	m.mu.Lock()
	ok := m.live()
	m.mu.Unlock()
	if !ok {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	fn()
	return true
}

func countBlocks(edges map[string]domain.RelationType) int {
	n := 0
	for _, t := range edges {
		if t == domain.RelBlocks {
			n++
		}
	}
	return n
}

// --- reads ---------------------------------------------------------------

func (m *Mirror) List(ctx context.Context, q beads.ListQuery) ([]domain.Issue, error) {
	if q.CreatedAfter != "" || q.CreatedBefore != "" || q.UpdatedAfter != "" || q.UpdatedBefore != "" ||
		(q.Sort != "" && q.Sort != "priority" && q.Sort != "created") {
		return m.Client.List(ctx, q)
	}
	var out []domain.Issue
	ok := m.readLocked(func() {
		for _, n := range m.issues {
			issue := m.view(n)
			if !m.listMatch(n, issue, q) {
				continue
			}
			out = append(out, issue)
		}
	})
	if !ok {
		return m.Client.List(ctx, q)
	}
	if q.Sort == "created" {
		sortCreated(out)
	} else {
		sortPriority(out)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (m *Mirror) listMatch(n *node, issue domain.Issue, q beads.ListQuery) bool {
	status := strings.ToLower(strings.TrimSpace(q.Status))
	switch {
	case status != "" && status != "all":
		if !strings.EqualFold(string(issue.Status), status) {
			return false
		}
	case !q.All && status != "all" && issue.IsClosed():
		return false
	}
	if q.Type != "" && !strings.EqualFold(string(issue.Type), q.Type) {
		return false
	}
	if !labelsMatch(issue, q.Labels, q.LabelsAny, q.ExcludeLabels) {
		return false
	}
	if q.Assignee != "" && !strings.EqualFold(n.assignee, q.Assignee) {
		return false
	}
	if q.NoAssignee && n.assignee != "" {
		return false
	}
	if q.Parent != "" && (issue.ParentID == nil || *issue.ParentID != q.Parent) {
		return false
	}
	if q.Priority != nil && int(issue.Priority) != *q.Priority {
		return false
	}
	if q.PriorityMin != nil && int(issue.Priority) < *q.PriorityMin {
		return false
	}
	if q.PriorityMax != nil && int(issue.Priority) > *q.PriorityMax {
		return false
	}
	if len(q.IDs) > 0 && !contains(q.IDs, issue.ID) {
		return false
	}
	return true
}

func (m *Mirror) Ready(ctx context.Context, q beads.ReadyQuery) ([]domain.Issue, error) {
	var out []domain.Issue
	ok := m.readLocked(func() {
		var within map[string]bool
		if q.Parent != "" {
			within = m.descendants(q.Parent)
		}
		for _, n := range m.issues {
			issue := m.view(n)
			if !m.ready(issue) {
				continue
			}
			if within != nil && !within[issue.ID] {
				continue
			}
			if q.Priority != nil && int(issue.Priority) != *q.Priority {
				continue
			}
			if q.PriorityMax != nil && (issue.Priority < 0 || int(issue.Priority) > *q.PriorityMax) {
				continue
			}
			if q.Type != "" && !strings.EqualFold(string(issue.Type), q.Type) {
				continue
			}
			if !labelsMatch(issue, q.Labels, q.LabelsAny, q.ExcludeLabels) {
				continue
			}
			if q.Assignee != "" && !strings.EqualFold(n.assignee, q.Assignee) {
				continue
			}
			if q.Unassigned && n.assignee != "" {
				continue
			}
			out = append(out, issue)
		}
	})
	if !ok {
		return m.Client.Ready(ctx, q)
	}
	sortReadyOrder(out)
	limit := q.Limit
	if limit <= 0 && q.PriorityMax == nil {
		limit = readyDefaultLimit
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ready is bd's ready rule as the copy can see it: open, not blocked by
// Beads' own verdict, and not deferred into the future.
func (m *Mirror) ready(issue domain.Issue) bool {
	return strings.EqualFold(string(issue.Status), string(domain.StatusOpen)) &&
		!issue.IsBlocked && !issue.IsDeferred()
}

// descendants are the issues under id through parent-child edges.
func (m *Mirror) descendants(id string) map[string]bool {
	out := map[string]bool{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for child, t := range m.rdeps[cur] {
			if t == domain.RelParentChild && !out[child] {
				out[child] = true
				queue = append(queue, child)
			}
		}
	}
	return out
}

func (m *Mirror) Blocked(ctx context.Context, scope beads.Scope) ([]domain.Issue, error) {
	var out []domain.Issue
	ok := m.readLocked(func() {
		for _, n := range m.issues {
			issue := m.view(n)
			if issue.IsBlocked && !issue.IsClosed() {
				out = append(out, issue)
			}
		}
	})
	if !ok {
		return m.Client.Blocked(ctx, scope)
	}
	sortPriority(out)
	return out, nil
}

func (m *Mirror) Stats(ctx context.Context, scope beads.Scope) (beads.Stats, error) {
	var st beads.Stats
	ok := m.readLocked(func() {
		for _, n := range m.issues {
			issue := m.view(n)
			st.Total++
			switch {
			case issue.IsClosed():
				st.Closed++
			case issue.IsInProgress():
				st.InProgress++
			case strings.EqualFold(string(issue.Status), string(domain.StatusDeferred)):
				st.Deferred++
			default:
				st.Open++
			}
			if issue.IsBlocked && !issue.IsClosed() {
				st.Blocked++
			}
			if m.ready(issue) {
				st.Ready++
			}
		}
		st.Available = true
	})
	if !ok {
		return m.Client.Stats(ctx, scope)
	}
	return st, nil
}

func (m *Mirror) ListDependencies(ctx context.Context, id string, dir beads.Direction, scope beads.Scope) ([]domain.Dependency, error) {
	var out []domain.Dependency
	found := false
	ok := m.readLocked(func() {
		if _, exists := m.issues[id]; !exists {
			return
		}
		found = true
		edges := m.deps[id]
		if dir == beads.DirectionUp {
			edges = m.rdeps[id]
		}
		for other, t := range edges {
			if on, ok := m.issues[other]; ok {
				out = append(out, domain.Dependency{Issue: m.view(on), Type: t})
			}
		}
	})
	if !ok || !found {
		return m.Client.ListDependencies(ctx, id, dir, scope)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Issue.ID < out[b].Issue.ID })
	return out, nil
}

// --- mutations: straight to bd, holding local reads until they land -------

func (m *Mirror) Create(ctx context.Context, in beads.CreateIssueInput) (domain.Issue, error) {
	m.hold(createPlaceholder)
	issue, err := m.Client.Create(ctx, in)
	m.release(createPlaceholder)
	if err == nil {
		m.holdUnlessApplied(issue.ID)
	}
	return issue, err
}

// createPlaceholder keeps reads on bd while a create is in flight, before
// its id is known.
const createPlaceholder = "\x00create"

func (m *Mirror) release(id string) {
	m.mu.Lock()
	delete(m.pending, id)
	m.mu.Unlock()
}

// holdUnlessApplied holds reads for a newly created issue unless its create
// record already arrived while bd was still answering.
func (m *Mirror) holdUnlessApplied(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, known := m.issues[id]; known {
		m.pending[id] = time.Now().Add(settle)
		return
	}
	m.pending[id] = time.Now().Add(pendingLimit)
}

// done ends a hold early when the mutation failed: bd changed nothing.
func (m *Mirror) done(id string, err error) {
	if err != nil {
		m.release(id)
	}
}

func (m *Mirror) Update(ctx context.Context, id string, in beads.UpdateIssueInput) (domain.Issue, error) {
	m.hold(id)
	issue, err := m.Client.Update(ctx, id, in)
	m.done(id, err)
	return issue, err
}

func (m *Mirror) Claim(ctx context.Context, id string, in beads.ClaimInput) (domain.Issue, error) {
	m.hold(id)
	issue, err := m.Client.Claim(ctx, id, in)
	m.done(id, err)
	return issue, err
}

func (m *Mirror) Close(ctx context.Context, id string, in beads.CloseInput) (domain.Issue, error) {
	m.hold(id)
	issue, err := m.Client.Close(ctx, id, in)
	m.done(id, err)
	return issue, err
}

func (m *Mirror) Reopen(ctx context.Context, id string, in beads.ReopenInput) (domain.Issue, error) {
	m.hold(id)
	issue, err := m.Client.Reopen(ctx, id, in)
	m.done(id, err)
	return issue, err
}

func (m *Mirror) Assign(ctx context.Context, id string, actor string, scope beads.Scope) (domain.Issue, error) {
	m.hold(id)
	issue, err := m.Client.Assign(ctx, id, actor, scope)
	m.done(id, err)
	return issue, err
}

func (m *Mirror) AddDependency(ctx context.Context, in beads.DependencyInput) error {
	m.hold(in.Blocked)
	err := m.Client.AddDependency(ctx, in)
	m.done(in.Blocked, err)
	return err
}

func (m *Mirror) RemoveDependency(ctx context.Context, in beads.DependencyInput) error {
	m.hold(in.Blocked)
	err := m.Client.RemoveDependency(ctx, in)
	m.done(in.Blocked, err)
	return err
}

// --- helpers ---------------------------------------------------------------

func labelsMatch(issue domain.Issue, all, anyOf, exclude []string) bool {
	if len(all) > 0 && !issue.HasAllLabels(all) {
		return false
	}
	if len(anyOf) > 0 && !issue.HasAnyLabel(anyOf) {
		return false
	}
	if len(exclude) > 0 && issue.HasAnyLabel(exclude) {
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func created(i domain.Issue) int64 {
	if i.CreatedAt == nil {
		return math.MinInt64
	}
	return i.CreatedAt.UnixNano()
}

// sortPriority is bd's default order: priority, newest first, then id.
func sortPriority(issues []domain.Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		x, y := issues[a], issues[b]
		if x.Priority != y.Priority {
			return x.Priority < y.Priority
		}
		if created(x) != created(y) {
			return created(x) > created(y)
		}
		return x.ID < y.ID
	})
}

// sortReadyOrder is bd ready's default order: priority, oldest first, id.
func sortReadyOrder(issues []domain.Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		x, y := issues[a], issues[b]
		if x.Priority != y.Priority {
			return x.Priority < y.Priority
		}
		if created(x) != created(y) {
			return created(x) < created(y)
		}
		return x.ID < y.ID
	})
}

func sortCreated(issues []domain.Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		x, y := issues[a], issues[b]
		if created(x) != created(y) {
			return created(x) > created(y)
		}
		return x.ID < y.ID
	})
}

// WorkspaceContext forwards to the wrapped client, so `lb doctor` inside
// the TUI can still report the storage mode.
func (m *Mirror) WorkspaceContext(ctx context.Context, scope beads.Scope) (beads.WorkspaceContext, error) {
	if wc, ok := m.Client.(interface {
		WorkspaceContext(context.Context, beads.Scope) (beads.WorkspaceContext, error)
	}); ok {
		return wc.WorkspaceContext(ctx, scope)
	}
	return beads.WorkspaceContext{}, &beads.CommandError{Kind: beads.ErrUnsupported, Operation: "context"}
}

var _ beads.Client = (*Mirror)(nil)
