// lb-4gm.5
package mirror

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// source stands in for bd: a baseline to list and a journal to follow.
// Reads that reach it are counted, so tests can tell the copy answered.
type source struct {
	beads.Client
	mu        sync.Mutex
	issues    []domain.Issue
	blocked   []string
	journal   []beads.JournalRecord
	wake      chan struct{}
	followErr error // returned once by the next JournalFollow
	disabled  bool
	lists     int
	passReads int
	mutations []string
	// duringList, if set, runs inside each baseline List (with mu held).
	duringList func(s *source)
	claimErr   error
}

func issueJSON(id, status string, priority int, extra string) domain.Issue {
	raw := fmt.Sprintf(`{"id":%q,"title":"t-%s","status":%q,"priority":%d,"created_at":"2026-09-01T00:00:0%dZ"%s}`,
		id, id, status, priority, len(id)%10, extra)
	issues, err := beadsDecode(raw)
	if err != nil {
		panic(err)
	}
	return issues
}

// beadsDecode builds an issue the way the CLI decoder would, keeping Raw.
func beadsDecode(raw string) (domain.Issue, error) {
	rec, err := beads.DecodeJournalRecord([]byte(`{"seq":1,"op":"create","issue_id":"x","issue":` + raw + `}`))
	if err != nil {
		return domain.Issue{}, err
	}
	return *rec.Issue, nil
}

func (s *source) push(op, id string, issue *domain.Issue, dep *beads.JournalDep) {
	s.mu.Lock()
	rec := beads.JournalRecord{Seq: int64(len(s.journal)) + 1, Op: op, IssueID: id, Issue: issue, Dep: dep}
	if issue != nil {
		rec.Actor = "tester"
	}
	s.journal = append(s.journal, rec)
	if s.wake != nil {
		close(s.wake)
		s.wake = nil
	}
	s.mu.Unlock()
}

func (s *source) List(_ context.Context, q beads.ListQuery) ([]domain.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q.All && q.Limit == 0 && q.Status == "" {
		s.lists++
		out := append([]domain.Issue(nil), s.issues...)
		if s.duringList != nil {
			s.duringList(s)
		}
		return out, nil
	}
	s.passReads++
	return []domain.Issue{{ID: "from-bd"}}, nil
}

func (s *source) Blocked(context.Context, beads.Scope) ([]domain.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Issue
	for _, id := range s.blocked {
		out = append(out, domain.Issue{ID: id})
	}
	return out, nil
}

func (s *source) Ready(context.Context, beads.ReadyQuery) ([]domain.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.passReads++
	return []domain.Issue{{ID: "from-bd"}}, nil
}

func (s *source) JournalRead(_ context.Context, since int64, _ int, _ beads.Scope) ([]beads.JournalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return nil, &beads.CommandError{Kind: beads.ErrJournalDisabled}
	}
	var out []beads.JournalRecord
	for _, r := range s.journal {
		if r.Seq > since {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *source) JournalFollow(ctx context.Context, since int64, _ beads.Scope, fn beads.JournalFunc) error {
	for {
		s.mu.Lock()
		if err := s.followErr; err != nil {
			s.followErr = nil
			s.mu.Unlock()
			return err
		}
		var batch []beads.JournalRecord
		for _, r := range s.journal {
			if r.Seq > since {
				batch = append(batch, r)
			}
		}
		if len(batch) == 0 {
			if s.wake == nil {
				s.wake = make(chan struct{})
			}
			wake := s.wake
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil
			case <-wake:
			}
			continue
		}
		s.mu.Unlock()
		for _, r := range batch {
			if err := fn(r); err != nil {
				return err
			}
			since = r.Seq
		}
	}
}

func (s *source) Claim(_ context.Context, id string, _ beads.ClaimInput) (domain.Issue, error) {
	s.mu.Lock()
	s.mutations = append(s.mutations, "claim:"+id)
	err := s.claimErr
	s.mu.Unlock()
	return domain.Issue{ID: id}, err
}

func (s *source) fail(err error) {
	s.mu.Lock()
	s.followErr = err
	if s.wake != nil {
		close(s.wake)
		s.wake = nil
	}
	s.mu.Unlock()
}

func (s *source) count() (lists, pass int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists, s.passReads
}

// start runs a mirror over src and waits until it is live.
func start(t *testing.T, src *source) (*Mirror, func()) {
	t.Helper()
	m := New(src, beads.Scope{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	waitFor(t, "live", func() bool { return m.Status().State == StateLive })
	return m, func() { cancel(); <-done }
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ids(issues []domain.Issue) string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.ID)
	}
	return strings.Join(out, ",")
}

