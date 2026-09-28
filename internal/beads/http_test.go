// lb-4gm.2
package beads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// fakeServe answers /v0 requests from the fixtures recorded against a live
// `bd serve` 1.3.0, and remembers every request it saw.
type fakeServe struct {
	t        *testing.T
	mu       sync.Mutex
	requests []*http.Request
	// routes maps "METHOD /path" to a fixture file under testdata/bd-1.3.0/http,
	// or to a literal problem body when the value starts with "{".
	routes map[string]string
	status map[string]int
}

func newFakeServe(t *testing.T) (*fakeServe, *httptest.Server) {
	f := &fakeServe{
		t: t,
		routes: map[string]string{
			"GET /v0/beads/context":             "context.json",
			"GET /v0/beads/ready":               "ready.json",
			"GET /v0/beads/issues":              "issues.json",
			"GET /v0/beads/issues/sv-379":       "issue-claimed.json",
			"GET /v0/beads/issues/sv-q59":       "issue-blocked.json",
			"GET /v0/beads/issues/sv-nope":      "problem-not-found.json",
			"GET /v0/beads/stats":               "stats.json",
			"GET /v0/beads/dependencies/cycles": "cycles-empty.json",
			"GET /v0/beads/dependencies":        "dependencies.json",
		},
		status: map[string]int{"GET /v0/beads/issues/sv-nope": http.StatusNotFound},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeServe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	src, ok := f.routes[key]
	if !ok {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"title":"Not Found","code":"not_found","request_id":"t"}`))
		return
	}
	body := []byte(src)
	if !strings.HasPrefix(src, "{") {
		var err error
		body, err = os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", src))
		if err != nil {
			f.t.Fatal(err)
		}
	}
	status := http.StatusOK
	if s, ok := f.status[key]; ok {
		status = s
	}
	if status >= 400 {
		w.Header().Set("Content-Type", "application/problem+json")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (f *fakeServe) last(path string) *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].URL.Path == path {
			return f.requests[i]
		}
	}
	return nil
}

// fallbackStub stands in for the CLI and records which reads reached it.
type fallbackStub struct {
	Client
	mu    sync.Mutex
	calls []string
}

func (s *fallbackStub) record(op string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, op)
}

func (s *fallbackStub) called(op string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c == op {
			return true
		}
	}
	return false
}

func (s *fallbackStub) Ready(context.Context, ReadyQuery) ([]domain.Issue, error) {
	s.record("ready")
	return []domain.Issue{{ID: "cli-1"}}, nil
}

func (s *fallbackStub) List(context.Context, ListQuery) ([]domain.Issue, error) {
	s.record("list")
	return []domain.Issue{{ID: "cli-1"}}, nil
}

func (s *fallbackStub) Show(_ context.Context, id string, _ Scope) (domain.IssueDetail, error) {
	s.record("show:" + id)
	if id == "379" {
		return domain.IssueDetail{Issue: domain.Issue{ID: "sv-379"}}, nil
	}
	return domain.IssueDetail{}, &CommandError{Kind: ErrNotFound, Operation: "show"}
}

func (s *fallbackStub) Stats(context.Context, Scope) (Stats, error) {
	s.record("stats")
	return Stats{Available: true}, nil
}

func newTestHTTP(t *testing.T, url string) (*HTTP, *fallbackStub) {
	t.Helper()
	fb := &fallbackStub{}
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: url}, fb)
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	return h, fb
}

func TestHTTPReadsContext(t *testing.T) {
	_, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	for _, op := range []string{"ready.list", "issues.get", "events.watch"} {
		if !h.Supports(op) {
			t.Errorf("server advertises %s", op)
		}
	}
	if h.Supports("teleport.now") {
		t.Error("an unadvertised operation must not be supported")
	}
	if got := h.ServerInfo().BDVersion; got != "1.3.0" {
		t.Errorf("bd_version = %q", got)
	}
}

func TestHTTPReady(t *testing.T) {
	f, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)
	issues, err := h.Ready(context.Background(), ReadyQuery{Labels: []string{"ui"}, Type: "task", Limit: 5})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 || issues[0].ID != "sv-kk9" || len(issues[0].Labels) != 1 {
		t.Fatalf("ready = %+v", issues)
	}
	q := f.last("/v0/beads/ready").URL.Query()
	if q.Get("label") != "ui" || q.Get("type") != "task" || q.Get("limit") != "5" {
		t.Errorf("query = %v", q)
	}
	if fb.called("ready") {
		t.Error("a supported read must not reach the CLI")
	}
}

func TestHTTPReadyPriorityCeilingIsLocal(t *testing.T) {
	f, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	max := 1
	issues, err := h.Ready(context.Background(), ReadyQuery{PriorityMax: &max})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("P2 issue survived a P1 ceiling: %+v", issues)
	}
	if got := f.last("/v0/beads/ready").URL.Query().Get("limit"); got != "0" {
		t.Errorf("a local ceiling needs the whole set; limit = %q", got)
	}
}

func TestHTTPListFallsBackForFiltersTheServerLacks(t *testing.T) {
	f, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)
	issues, err := h.List(context.Background(), ListQuery{Status: "open", All: true})
	if err != nil || len(issues) != 3 {
		t.Fatalf("List = %d issues, %v", len(issues), err)
	}
	if q := f.last("/v0/beads/issues").URL.Query(); q.Get("status") != "open" || q.Get("all") != "true" {
		t.Errorf("query = %v", q)
	}
	p := 1
	for name, q := range map[string]ListQuery{
		"priority":      {Priority: &p},
		"updated_after": {UpdatedAfter: "2026-01-01"},
		"ids":           {IDs: []string{"a"}},
		"no_assignee":   {NoAssignee: true},
	} {
		fb.calls = nil
		issues, err := h.List(context.Background(), q)
		if err != nil || len(issues) != 1 || issues[0].ID != "cli-1" || !fb.called("list") {
			t.Errorf("%s: want CLI fallback, got %+v %v", name, issues, err)
		}
	}
}

func TestHTTPShow(t *testing.T) {
	_, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	claimed, err := h.Show(context.Background(), "sv-379", Scope{})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if claimed.Status != domain.StatusInProgress || len(claimed.Dependents) != 1 || claimed.Dependents[0].Issue.ID != "sv-q59" {
		t.Errorf("claimed = %+v", claimed)
	}
	blocked, err := h.Show(context.Background(), "sv-q59", Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Dependencies) != 1 || blocked.Dependencies[0].Issue.ID != "sv-379" || blocked.Dependencies[0].Type != domain.RelBlocks {
		t.Errorf("blocked deps = %+v", blocked.Dependencies)
	}
	if _, err := h.Show(context.Background(), "../etc", Scope{}); err == nil {
		t.Error("ids are validated before they reach a URL")
	}
}

func TestHTTPStatsAndCycles(t *testing.T) {
	_, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	st, err := h.Stats(context.Background(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if st != (Stats{Total: 4, Open: 2, InProgress: 1, Blocked: 1, Ready: 1, Closed: 1, Available: true}) {
		t.Errorf("stats = %+v", st)
	}
	cycles, err := h.Cycles(context.Background(), Scope{})
	if err != nil || len(cycles) != 0 {
		t.Errorf("cycles = %v, %v", cycles, err)
	}
}

func TestHTTPProblemMapping(t *testing.T) {
	_, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	_ = h
	body, err := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", "problem-not-found.json"))
	if err != nil {
		t.Fatal(err)
	}
	ce, ok := AsCommandError(problemError("show", 404, "application/problem+json; charset=utf-8", body))
	if !ok || ce.Kind != ErrNotFound {
		t.Fatalf("err = %v", ce)
	}
	if ce.Problem == nil || ce.Problem.Code != "not_found" || ce.Problem.Status != 404 {
		t.Errorf("problem = %+v", ce.Problem)
	}

	cases := []struct {
		status int
		body   string
		want   ErrorKind
	}{
		{400, `{"status":400,"title":"Bad Request","code":"invalid_argument","detail":"limit must be >= 0","request_id":"r"}`, ErrValidation},
		{409, `{"status":409,"title":"Conflict","code":"already_claimed","request_id":"r"}`, ErrConflict},
		{409, `{"status":409,"title":"Conflict","code":"events_journal_disabled","request_id":"r"}`, ErrJournalDisabled},
		{410, `{"status":410,"title":"Gone","code":"events_journal_truncated","since":3,"floor":10,"head":42,"request_id":"r"}`, ErrJournalTruncated},
		{503, `{"status":503,"title":"Unavailable","code":"busy","request_id":"r"}`, ErrUnavailable},
		{418, `{"status":418,"title":"Teapot","code":"brand_new_code","request_id":"r"}`, ErrBDExecution},
		{503, `{"status":503,"title":"Unavailable","code":"brand_new_code","request_id":"r"}`, ErrUnavailable},
		{500, `not json at all`, ErrBDExecution},
	}
	for _, c := range cases {
		err := problemError("op", c.status, "application/problem+json", []byte(c.body))
		ce, ok := AsCommandError(err)
		if !ok || ce.Kind != c.want {
			t.Errorf("%d %s: kind = %v, want %s", c.status, c.body, err, c.want)
		}
	}
	trunc, _ := AsCommandError(problemError("op", 410, "application/problem+json", []byte(cases[3].body)))
	if trunc.Problem.Floor != 10 || trunc.Problem.Head != 42 || trunc.Problem.Since != 3 {
		t.Errorf("truncation fields = %+v", trunc.Problem)
	}
	if !strings.Contains(problemError("op", 400, "application/problem+json", []byte(cases[0].body)).(*CommandError).Message(), "limit must be >= 0") {
		t.Error("a validation problem surfaces its detail")
	}
}

func TestHTTPFallsBackWhenUnsupportedOrUnreachable(t *testing.T) {
	f, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)

	delete(h.caps, "stats.get")
	if _, err := h.Stats(context.Background(), Scope{}); err != nil || !fb.called("stats") {
		t.Errorf("unadvertised stats should use the CLI: %v %v", err, fb.calls)
	}
	if f.last("/v0/beads/stats") != nil {
		t.Error("an unadvertised operation must not be requested")
	}

	srv.Close()
	fb.calls = nil
	issues, err := h.Ready(context.Background(), ReadyQuery{})
	if err != nil || len(issues) != 1 || issues[0].ID != "cli-1" || !fb.called("ready") {
		t.Errorf("an unreachable server should fall back to the CLI for reads: %+v %v", issues, err)
	}
}

func TestHTTPSendsTokenWithoutLeakingIt(t *testing.T) {
	f, srv := newFakeServe(t)
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL, Token: "s3cret"}, &fallbackStub{})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.last("/v0/beads/context").Header.Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization = %q", got)
	}
	_, err = h.Stats(context.Background(), Scope{Timeout: 1})
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error = %v", err)
	}
}

func TestNewHTTPRejectsUnreachableServer(t *testing.T) {
	_, srv := newFakeServe(t)
	url := srv.URL
	srv.Close()
	_, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: url}, &fallbackStub{})
	ce, ok := AsCommandError(err)
	if !ok || ce.Kind != ErrUnavailable {
		t.Errorf("err = %v", err)
	}
}

func TestHTTPListOrdersLikeBDList(t *testing.T) {
	f, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)
	if _, err := h.List(context.Background(), ListQuery{}); err != nil {
		t.Fatal(err)
	}
	if got := f.last("/v0/beads/issues").URL.Query().Get("sort"); got != "priority" {
		t.Errorf("sort = %q; bd list orders by priority when no sort is given", got)
	}
	if got := f.last("/v0/beads/issues").URL.Query().Get("limit"); got != "0" {
		t.Errorf("limit = %q; bd list --json without --limit returns every match", got)
	}
	if _, err := h.List(context.Background(), ListQuery{Sort: "created"}); err != nil {
		t.Fatal(err)
	}
	if got := f.last("/v0/beads/issues").URL.Query().Get("sort"); got != "created" {
		t.Errorf("sort = %q", got)
	}
	if issues, _ := h.List(context.Background(), ListQuery{Sort: "title"}); len(issues) != 1 || !fb.called("list") {
		t.Error("an order the server does not serve goes to the CLI")
	}
}

func TestHTTPRefusedArgumentFallsBack(t *testing.T) {
	f, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)
	f.routes["GET /v0/beads/ready"] = `{"status":400,"title":"Bad Request","code":"invalid_argument","param":"limit","reason":"invalid_value","detail":"unlimited reads are loopback-only; pass an explicit limit","request_id":"r"}`
	f.status["GET /v0/beads/ready"] = 400
	max := 2
	issues, err := h.Ready(context.Background(), ReadyQuery{PriorityMax: &max})
	if err != nil || len(issues) != 1 || !fb.called("ready") {
		t.Errorf("a refused argument should fall back to the CLI: %+v %v", issues, err)
	}
}

func TestHTTPShowResolvesShortIDsThroughCLI(t *testing.T) {
	_, srv := newFakeServe(t)
	h, fb := newTestHTTP(t, srv.URL)
	got, err := h.Show(context.Background(), "379", Scope{})
	if err != nil || got.ID != "sv-379" || !fb.called("show:379") {
		t.Errorf("short id = %+v, %v", got, err)
	}
	if _, err := h.Show(context.Background(), "sv-nope", Scope{}); err == nil {
		t.Error("an id neither side knows is still not found")
	}
}

func TestHTTPReadyCeilingKeepsLimit(t *testing.T) {
	f, srv := newFakeServe(t)
	h, _ := newTestHTTP(t, srv.URL)
	f.routes["GET /v0/beads/ready"] = "issues.json"
	max := 4
	issues, err := h.Ready(context.Background(), ReadyQuery{PriorityMax: &max, Limit: 2})
	if err != nil || len(issues) != 2 {
		t.Errorf("ceiling plus limit = %d issues, %v", len(issues), err)
	}
}

func TestNewHTTPRequiresFallback(t *testing.T) {
	_, srv := newFakeServe(t)
	if _, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, nil); err == nil {
		t.Error("a nil CLI fallback must be rejected")
	}
}
