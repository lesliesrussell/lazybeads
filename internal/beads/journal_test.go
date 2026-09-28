//go:build !windows

// lb-4gm.4
package beads

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", name))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) > 0 {
			out = append(out, append([]byte(nil), sc.Bytes()...))
		}
	}
	return out
}

func TestDecodeJournalRecords(t *testing.T) {
	lines := fixtureLines(t, "events.jsonl")
	var recs []JournalRecord
	for _, l := range lines {
		r, err := DecodeJournalRecord(l)
		if err != nil {
			t.Fatalf("decode %s: %v", l, err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 8 {
		t.Fatalf("records = %d", len(recs))
	}
	for i, r := range recs {
		if r.Seq != int64(i+1) || r.Issue == nil || r.Issue.ID != r.IssueID || r.TS.IsZero() {
			t.Errorf("record %d = %+v", i, r)
		}
	}
	dep := recs[3]
	if dep.Op != JournalOpDepAdd || dep.Dep == nil || dep.Dep.Kind != "blocks" || dep.Dep.Target != "fx-2ah" || !dep.Issue.IsBlocked {
		t.Errorf("dep_add = %+v issue=%+v", dep.Dep, dep.Issue)
	}
	if recs[0].Issue.IsBlocked {
		t.Error("a missing is_blocked means false")
	}
	c := recs[6]
	if c.Op != JournalOpComment || c.Comment == nil || c.Comment.Text != "hello" {
		t.Errorf("comment = %+v", c.Comment)
	}
	if ev := c.Event(); !strings.Contains(ev.Summary, "hello") || ev.Actor == nil {
		t.Errorf("comment event = %+v", ev)
	}

	del, err := DecodeJournalRecord([]byte(`{"seq":9,"ts":"2026-09-28T13:40:00Z","op":"delete","issue_id":"fx-1w1","actor":"fixture","issue":null}`))
	if err != nil || del.Issue != nil || del.Event().Summary != "deleted fx-1w1" {
		t.Errorf("delete = %+v, %v", del, err)
	}
	derived, _ := DecodeJournalRecord([]byte(`{"seq":10,"ts":"2026-09-28T13:40:00Z","op":"update","issue_id":"fx-1w1","issue":{"id":"fx-1w1","title":"Beta bug","status":"open"}}`))
	if derived.Actor != "" || !strings.Contains(derived.Event().Summary, "blocked state") {
		t.Errorf("derived update = %+v", derived.Event())
	}
	if _, err := DecodeJournalRecord([]byte(`{"code":"events_journal_truncated"}`)); err == nil {
		t.Error("an error object is not a record")
	}
}

func TestJournalProblemFromCLI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "events-truncated.json"))
	if err != nil {
		t.Fatal(err)
	}
	ce, ok := journalProblem(data)
	if !ok || ce.Kind != ErrJournalTruncated || ce.Problem.Floor != 3 || ce.Problem.Head != 4 {
		t.Fatalf("problem = %+v", ce)
	}
	if _, ok := journalProblem(fixtureLines(t, "events.jsonl")[0]); ok {
		t.Error("a record is not a problem")
	}
}

// journalBD is a fake bd whose `events tail` runs body.
func journalBD(t *testing.T, body string) (*CLI, string) {
	t.Helper()
	bin, dir := fakeBD(t, body)
	return NewCLI(NewRunner(bin), ""), dir
}

func TestCLIJournalRead(t *testing.T) {
	events := filepath.Join("testdata", "bd-1.3.0", "events.jsonl")
	abs, _ := filepath.Abs(events)
	cli, dir := journalBD(t, "cat "+abs)
	recs, err := cli.JournalRead(context.Background(), 5, 2, Scope{Project: dir})
	if err != nil || len(recs) != 8 {
		t.Fatalf("read = %d, %v", len(recs), err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "args.log"))
	if !strings.Contains(string(log), "events tail --since 5 --json --limit 2") {
		t.Errorf("argv = %s", log)
	}
}