// workspace: epic E with child A (P1, open); B (P0) blocked by A; C closed;
// D (P2) assigned to someone, owned by someone else.
func workspace() *source {
	return &source{
		issues: []domain.Issue{
			issueJSON("E", "open", 2, `,"issue_type":"epic"`),
			issueJSON("A", "open", 1, `,"labels":["ui"],"dependencies":[{"issue_id":"A","depends_on_id":"E","type":"parent-child"}]`),
			issueJSON("B", "open", 0, `,"dependencies":[{"issue_id":"B","depends_on_id":"A","type":"blocks"}]`),
			issueJSON("C", "closed", 1, ``),
			issueJSON("D", "in_progress", 2, `,"assignee":"sam","owner":"pat"`),
		},
		blocked: []string{"B"},
		journal: []beads.JournalRecord{{Seq: 1, Op: "create", IssueID: "old"}, {Seq: 2, Op: "update", IssueID: "old"}},
	}
}

func TestBaselineViews(t *testing.T) {
	src := workspace()
	m, stop := start(t, src)
	defer stop()
	ctx := context.Background()

	if got := m.Status().Checkpoint; got != 2 {
		t.Errorf("checkpoint = %d, want the journal head 2", got)
	}
	list, _ := m.List(ctx, beads.ListQuery{})
	if ids(list) != "B,A,D,E" {
		t.Errorf("list = %s (priority, newest, then id)", ids(list))
	}
	all, _ := m.List(ctx, beads.ListQuery{All: true})
	if ids(all) != "B,A,C,D,E" {
		t.Errorf("list --all = %s", ids(all))
	}
	ready, _ := m.Ready(ctx, beads.ReadyQuery{})
	if ids(ready) != "A,E" {
		t.Errorf("ready = %s: open, unblocked", ids(ready))
	}
	blocked, _ := m.Blocked(ctx, beads.Scope{})
	if ids(blocked) != "B" {
		t.Errorf("blocked = %s", ids(blocked))
	}
	st, _ := m.Stats(ctx, beads.Scope{})
	if st != (beads.Stats{Total: 5, Open: 3, InProgress: 1, Closed: 1, Blocked: 1, Ready: 2, Available: true}) {
		t.Errorf("stats = %+v", st)
	}
	for _, i := range all {
		switch i.ID {
		case "A":
			// Only "blocks" edges count: A's parent link does not.
			if i.DependencyCount != 0 || i.DependentCount != 1 || i.ParentID == nil || *i.ParentID != "E" {
				t.Errorf("A = deps %d dependents %d parent %v", i.DependencyCount, i.DependentCount, i.ParentID)
			}
		case "E":
			if i.DependentCount != 0 {
				t.Errorf("E dependents = %d", i.DependentCount)
			}
		}
	}
	if got, _ := m.List(ctx, beads.ListQuery{Assignee: "sam"}); ids(got) != "D" {
		t.Errorf("assignee sam = %s", ids(got))
	}
	if got, _ := m.List(ctx, beads.ListQuery{Assignee: "pat"}); len(got) != 0 {
		t.Errorf("the owner is not the assignee: %s", ids(got))
	}
	if got, _ := m.Ready(ctx, beads.ReadyQuery{Parent: "E"}); ids(got) != "A" {
		t.Errorf("ready under E = %s", ids(got))
	}
	if got, _ := m.Ready(ctx, beads.ReadyQuery{Labels: []string{"ui"}}); ids(got) != "A" {
		t.Errorf("ready label ui = %s", ids(got))
	}
	up, _ := m.ListDependencies(ctx, "A", beads.DirectionUp, beads.Scope{})
	down, _ := m.ListDependencies(ctx, "A", beads.DirectionDown, beads.Scope{})
	if ids(depIssues(up)) != "B" || ids(depIssues(down)) != "E" || down[0].Type != domain.RelParentChild {
		t.Errorf("A: dependents %v, dependencies %v", depIssues(up), depIssues(down))
	}
	if _, pass := src.count(); pass != 0 {
		t.Errorf("%d reads reached bd while the copy was live", pass)
	}
	if got, _ := m.List(ctx, beads.ListQuery{UpdatedAfter: "2026-01-01"}); ids(got) != "from-bd" {
		t.Error("a filter the copy does not model must go to bd")
	}
}

