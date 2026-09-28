// lb-4gm.4
package integration

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
)

// TestLiveJournalCLI follows a real embedded workspace's events journal
// through `bd events tail --follow` and sees a new mutation arrive.
func TestLiveJournalCLI(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	dir := t.TempDir()
	run(t, dir, "bd", "init", "--prefix", "j", "--quiet")
	scope := beads.Scope{Project: dir}
	cli := beads.NewCLI(beads.NewRunner(""), "")
	if !cli.Capabilities(context.Background(), scope).EventsJournal {
		t.Skip("this bd has no events journal")
	}

	// Off by default: reads and follows say so rather than waiting forever.
	_, err := cli.JournalRead(context.Background(), 0, 0, scope)
	if ce, ok := beads.AsCommandError(err); !ok || ce.Kind != beads.ErrJournalDisabled {
		t.Fatalf("disabled journal read = %v", err)
	}

	run(t, dir, "bd", "config", "set", "events-journal", "true")
	run(t, dir, "bd", "create", "first", "--quiet")
	recs, err := cli.JournalRead(context.Background(), 0, 0, scope)
	if err != nil || len(recs) != 1 || recs[0].Op != beads.JournalOpCreate || recs[0].Issue == nil || recs[0].Issue.Title != "first" {
		t.Fatalf("read = %+v, %v", recs, err)
	}

	got := make(chan beads.JournalRecord, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cli.JournalFollow(ctx, recs[0].Seq, scope, func(r beads.JournalRecord) error {
			got <- r
			return nil
		})
	}()
	time.Sleep(500 * time.Millisecond)
	run(t, dir, "bd", "create", "second", "--quiet")
	select {
	case r := <-got:
		if r.Seq != recs[0].Seq+1 || r.Issue == nil || r.Issue.Title != "second" {
			t.Errorf("followed = %+v", r)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the new record never arrived")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("follow ended with %v", err)
	}
}

// TestLiveJournalHTTP checks the server's journal against the CLI's and sees
// a mutation arrive over events:watch. It needs LB_TEST_BD_SERVE_URL.
func TestLiveJournalHTTP(t *testing.T) {
	url := os.Getenv("LB_TEST_BD_SERVE_URL")
	if url == "" {
		t.Skip("LB_TEST_BD_SERVE_URL not set")
	}
	ctx := context.Background()
	cli := beads.NewCLI(beads.NewRunner(""), "")
	h, err := beads.NewHTTP(ctx, beads.HTTPConfig{BaseURL: url}, cli)
	if err != nil {
		t.Fatal(err)
	}
	scope := beads.Scope{Project: h.ServerInfo().RepoRoot}
	fromHTTP, err := h.JournalRead(ctx, 0, 0, scope)
	if err != nil {
		t.Fatal(err)
	}
	fromCLI, err := cli.JournalRead(ctx, 0, 0, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromHTTP) == 0 || len(fromHTTP) != len(fromCLI) {
		t.Fatalf("http %d records, cli %d", len(fromHTTP), len(fromCLI))
	}
	for i := range fromHTTP {
		a, b := fromHTTP[i], fromCLI[i]
		if a.Seq != b.Seq || a.Op != b.Op || a.IssueID != b.IssueID || !reflect.DeepEqual(a.Dep, b.Dep) || (a.Issue == nil) != (b.Issue == nil) {
			t.Fatalf("record %d differs: http %+v cli %+v", i, a, b)
		}
	}

	head := fromHTTP[len(fromHTTP)-1].Seq
	got := make(chan beads.JournalRecord, 8)
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = h.JournalFollow(fctx, head, scope, func(r beads.JournalRecord) error {
			got <- r
			return nil
		})
	}()
	time.Sleep(500 * time.Millisecond)
	run(t, scope.Project, "bd", "create", "journal live check", "--quiet")
	select {
	case r := <-got:
		if r.Seq != head+1 || r.Op != beads.JournalOpCreate {
			t.Errorf("followed = %+v", r)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the new record never arrived over events:watch")
	}
}