func TestCLIJournalFailures(t *testing.T) {
	trunc, _ := filepath.Abs(filepath.Join("testdata", "bd-1.3.0", "events-truncated.json"))
	cli, dir := journalBD(t, "cat "+trunc+"; exit 1")
	_, err := cli.JournalRead(context.Background(), 0, 0, Scope{Project: dir})
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrJournalTruncated || ce.Problem.Floor != 3 {
		t.Errorf("truncated = %v", err)
	}

	note, _ := filepath.Abs(filepath.Join("testdata", "bd-1.3.0", "events-disabled.stderr"))
	cli, dir = journalBD(t, "cat "+note+" >&2; exit 0")
	_, err = cli.JournalRead(context.Background(), 0, 0, Scope{Project: dir})
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrJournalDisabled {
		t.Errorf("disabled read = %v", err)
	}
	// Following a disabled journal would wait forever; it must fail instead.
	cli, dir = journalBD(t, "cat "+note+" >&2; while :; do sleep 0.05; done")
	done := make(chan error, 1)
	go func() {
		done <- cli.JournalFollow(context.Background(), 0, Scope{Project: dir}, func(JournalRecord) error { return nil })
	}()
	select {
	case err := <-done:
		if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrJournalDisabled {
			t.Errorf("disabled follow = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("following a disabled journal hung")
	}
}

func TestCLIJournalFollowStreamsAndStops(t *testing.T) {
	events, _ := filepath.Abs(filepath.Join("testdata", "bd-1.3.0", "events.jsonl"))
	cli, dir := journalBD(t, "head -n 2 "+events+"; sleep 0.3; sed -n 3p "+events+"; while :; do sleep 0.05; done")
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got []int64
	done := make(chan error, 1)
	go func() {
		done <- cli.JournalFollow(ctx, 0, Scope{Project: dir}, func(r JournalRecord) error {
			mu.Lock()
			got = append(got, r.Seq)
			n := len(got)
			mu.Unlock()
			if n == 3 {
				cancel()
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("follow = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not deliver the late record or stop on cancel")
	}
	if fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("seqs = %v", got)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "args.log"))
	if !strings.Contains(string(log), "--follow") {
		t.Errorf("argv = %s", log)
	}
}

func TestStreamHandlesLongLines(t *testing.T) {
	big := strings.Repeat("x", 200<<10)
	bin, dir := fakeBD(t, `printf '%s\n' "`+big+`"; echo short`)
	var lens []int
	err := NewRunner(bin).Stream(context.Background(), "t", Scope{Project: dir}, StreamOptions{
		Stdout: func(line []byte) error { lens = append(lens, len(line)); return nil },
	}, "x")
	if err != nil || len(lens) != 2 || lens[0] != len(big) {
		t.Errorf("lens = %v, %v", lens, err)
	}
}

func TestReadSSE(t *testing.T) {
	stream := "retry: 5000\n\n: heartbeat\n\nid: 7\ndata: {\"a\":1}\n\nevent: truncated\ndata: {\"code\":\"x\"}\n\ndata: line1\ndata: line2\n\n"
	var got []sseEvent
	retry := time.Second
	if err := readSSE(strings.NewReader(stream), func(e sseEvent) error { got = append(got, e); return nil }, &retry); err != nil {
		t.Fatal(err)
	}
	if retry != 5*time.Second || len(got) != 3 || got[0].id != "7" || got[1].event != "truncated" || got[2].data != "line1\nline2" {
		t.Errorf("events = %+v retry=%v", got, retry)
	}
}

// journalServe replays the recorded events:watch stream and answers paged
// reads, counting watch connections and the cursor each one asked for.
type journalServe struct {
	mu          sync.Mutex
	watchCursor []string
	onWatch     func(n int, w http.ResponseWriter)
}

func newJournalServe(t *testing.T, caps string, js *journalServe) *httptest.Server {
	t.Helper()
	page, _ := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", "events-page.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/beads/context":
			fmt.Fprintf(w, `{"api_version":"v0","bd_version":"1.3.0","project_id":"p","capabilities":[%s]}`, caps)
		case "/v0/beads/events":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("since") == "0" {
				_, _ = w.Write(page)
			} else {
				_, _ = w.Write([]byte(`{"records":[],"head":114}`))
			}
		case "/v0/beads/events:watch":
			js.mu.Lock()
			js.watchCursor = append(js.watchCursor, r.Header.Get("Last-Event-ID"))
			n := len(js.watchCursor)
			js.mu.Unlock()
			js.onWatch(n, w)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPJournalReadPages(t *testing.T) {
	srv := newJournalServe(t, `"events.list"`, &journalServe{})
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, &fallbackStub{})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := h.JournalRead(context.Background(), 0, 0, Scope{})
	if err != nil || len(recs) != 3 || recs[2].Seq != 3 {
		t.Fatalf("read = %d, %v", len(recs), err)
	}
}

func TestHTTPJournalFollowReconnectsFromLastSeq(t *testing.T) {
	sse, _ := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", "events-watch.sse"))
	js := &journalServe{onWatch: func(n int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = w.Write(bytes.ReplaceAll(sse, []byte("retry: 3000"), []byte("retry: 10"))) // seqs 1, 2, 115, then the stream drops
			return
		}
		fmt.Fprint(w, "retry: 10\n\nevent: truncated\ndata: {\"status\":410,\"title\":\"Gone\",\"code\":\"events_journal_truncated\",\"since\":115,\"floor\":200,\"head\":300,\"request_id\":\"r\"}\n\n")
	}}
	srv := newJournalServe(t, `"events.list","events.watch"`, js)
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, &fallbackStub{})
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	err = h.JournalFollow(context.Background(), 0, Scope{}, func(r JournalRecord) error {
		got = append(got, r.Seq)
		return nil
	})
	if fmt.Sprint(got) != "[1 2 115]" {
		t.Errorf("seqs = %v", got)
	}
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrJournalTruncated || ce.Problem.Floor != 200 {
		t.Errorf("err = %v", err)
	}
	if fmt.Sprint(js.watchCursor) != "[0 115]" {
		t.Errorf("reconnect cursors = %v", js.watchCursor)
	}
}

func TestHTTPJournalFollowPollsWhenStreamSaturated(t *testing.T) {
	js := &journalServe{onWatch: func(_ int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"status":503,"title":"Unavailable","code":"events_watch_saturated","request_id":"r"}`)
	}}
	srv := newJournalServe(t, `"events.list","events.watch"`, js)
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, &fallbackStub{})
	if err != nil {
		t.Fatal(err)
	}
	h.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	var got []int64
	err = h.JournalFollow(ctx, 0, Scope{}, func(r JournalRecord) error {
		got = append(got, r.Seq)
		if len(got) == 3 {
			cancel()
		}
		return nil
	})
	if err != nil || fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("polled = %v, %v", got, err)
	}
}

type journalFallback struct {
	fallbackStub
	followedFrom int64
}

func (j *journalFallback) JournalFollow(_ context.Context, since int64, _ Scope, _ JournalFunc) error {
	j.followedFrom = since
	return nil
}

func TestHTTPJournalFollowFallsBackToCLIWhenServerGoes(t *testing.T) {
	sse, _ := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", "events-watch.sse"))
	js := &journalServe{onWatch: func(_ int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(bytes.ReplaceAll(sse, []byte("retry: 3000"), []byte("retry: 10")))
	}}
	srv := newJournalServe(t, `"events.list","events.watch"`, js)
	fb := &journalFallback{}
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, fb)
	if err != nil {
		t.Fatal(err)
	}
	err = h.JournalFollow(context.Background(), 0, Scope{}, func(r JournalRecord) error {
		if r.Seq == 115 {
			// The server goes away once the last record has been delivered.
			srv.Close()
		}
		return nil
	})
	if err != nil || fb.followedFrom != 115 {
		t.Errorf("fallback from = %d, %v", fb.followedFrom, err)
	}
}

// lb-4gm.4 review regressions.

type scopedFallback struct {
	fallbackStub
	mu    sync.Mutex
	since int64
	scope Scope
}

func (s *scopedFallback) JournalFollow(_ context.Context, since int64, scope Scope, _ JournalFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.since, s.scope = since, scope
	return nil
}

func TestSaturatedThenGoneFallsBackWithTheWorkspaceScope(t *testing.T) {
	var srv *httptest.Server
	js := &journalServe{onWatch: func(_ int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"status":503,"title":"Unavailable","code":"events_watch_saturated","request_id":"r"}`)
	}}
	srv = newJournalServe(t, `"events.list","events.watch"`, js)
	fb := &scopedFallback{}
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, fb)
	if err != nil {
		t.Fatal(err)
	}
	h.PollInterval = 10 * time.Millisecond
	scope := Scope{Project: "/work/here"}
	err = h.JournalFollow(context.Background(), 0, scope, func(r JournalRecord) error {
		if r.Seq == 3 {
			srv.Close() // gone while polling
		}
		return nil
	})
	if err != nil || fb.since != 3 || fb.scope.Project != "/work/here" {
		t.Errorf("fallback since=%d scope=%+v err=%v", fb.since, fb.scope, err)
	}
}

