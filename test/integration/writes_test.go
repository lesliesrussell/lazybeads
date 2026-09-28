// lb-4gm.8
package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// TestLiveHTTPWrites makes every mutation lb supports through bd serve and
// checks the result through the bd CLI and the events journal. It needs
// LB_TEST_BD_SERVE_URL and writes to that server's workspace.
func TestLiveHTTPWrites(t *testing.T) {
	url := os.Getenv("LB_TEST_BD_SERVE_URL")
	if url == "" {
		t.Skip("LB_TEST_BD_SERVE_URL not set")
	}
	ctx := context.Background()
	const actor = "lb-http-writer"
	cli := beads.NewCLI(beads.NewRunner(""), "")
	h, err := beads.NewHTTP(ctx, beads.HTTPConfig{BaseURL: url, Actor: actor, AllowWrites: true}, cli)
	if err != nil {
		t.Fatal(err)
	}
	scope := beads.Scope{Project: h.ServerInfo().RepoRoot}
	head, err := beads.JournalHead(ctx, cli, scope)
	if err != nil {
		t.Fatal(err)
	}
	show := func(id string) domain.IssueDetail {
		t.Helper()
		d, err := cli.Show(ctx, id, scope)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	p1 := 1
	parent, err := h.Create(ctx, beads.CreateIssueInput{Scope: scope, Title: "http parent", Type: "epic", Labels: []string{"team"}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := h.Create(ctx, beads.CreateIssueInput{Scope: scope, Title: "http child", Priority: &p1, Labels: []string{"api"}, Parent: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); d.Title != "http child" || d.Priority != 1 || !d.HasLabel("api") || !d.HasLabel("team") ||
		d.ParentID == nil || *d.ParentID != parent.ID || d.ID != parent.ID+".1" {
		t.Fatalf("created = %+v parent %v", d.Issue, d.ParentID)
	}

	title := "http child renamed"
	if _, err := h.Update(ctx, child.ID, beads.UpdateIssueInput{Scope: scope, Title: &title, AddLabels: []string{"live"}, RemoveLabels: []string{"api"}}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); d.Title != title || !d.HasLabel("live") || d.HasLabel("api") {
		t.Errorf("updated = %+v", d.Issue)
	}

	if _, err := h.Claim(ctx, child.ID, beads.ClaimInput{Scope: scope}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); d.Status != domain.StatusInProgress || d.Assignee == nil || d.Assignee.String() != actor {
		t.Errorf("claimed = %s by %v", d.Status, d.Assignee)
	}

	blocker, err := h.Create(ctx, beads.CreateIssueInput{Scope: scope, Title: "http blocker"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AddDependency(ctx, beads.DependencyInput{Scope: scope, Blocked: child.ID, Blocker: blocker.ID}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); !hasDep(d.Dependencies, blocker.ID) {
		t.Errorf("dependency not added: %+v", d.Dependencies)
	}
	if err := h.RemoveDependency(ctx, beads.DependencyInput{Scope: scope, Blocked: child.ID, Blocker: blocker.ID}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); hasDep(d.Dependencies, blocker.ID) {
		t.Errorf("dependency not removed: %+v", d.Dependencies)
	}

	if _, err := h.Close(ctx, child.ID, beads.CloseInput{Scope: scope, Reason: "shipped over http"}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); !d.IsClosed() || d.CloseReason == nil || *d.CloseReason != "shipped over http" {
		t.Errorf("closed = %s %v", d.Status, d.CloseReason)
	}
	if _, err := h.Reopen(ctx, child.ID, beads.ReopenInput{Scope: scope, Reason: "one more thing"}); err != nil {
		t.Fatal(err)
	}
	if d := show(child.ID); d.IsClosed() || !strings.Contains(string(d.Raw), "Reopened: one more thing") {
		t.Errorf("reopened = %s, notes in %s", d.Status, d.Raw)
	}
	// Someone else claims the blocker; lb can still reassign it, as
	// `bd update --assignee` can.
	run(t, scope.Project, "bd", "update", blocker.ID, "--claim", "--actor", "someone-else")
	if _, err := h.Assign(ctx, blocker.ID, "sam", scope); err != nil {
		t.Fatal(err)
	}
	if d := show(blocker.ID); d.Assignee == nil || d.Assignee.String() != "sam" {
		t.Errorf("assigned = %v", d.Assignee)
	}

	// Every write is journaled and attributed to lb's actor.
	recs, err := cli.JournalRead(ctx, head, 0, scope)
	if err != nil {
		t.Fatal(err)
	}
	attributed := 0
	for _, r := range recs {
		if r.Actor == actor {
			attributed++
		}
	}
	if attributed < 10 {
		t.Errorf("only %d of %d new journal records carry the actor %q", attributed, len(recs), actor)
	}
}

func hasDep(deps []domain.Dependency, id string) bool {
	for _, d := range deps {
		if d.Issue.ID == id {
			return true
		}
	}
	return false
}
