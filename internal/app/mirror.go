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
	inner := s.Client
	m := mirror.New(inner, s.Scope())
	m.OnChange = s.InvalidateCache
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
		// A stopped copy is frozen: hand reads back to bd.
		s.Client, s.Mirror = inner, nil
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