func TestRecordsKeepTheCopyCurrent(t *testing.T) {
	src := workspace()
	m, stop := start(t, src)
	defer stop()
	ctx := context.Background()
	at := func(seq int64) {
		t.Helper()
		waitFor(t, fmt.Sprintf("seq %d", seq), func() bool { return m.Status().Checkpoint >= seq })
	}

	// Closing A unblocks B: bd sends A's close and a derived update for B.
	closedA := issueJSON("A", "closed", 1, ``)
	unblockedB := issueJSON("B", "open", 0, ``)
	src.push("close", "A", &closedA, nil)
	src.push("update", "B", &unblockedB, nil)
	at(4)
	if got, _ := m.Ready(ctx, beads.ReadyQuery{}); ids(got) != "B,E" {
		t.Errorf("ready after close = %s", ids(got))
	}
	if got, _ := m.Blocked(ctx, beads.Scope{}); len(got) != 0 {
		t.Errorf("blocked after close = %s", ids(got))
	}

	// A new issue, then a dependency making it wait on E.
	newF := issueJSON("F", "open", 3, ``)
	src.push("create", "F", &newF, nil)
	blockedF := issueJSON("F", "open", 3, `,"is_blocked":true`)
	src.push("dep_add", "F", &blockedF, &beads.JournalDep{Kind: "blocks", Target: "E"})
	at(6)
	all, _ := m.List(ctx, beads.ListQuery{All: true})
	for _, i := range all {
		if i.ID == "F" && (!i.IsBlocked || i.DependencyCount != 1) {
			t.Errorf("F = blocked %v deps %d", i.IsBlocked, i.DependencyCount)
		}
		if i.ID == "E" && i.DependentCount != 1 {
			t.Errorf("E dependents = %d, want F (A is a parent-child link)", i.DependentCount)
		}
	}

	// A comment keeps the count bd derives; a removal and a delete drop edges.
	commented := issueJSON("E", "open", 2, `,"issue_type":"epic"`)
	src.push("comment", "E", &commented, nil)
	freeF := issueJSON("F", "open", 3, ``)
	src.push("dep_remove", "F", &freeF, &beads.JournalDep{Kind: "blocks", Target: "E"})
	src.push("delete", "A", nil, nil)
	at(9)
	all, _ = m.List(ctx, beads.ListQuery{All: true})
	if ids(all) != "B,C,D,E,F" {
		t.Errorf("after delete = %s", ids(all))
	}
	for _, i := range all {
		if i.ID == "E" && (i.DependentCount != 0 || i.CommentCount != 1) {
			t.Errorf("E = dependents %d comments %d", i.DependentCount, i.CommentCount)
		}
	}
	if lists, _ := src.count(); lists != 1 {
		t.Errorf("baseline listed %d times; records must not trigger re-reads", lists)
	}
}

