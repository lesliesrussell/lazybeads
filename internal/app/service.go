// lb-cn6
package app

import (
	"context"
	"sync"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// Now is the clock used for age and staleness reasoning. Tests replace it to
// make output deterministic.
var Now = time.Now

// Service is the application layer every interface calls. The CLI and the TUI
// both go through it, so the TUI can hold no business logic of its own.
type Service struct {
	Client    beads.Client
	Config    config.Config
	Workspace workspace.Workspace
	Actor     string
	Locks     workspace.LockManager

	cache *cache
}

// NewService wires a service around a client and a resolved workspace.
func NewService(client beads.Client, cfg config.Config, ws workspace.Workspace, actor string, locks workspace.LockManager) *Service {
	if locks == nil {
		locks = workspace.NewInProcessLocks()
	}
	return &Service{
		Client:    client,
		Config:    cfg,
		Workspace: ws,
		Actor:     actor,
		Locks:     locks,
		cache:     newCache(5 * time.Second),
	}
}

// Scope returns the execution scope for this service's workspace.
func (s *Service) Scope() beads.Scope {
	return s.Workspace.Scope(s.Config.General.Timeout.Duration())
}

// WorkspaceRef renders the workspace identity carried by every JSON envelope.
func (s *Service) WorkspaceRef() domain.WorkspaceRef {
	ref := domain.WorkspaceRef{RootPath: s.Workspace.RootPath}
	if s.Workspace.Rig != "" {
		rig := s.Workspace.Rig
		ref.Rig = &rig
	}
	return ref
}

// InvalidateCache drops all cached reads. It is called after every successful
// mutation so no stale value can be presented as confirmed state.
func (s *Service) InvalidateCache() {
	if s.cache != nil {
		s.cache.clear()
	}
}

// cache is a tiny TTL store for read results within a single command or TUI
// refresh cycle. It is in-memory only and never authoritative.
type cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

type cacheEntry struct {
	value any
	at    time.Time
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, entries: map[string]cacheEntry{}}
}

func (c *cache) get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Since(entry.at) > c.ttl {
		return nil, false
	}
	return entry.value, true
}

func (c *cache) put(key string, value any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{value: value, at: time.Now()}
}

func (c *cache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]cacheEntry{}
}

// cachedIssues memoizes a read for the cache TTL.
func cachedIssues(ctx context.Context, s *Service, key string, fetch func() ([]domain.Issue, error)) ([]domain.Issue, error) {
	if v, ok := s.cache.get(key); ok {
		if issues, ok := v.([]domain.Issue); ok {
			return issues, nil
		}
	}
	issues, err := fetch()
	if err != nil {
		return nil, err
	}
	s.cache.put(key, issues)
	return issues, nil
}
