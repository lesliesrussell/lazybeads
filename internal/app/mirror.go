// lb-4gm.5
package app

import (
	"context"
	"sync"

	"github.com/lesliesrussell/lazybeads/internal/mirror"
)

// StartMirror puts a journal-fed copy of the workspace in front of the
// client, so views read locally instead of asking bd each time. It is for
// long-lived sessions (the TUI, --watch): building the copy costs more than
// one query. Without an events journal it does nothing. The returned stop
// function ends the follower and is never nil.
func (s *Service) StartMirror(ctx context.Context) func() {
	if s.Mirror != nil || !s.Client.Capabilities(ctx, s.Scope()).EventsJournal {
		return func() {}
	}
	m := mirror.New(s.Client, s.Scope())
	changes := make(chan struct{}, 1)
	s.changes = changes
	m.OnChange = func() {
		s.InvalidateCache()
		// Coalesce: one pending signal says "something changed".
		select {
		case changes <- struct{}{}:
		default:
		}
	}
	s.Mirror = m
	s.Client = m
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Run(ctx)
	}()
	return func() {
		cancel()
		wg.Wait()
		// A stopped copy answers nothing and passes every read to bd, so it
		// stays in place: swapping s.Client here would race with reads still
		// in flight as the session exits.
		s.InvalidateCache()
	}
}

// Refresh drops cached reads and asks the mirror, if any, to rebuild from
// current state — for a sync or `bd sql` change the journal never records.
func (s *Service) Refresh() {
	s.InvalidateCache()
	if s.Mirror != nil {
		s.Mirror.Rebaseline()
	}
}

// Changes signals, coalesced, whenever the mirror applies records or changes
// state. It is nil when no mirror has run, so a select on it never fires.
// lb-4gm.6
func (s *Service) Changes() <-chan struct{} { return s.changes }

// LiveMode says how views stay current, for the TUI header and --watch.
// lb-4gm.6
type LiveMode struct {
	// Kind is "live" (the journal drives updates), "syncing" (the copy is
	// being built; reads go to bd) or "polling" (no journal: views refresh
	// on a timer).
	Kind string `json:"kind"`
	// Via is "bd serve" or "bd".
	Via string `json:"via"`
	// Reason explains polling, when known.
	Reason string `json:"reason,omitempty"`
}

// LiveMode reports the current mode.
func (s *Service) LiveMode() LiveMode {
	via := "bd"
	if s.Transport.Kind == "http" {
		via = "bd serve"
	}
	if s.Mirror == nil {
		return LiveMode{Kind: "polling", Via: via, Reason: "no events journal"}
	}
	st := s.Mirror.Status()
	switch st.State {
	case mirror.StateLive:
		return LiveMode{Kind: "live", Via: via}
	case mirror.StateSyncing:
		return LiveMode{Kind: "syncing", Via: via}
	}
	return LiveMode{Kind: "polling", Via: via, Reason: st.Reason}
}
