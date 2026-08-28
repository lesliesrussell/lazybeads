// lb-1td
package workspace

import (
	"context"
	"sync"
)

// LockManager serializes LazyBeads' own writes against a workspace. It does not
// replace the atomic guarantees Beads provides; it only prevents LazyBeads from
// issuing avoidable concurrent mutations.
type LockManager interface {
	WithWriteLock(ctx context.Context, workspaceID string, fn func(context.Context) error) error
}

// InProcessLocks keys a mutex per canonical workspace identity.
type InProcessLocks struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

// NewInProcessLocks returns an empty lock manager.
func NewInProcessLocks() *InProcessLocks {
	return &InProcessLocks{locks: map[string]chan struct{}{}}
}

// WithWriteLock runs fn while holding the workspace write lock. A cancelled
// context aborts the wait rather than blocking indefinitely.
func (l *InProcessLocks) WithWriteLock(ctx context.Context, workspaceID string, fn func(context.Context) error) error {
	if workspaceID == "" {
		workspaceID = "unknown"
	}

	l.mu.Lock()
	ch, ok := l.locks[workspaceID]
	if !ok {
		// A buffered channel of one is a mutex that also honours a context.
		ch = make(chan struct{}, 1)
		l.locks[workspaceID] = ch
	}
	l.mu.Unlock()

	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-ch }()

	return fn(ctx)
}
