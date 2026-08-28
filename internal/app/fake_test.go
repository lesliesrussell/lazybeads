// lb-cn6
package app

import (
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

type fakeClient = MemClient

func newFakeClient() *MemClient { return NewMemClient() }

func newTestService(f *MemClient) *Service {
	cfg := config.Default()
	ws := workspace.Workspace{RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.0.5"}
	return NewService(f, cfg, ws, "operator", workspace.NewInProcessLocks())
}

func hoursAgo(h int) *time.Time {
	t := Now().Add(-time.Duration(h) * time.Hour)
	return &t
}

func closeInput(reason string) beads.CloseInput {
	return beads.CloseInput{Reason: reason}
}

func (f *MemClient) add(issue domain.Issue) *MemClient { return f.Add(issue) }

func (f *MemClient) dep(blocked, blocker string) *MemClient { return f.Dep(blocked, blocker) }

func (f *MemClient) rel(blocked, blocker string, t domain.RelationType) *MemClient {
	return f.Rel(blocked, blocker, t)
}
