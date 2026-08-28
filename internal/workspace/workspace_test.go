// lb-1td
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
)

func TestWorkspaceIDIsCanonical(t *testing.T) {
	a := Workspace{RootPath: "/p", BeadsDir: "/p/.beads"}
	b := Workspace{RootPath: "/other", BeadsDir: "/p/.beads"}
	if a.ID() != b.ID() {
		t.Error("two selections reaching the same database must share a lock identity")
	}
	c := Workspace{RootPath: "/p"}
	if c.ID() == a.ID() {
		t.Error("a workspace without a beads dir should key differently")
	}
}

func TestClassifyStorage(t *testing.T) {
	cases := []struct {
		info beads.Info
		want StorageMode
	}{
		{beads.Info{DatabasePath: "/p/.beads/embeddeddolt", Mode: "direct"}, StorageEmbedded},
		{beads.Info{Mode: "server"}, StorageServer},
		{beads.Info{DatabasePath: "mysql://host/db"}, StorageServer},
		{beads.Info{}, StorageUnknown},
	}
	for _, c := range cases {
		if got := classifyStorage(c.info); got != c.want {
			t.Errorf("classifyStorage(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestResolveActorPrecedence(t *testing.T) {
	t.Setenv("LB_ACTOR", "")
	got, src := ResolveActor("flag-actor", "config-actor", false, "")
	if got != "flag-actor" || src != "--actor" {
		t.Errorf("flag should win: %q from %q", got, src)
	}

	t.Setenv("LB_ACTOR", "env-actor")
	got, src = ResolveActor("", "config-actor", false, "")
	if got != "env-actor" || src != "LB_ACTOR" {
		t.Errorf("environment should beat config: %q from %q", got, src)
	}

	t.Setenv("LB_ACTOR", "")
	got, src = ResolveActor("", "config-actor", false, "")
	if got != "config-actor" || src != "config" {
		t.Errorf("config should apply: %q from %q", got, src)
	}

	// Git identity is only consulted when explicitly enabled; otherwise the
	// unconfigured state must be reported rather than assumed.
	got, _ = ResolveActor("", "", false, ".")
	if got != "" {
		t.Errorf("actor should be unconfigured, got %q", got)
	}
}

func TestFindGitRoot(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if root := findGitRoot(nested); root != nil {
		t.Errorf("no .git should yield nil, got %q", *root)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := findGitRoot(nested)
	if root == nil {
		t.Fatal("git root should be discovered by walking upward")
	}
	// t.TempDir may hand back a symlinked path on macOS; compare resolved forms.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(*root)
	if got != want {
		t.Errorf("git root = %q, want %q", got, want)
	}
}

// TestWriteLockSerializes proves LazyBeads never issues two concurrent writes
// against one workspace.
func TestWriteLockSerializes(t *testing.T) {
	locks := NewInProcessLocks()
	var mu sync.Mutex
	concurrent, peak := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = locks.WithWriteLock(context.Background(), "ws-1", func(context.Context) error {
				mu.Lock()
				concurrent++
				if concurrent > peak {
					peak = concurrent
				}
				mu.Unlock()

				time.Sleep(time.Millisecond)

				mu.Lock()
				concurrent--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak)
	}
}

func TestWriteLockAllowsDistinctWorkspaces(t *testing.T) {
	locks := NewInProcessLocks()
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_ = locks.WithWriteLock(context.Background(), "ws-a", func(context.Context) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started

	finished := make(chan struct{})
	go func() {
		_ = locks.WithWriteLock(context.Background(), "ws-b", func(context.Context) error { return nil })
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Error("a different workspace must not be blocked")
	}
	close(done)
}

func TestWriteLockHonoursContextCancellation(t *testing.T) {
	locks := NewInProcessLocks()
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = locks.WithWriteLock(context.Background(), "ws", func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := locks.WithWriteLock(ctx, "ws", func(context.Context) error {
		t.Error("the second writer should never run")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want a deadline error", err)
	}
	close(release)
}

func TestWriteLockPropagatesError(t *testing.T) {
	locks := NewInProcessLocks()
	sentinel := errors.New("mutation failed")
	err := locks.WithWriteLock(context.Background(), "ws", func(context.Context) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the callback's error", err)
	}
}