func TestMutationsReadThroughUntilTheirRecordLands(t *testing.T) {
	src := workspace()
	m, stop := start(t, src)
	defer stop()
	ctx := context.Background()

	if _, err := m.Claim(ctx, "A", beads.ClaimInput{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Ready(ctx, beads.ReadyQuery{}); ids(got) != "from-bd" {
		t.Errorf("before the claim's record, ready must come from bd: %s", ids(got))
	}
	claimed := issueJSON("A", "in_progress", 1, `,"assignee":"me"`)
	src.push("update", "A", &claimed, nil)
	waitFor(t, "claim applied and settled", func() bool {
		got, _ := m.Ready(ctx, beads.ReadyQuery{})
		return ids(got) == "E"
	})
}

func TestTruncationRebuildsAndDisabledHandsReadsToBD(t *testing.T) {
	src := workspace()
	m, stop := start(t, src)
	defer stop()

	src.fail(&beads.CommandError{Kind: beads.ErrJournalTruncated, Problem: &beads.Problem{Floor: 9, Head: 10}})
	waitFor(t, "rebuild after truncation", func() bool { l, _ := src.count(); return l == 2 })
	waitFor(t, "live again", func() bool { return m.Status().State == StateLive })

	src.mu.Lock()
	src.disabled = true
	src.mu.Unlock()
	src.fail(&beads.CommandError{Kind: beads.ErrJournalDisabled})
	waitFor(t, "off", func() bool { return m.Status().State == StateOff })
	if got, _ := m.Ready(context.Background(), beads.ReadyQuery{}); ids(got) != "from-bd" {
		t.Error("with the journal off, reads go to bd")
	}
	// It stays off until asked, then picks up once the journal is back on.
	time.Sleep(50 * time.Millisecond)
	if m.Status().State != StateOff {
		t.Error("a disabled journal must not be retried on its own")
	}
	src.mu.Lock()
	src.disabled = false
	src.mu.Unlock()
	m.Rebaseline()
	waitFor(t, "live after rebaseline", func() bool { return m.Status().State == StateLive })
}

func TestSafetyRebuild(t *testing.T) {
	src := workspace()
	m := New(src, beads.Scope{})
	m.SafetyInterval = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	waitFor(t, "several rebuilds", func() bool { l, _ := src.count(); return l >= 3 })
}

func TestReadyDefaultLimit(t *testing.T) {
	src := &source{}
	for i := 0; i < 120; i++ {
		src.issues = append(src.issues, issueJSON(fmt.Sprintf("r%03d", i), "open", i%5, ``))
	}
	m, stop := start(t, src)
	defer stop()
	got, _ := m.Ready(context.Background(), beads.ReadyQuery{})
	if len(got) != readyDefaultLimit {
		t.Errorf("ready = %d, bd's default page is %d", len(got), readyDefaultLimit)
	}
	max := 1
	if got, _ := m.Ready(context.Background(), beads.ReadyQuery{PriorityMax: &max}); len(got) != 48 {
		t.Errorf("ready P<=1 = %d, want every match", len(got))
	}
}

func depIssues(deps []domain.Dependency) []domain.Issue {
	out := make([]domain.Issue, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Issue)
	}
	return out
}

// appendLocked adds a record while s.mu is already held.
func (s *source) appendLocked(op, id string, issue *domain.Issue) {
	s.journal = append(s.journal, beads.JournalRecord{Seq: int64(len(s.journal)) + 1, Op: op, IssueID: id, Issue: issue, Actor: "agent"})
}

func TestBaselineRetriesWhenTheWorkspaceChangesMidRead(t *testing.T) {
	src := workspace()
	src.duringList = func(s *source) {
		if s.lists == 1 {
			e := issueJSON("E", "open", 2, `,"issue_type":"epic"`)
			s.appendLocked("update", "E", &e)
		}
	}
	m, stop := start(t, src)
	defer stop()
	if l, _ := src.count(); l != 2 {
		t.Errorf("baseline read %d times; a change mid-read must retry", l)
	}
	if m.Status().Checkpoint != 3 {
		t.Errorf("checkpoint = %d", m.Status().Checkpoint)
	}
}

func TestChurningBaselineReplaysWithoutDoubleCounting(t *testing.T) {
	src := workspace()
	// Every read races a new comment on E, which the listed state already
	// counts: the replayed comment records must not count it again.
	e := issueJSON("E", "open", 2, `,"issue_type":"epic","comment_count":1`)
	src.issues[0] = e
	src.duringList = func(s *source) {
		c := issueJSON("E", "open", 2, `,"issue_type":"epic"`)
		s.appendLocked("comment", "E", &c)
	}
	m, stop := start(t, src)
	defer stop()
	waitFor(t, "replay past the second head", func() bool { return m.Status().Checkpoint >= int64(2+baselineAttempts) })
	all, _ := m.List(context.Background(), beads.ListQuery{All: true})
	for _, i := range all {
		if i.ID == "E" && i.CommentCount != 1 {
			t.Errorf("E comments = %d after replaying the gap", i.CommentCount)
		}
	}
	if l, _ := src.count(); l != baselineAttempts {
		t.Errorf("baseline read %d times, want %d attempts", l, baselineAttempts)
	}
}

func TestFailedMutationDoesNotHoldReads(t *testing.T) {
	src := workspace()
	src.claimErr = &beads.CommandError{Kind: beads.ErrConflict}
	m, stop := start(t, src)
	defer stop()
	if _, err := m.Claim(context.Background(), "A", beads.ClaimInput{}); err == nil {
		t.Fatal("claim should fail")
	}
	if got, _ := m.Ready(context.Background(), beads.ReadyQuery{}); ids(got) == "from-bd" {
		t.Error("a mutation bd rejected changed nothing; the copy should keep answering")
	}
}

func TestStoppedMirrorAnswersNothing(t *testing.T) {
	src := workspace()
	m, stop := start(t, src)
	stop()
	if m.Status().State != StateOff {
		t.Errorf("state after stop = %s", m.Status().State)
	}
	if got, _ := m.Ready(context.Background(), beads.ReadyQuery{}); ids(got) != "from-bd" {
		t.Error("a stopped copy must hand reads to bd")
	}
}