func TestBusyWatchIsRetriedOnHTTP(t *testing.T) {
	sse, _ := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "http", "events-watch.sse"))
	js := &journalServe{onWatch: func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":503,"title":"Unavailable","code":"busy","request_id":"r"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(sse)
	}}
	srv := newJournalServe(t, `"events.list","events.watch"`, js)
	fb := &scopedFallback{}
	h, err := NewHTTP(context.Background(), HTTPConfig{BaseURL: srv.URL}, fb)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got []int64
	_ = h.JournalFollow(ctx, 0, Scope{}, func(r JournalRecord) error {
		got = append(got, r.Seq)
		if r.Seq == 115 {
			cancel()
		}
		return nil
	})
	if fmt.Sprint(got) != "[1 2 115]" || fb.since != 0 || fb.scope.Project != "" {
		t.Errorf("got %v; CLI fallback since=%d", got, fb.since)
	}
}

func TestReadSSEBoundsRetryAndRejectsOversizedRecords(t *testing.T) {
	retry := time.Second
	_ = readSSE(strings.NewReader("retry: 0\n\n"), func(sseEvent) error { return nil }, &retry)
	if retry != minSSERetry {
		t.Errorf("retry = %v", retry)
	}
	huge := "data: " + strings.Repeat("x", MaxStreamLine+10) + "\n\n"
	if err := readSSE(strings.NewReader(huge), func(sseEvent) error { return nil }, &retry); err == nil {
		t.Error("an unreadable record must fail, not reconnect forever")
	}
}

