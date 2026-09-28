// lb-4gm.8
package beads

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// writeServe answers /v0 writes with an issue body and records each request.
type writeServe struct {
	mu     sync.Mutex
	seen   []writeCall
	status int
	body   string
	delay  time.Duration
}

type writeCall struct {
	Method, Path string
	Body         map[string]any
}

func newWriteServe(t *testing.T, ws *writeServe) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/beads/context" {
			_, _ = io.WriteString(w, `{"api_version":"v0","bd_version":"1.3.0","project_id":"p","capabilities":["issues.create","issues.update","issues.claim","issues.close","issues.reopen","dependencies.add","dependencies.remove"]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		ws.mu.Lock()
		ws.seen = append(ws.seen, writeCall{r.Method, r.URL.Path, body})
		status, reply, delay := ws.status, ws.body, ws.delay
		ws.mu.Unlock()
		time.Sleep(delay)
		if status >= 400 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, reply)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if reply == "" {
			reply = `{"issue":{"id":"sv-1","title":"t","status":"in_progress","priority":1},"already_claimed":false}`
		}
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (ws *writeServe) calls() []writeCall {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return append([]writeCall(nil), ws.seen...)
}

// cliWrites stands in for the bd CLI and records the writes that reach it.
type cliWrites struct {
	fallbackStub
	mu    sync.Mutex
	wrote []string
}

func (c *cliWrites) note(op string) {
	c.mu.Lock()
	c.wrote = append(c.wrote, op)
	c.mu.Unlock()
}

func (c *cliWrites) Create(context.Context, CreateIssueInput) (domain.Issue, error) {
	c.note("create")
	return domain.Issue{ID: "cli"}, nil
}
func (c *cliWrites) Update(context.Context, string, UpdateIssueInput) (domain.Issue, error) {
	c.note("update")
	return domain.Issue{ID: "cli"}, nil
}
func (c *cliWrites) Claim(context.Context, string, ClaimInput) (domain.Issue, error) {
	c.note("claim")
	return domain.Issue{ID: "cli"}, nil
}
func (c *cliWrites) Close(context.Context, string, CloseInput) (domain.Issue, error) {
	c.note("close")
	return domain.Issue{ID: "cli"}, nil
}
func (c *cliWrites) AddDependency(context.Context, DependencyInput) error {
	c.note("dep add")
	return nil
}

func writer(t *testing.T, url, actor string, allow bool) (*HTTP, *cliWrites) {
	t.Helper()
	cli := &cliWrites{}
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: url, Actor: actor, AllowWrites: allow}, cli)
	if err != nil {
		t.Fatal(err)
	}
	return h, cli
}

func TestHTTPWritesMapToTheAPI(t *testing.T) {
	ws := &writeServe{}
	srv := newWriteServe(t, ws)
	h, cli := writer(t, srv.URL, "operator", true)
	ctx := context.Background()
	p := 2
	title, status := "renamed", "blocked"

	claimed, err := h.Claim(ctx, "sv-1", ClaimInput{})
	if err != nil || claimed.ID != "sv-1" || claimed.Status != domain.StatusInProgress {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	if _, err := h.Close(ctx, "sv-1", CloseInput{Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Reopen(ctx, "sv-1", ReopenInput{Reason: "not done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Update(ctx, "sv-1", UpdateIssueInput{Title: &title, Priority: &p, Status: &status, AddLabels: []string{"ui"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Assign(ctx, "sv-1", "sam", Scope{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Create(ctx, CreateIssueInput{Title: "new", Type: "bug", Priority: &p, Labels: []string{"x"}, Parent: "sv-9"}); err != nil {
		t.Fatal(err)
	}
	if err := h.AddDependency(ctx, DependencyInput{Blocked: "sv-2", Blocker: "sv-1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveDependency(ctx, DependencyInput{Blocked: "sv-2", Blocker: "sv-1"}); err != nil {
		t.Fatal(err)
	}

	got := ws.calls()
	want := []struct{ method, path, check string }{
		{"POST", "/v0/beads/issues/sv-1:claim", `"actor":"operator"`},
		{"POST", "/v0/beads/issues/sv-1:close", `"reason":"done"`},
		{"POST", "/v0/beads/issues/sv-1:reopen", `"reason":"not done"`},
		{"PATCH", "/v0/beads/issues/sv-1", `"append_notes":"Reopened: not done"`},
		{"PATCH", "/v0/beads/issues/sv-1", `"add_labels":["ui"],"priority":2,"status":"blocked","title":"renamed"`},
		{"PATCH", "/v0/beads/issues/sv-1", `"force_assignee_transfer":true,"patch":{"assignee":"sam"}`},
		{"POST", "/v0/beads/issues", `"inherit_labels_from_parent":true,"issue_type":"bug","labels":["x"],"parent_id":"sv-9","priority":2,"title":"new"`},
		{"POST", "/v0/beads/dependencies:add", `"edges":[{"depends_on_id":"sv-1","issue_id":"sv-2","type":"blocks"}]`},
		{"POST", "/v0/beads/dependencies:remove", `"depends_on_id":"sv-1","issue_id":"sv-2"`},
	}
	if len(got) != len(want) {
		t.Fatalf("calls = %+v", got)
	}
	for i, w := range want {
		body, _ := json.Marshal(got[i].Body)
		if got[i].Method != w.method || got[i].Path != w.path || !strings.Contains(string(body), w.check) || got[i].Body["actor"] != "operator" {
			t.Errorf("call %d = %s %s %s, want %s %s ~%s", i, got[i].Method, got[i].Path, body, w.method, w.path, w.check)
		}
	}
	if len(cli.wrote) != 0 {
		t.Errorf("writes reached the CLI: %v", cli.wrote)
	}
}

func TestHTTPCreateSendsTheCLIDefaults(t *testing.T) {
	ws := &writeServe{}
	srv := newWriteServe(t, ws)
	h, _ := writer(t, srv.URL, "operator", true)
	if _, err := h.Create(context.Background(), CreateIssueInput{Title: "bare"}); err != nil {
		t.Fatal(err)
	}
	body := ws.calls()[0].Body
	if body["issue_type"] != "task" || body["priority"] != float64(2) {
		t.Errorf("create body = %v; bd create defaults to a P2 task", body)
	}
}

func TestHTTPWritesLeaveBdParsingToTheCLI(t *testing.T) {
	ws := &writeServe{}
	srv := newWriteServe(t, ws)
	h, cli := writer(t, srv.URL, "operator", true)
	ctx := context.Background()
	due := "tomorrow"
	_, _ = h.Create(ctx, CreateIssueInput{Title: "with deps", Deps: []string{"blocks:sv-1"}})
	_, _ = h.Create(ctx, CreateIssueInput{Title: "dated", Due: "next week"})
	_, _ = h.Update(ctx, "sv-1", UpdateIssueInput{Due: &due})
	_, _ = h.Update(ctx, "sv-1", UpdateIssueInput{Metadata: map[string]string{"k": "v"}})
	if len(ws.calls()) != 0 || strings.Join(cli.wrote, ",") != "create,create,update,update" {
		t.Errorf("server saw %d writes; cli %v", len(ws.calls()), cli.wrote)
	}
}

func TestHTTPWritesNeedPermissionAndAnActor(t *testing.T) {
	ws := &writeServe{}
	srv := newWriteServe(t, ws)
	for name, h := range map[string]*HTTP{
		"writes off": func() *HTTP { h, _ := writer(t, srv.URL, "operator", false); return h }(),
		"no actor":   func() *HTTP { h, _ := writer(t, srv.URL, "", true); return h }(),
	} {
		if _, err := h.Claim(context.Background(), "sv-1", ClaimInput{}); err != nil {
			t.Fatal(err)
		}
		if len(ws.calls()) != 0 {
			t.Errorf("%s: a write went over HTTP", name)
		}
	}
}

func TestHTTPWriteRejectionsAreNotRetried(t *testing.T) {
	ws := &writeServe{status: 409, body: `{"status":409,"title":"Conflict","code":"already_claimed","detail":"claimed by sam","request_id":"r"}`}
	srv := newWriteServe(t, ws)
	h, cli := writer(t, srv.URL, "operator", true)
	_, err := h.Claim(context.Background(), "sv-1", ClaimInput{})
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrConflict || ce.Problem == nil || ce.Problem.Detail != "claimed by sam" {
		t.Errorf("err = %v", err)
	}
	ws.mu.Lock()
	ws.status, ws.body = 503, `{"status":503,"title":"Unavailable","code":"busy","request_id":"r"}`
	ws.mu.Unlock()
	if _, err := h.Close(context.Background(), "sv-1", CloseInput{Reason: "x"}); err == nil {
		t.Error("a busy server's answer is an error, not a reason to write again")
	}
	if len(cli.wrote) != 0 {
		t.Errorf("a write that reached the server was repeated through the CLI: %v", cli.wrote)
	}
}

func TestHTTPWriteThatMayHaveLandedIsNotRepeated(t *testing.T) {
	ws := &writeServe{delay: 300 * time.Millisecond}
	srv := newWriteServe(t, ws)
	h, cli := writer(t, srv.URL, "operator", true)
	_, err := h.Claim(context.Background(), "sv-1", ClaimInput{Scope: Scope{Timeout: 50 * time.Millisecond}})
	if err == nil || len(cli.wrote) != 0 {
		t.Errorf("timed-out write: err=%v cli=%v", err, cli.wrote)
	}
}

func TestHTTPWriteFallsBackWhenNothingListens(t *testing.T) {
	ws := &writeServe{}
	srv := newWriteServe(t, ws)
	h, cli := writer(t, srv.URL, "operator", true)
	srv.Close()
	if _, err := h.Claim(context.Background(), "sv-1", ClaimInput{}); err != nil || strings.Join(cli.wrote, ",") != "claim" {
		t.Errorf("refused connection: err=%v cli=%v", err, cli.wrote)
	}
}

func TestEventHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Git hooks bd installs are not event hooks.
	_ = os.WriteFile(filepath.Join(dir, "hooks", "pre-commit"), []byte("#!/bin/sh\n"), 0o755)
	if got := EventHooks(dir); len(got) != 0 {
		t.Errorf("git hooks counted as event hooks: %v", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "hooks", "on_update"), []byte("#!/bin/sh\n"), 0o755)
	if got := EventHooks(dir); strings.Join(got, ",") != "on_update" {
		t.Errorf("hooks = %v", got)
	}
}
