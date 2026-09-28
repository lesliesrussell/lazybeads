// lb-4gm.6
package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestLiveRecordsDebounceIntoOneQuietReload(t *testing.T) {
	m, f := testModel(t)
	m = pump(m, m.Init())
	f.Add(domain.Issue{ID: "lb-9", Title: "Arrived from an agent", Priority: 1, Type: domain.TypeTask})

	next, cmd := m.Update(liveMsg{})
	m = next.(Model)
	if !m.refreshPending || cmd == nil {
		t.Fatal("a record should schedule a redraw")
	}
	next, _ = m.Update(liveMsg{})
	if !next.(Model).refreshPending {
		t.Fatal("a burst stays one pending redraw")
	}
	m.svc.InvalidateCache()
	next, cmd = m.Update(refreshMsg{})
	m = next.(Model)
	if m.refreshPending || m.loading {
		t.Error("a live redraw is quiet and clears the pending flag")
	}
	m = pump(m, cmd)
	found := false
	for _, r := range m.rows {
		found = found || r.ID == "lb-9"
	}
	if !found {
		t.Errorf("the redraw did not pick up the new issue: %+v", m.rows)
	}
}

func TestLiveReloadKeepsTheSelectedIssue(t *testing.T) {
	m, f := testModel(t)
	m = pump(m, m.Init())
	row, _ := m.currentRow()
	if row.ID != "lb-1" {
		t.Fatalf("selected %s", row.ID)
	}
	// A more urgent issue lands above it.
	f.Add(domain.Issue{ID: "lb-0", Title: "Urgent", Priority: 0, Type: domain.TypeTask})
	m.svc.InvalidateCache()
	next, cmd := m.Update(refreshMsg{})
	m = pump(next.(Model), cmd)
	if row, _ := m.currentRow(); row.ID != "lb-1" {
		t.Errorf("selection moved to %s when rows reordered", row.ID)
	}
}

func TestPollingOnlyWhenTheJournalIsNotDrivingUpdates(t *testing.T) {
	m, f := testModel(t)
	m = pump(m, m.Init())
	// Set after the synchronous load: pump would follow the timer forever.
	m.opts.PollEvery = time.Millisecond
	if !strings.Contains(m.headerLine(), "@ polling") {
		t.Errorf("header = %q", m.headerLine())
	}
	next, cmd := m.Update(pollMsg{})
	if cmd == nil || next.(Model).view != viewReady {
		t.Fatal("polling mode reloads and re-arms")
	}

	stop := m.svc.StartMirror(context.Background())
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for m.svc.LiveMode().Kind != "live" {
		if time.Now().After(deadline) {
			t.Fatalf("mode = %+v", m.svc.LiveMode())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(m.headerLine(), "* live") {
		t.Errorf("header = %q", m.headerLine())
	}
	_, cmd = m.Update(pollMsg{})
	if msg := cmd(); msg != (pollMsg{}) {
		t.Errorf("a live session only re-arms the timer, got %T", msg)
	}

	// A record committed by someone else wakes the TUI.
	woke := make(chan any, 1)
	go func() { woke <- m.waitChange()() }()
	f.AppendJournal(beads.JournalRecord{Op: beads.JournalOpUpdate, IssueID: "lb-1",
		Issue: &domain.Issue{ID: "lb-1", Title: "Add typed bd adapter", Status: domain.StatusOpen, Priority: 1}})
	select {
	case msg := <-woke:
		if _, ok := msg.(liveMsg); !ok {
			t.Errorf("woke with %T", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a journal record never reached the TUI")
	}
}

// lb-4gm.6 review regressions.

func TestUserMovesWinOverAPendingLiveReload(t *testing.T) {
	m, f := testModel(t)
	f.Add(domain.Issue{ID: "lb-4", Title: "Another ready one", Priority: 1, Type: domain.TypeTask})
	m = pump(m, m.Init())
	next, reload := m.Update(refreshMsg{}) // reload in flight, keeping the current row
	m = next.(Model)
	next, _ = m.Update(key("j"))
	m = next.(Model)
	moved, _ := m.currentRow()
	m = pump(m, reload) // the reload lands after the keypress
	if row, _ := m.currentRow(); row.ID != moved.ID {
		t.Errorf("cursor snapped back to %s after the user moved to %s", row.ID, moved.ID)
	}
}

func TestPollInANonLiveViewDoesNotSwallowKeys(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	m.opts.PollEvery = time.Hour
	next, _ := m.Update(key("m")) // memory view: not rebuilt by live reloads
	m = next.(Model)
	next, _ = m.Update(pollMsg{})
	if next.(Model).keepID != "" {
		t.Error("a reload that will not replace the rows must not pin the cursor")
	}
}

func TestStaleReplyForAnotherTabIsDropped(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	before := len(m.rows)
	next, _ := m.Update(issuesMsg{issues: []domain.Issue{{ID: "x-1"}, {ID: "x-2"}, {ID: "x-3"}}})
	if got := next.(Model); len(got.rows) != before {
		t.Errorf("an issues reply replaced the ready tab's rows: %+v", got.rows)
	}
}