func TestCLIJournalSkipsStrayStdout(t *testing.T) {
	events, _ := filepath.Abs(filepath.Join("testdata", "bd-1.3.0", "events.jsonl"))
	cli, dir := journalBD(t, "echo 'warning: something bd wanted to say'; cat "+events)
	recs, err := cli.JournalRead(context.Background(), 0, 0, Scope{Project: dir})
	if err != nil || len(recs) != 8 {
		t.Errorf("read = %d, %v", len(recs), err)
	}
}

func TestCLIJournalFollowThatEndsIsAnError(t *testing.T) {
	events, _ := filepath.Abs(filepath.Join("testdata", "bd-1.3.0", "events.jsonl"))
	cli, dir := journalBD(t, "cat "+events)
	err := cli.JournalFollow(context.Background(), 0, Scope{Project: dir}, func(JournalRecord) error { return nil })
	if ce, ok := AsCommandError(err); !ok || ce.Kind != ErrUnavailable {
		t.Errorf("err = %v", err)
	}
}

func TestStreamStopsDespiteAGrandchildHoldingPipes(t *testing.T) {
	bin, dir := fakeBD(t, `sleep 30 & echo '{"seq":1}'; while :; do sleep 0.05; done`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- NewRunner(bin).Stream(ctx, "t", Scope{Project: dir}, StreamOptions{
			Stdout: func([]byte) error { cancel(); return nil },
		}, "x")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stream hung on a grandchild holding its pipes")
	}
}
