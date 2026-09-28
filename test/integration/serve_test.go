// lb-4gm.2
package integration

import (
	"context"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// TestLiveServeMatchesCLI checks that every read the HTTP transport answers
// agrees with the bd CLI on the same workspace. `bd serve` needs a Dolt
// server workspace, so this runs only when LB_TEST_BD_SERVE_URL names one.
func TestLiveServeMatchesCLI(t *testing.T) {
	url := os.Getenv("LB_TEST_BD_SERVE_URL")
	if url == "" {
		t.Skip("LB_TEST_BD_SERVE_URL not set")
	}
	ctx := context.Background()
	cli := beads.NewCLI(beads.NewRunner(""), "")
	h, err := beads.NewHTTP(ctx, beads.HTTPConfig{BaseURL: url}, cli)
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	scope := beads.Scope{Project: h.ServerInfo().RepoRoot}

	ids := func(issues []domain.Issue, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(issues))
		for _, i := range issues {
			out = append(out, i.ID)
		}
		sort.Strings(out)
		return out
	}
	for _, q := range []beads.ReadyQuery{{Scope: scope}, {Scope: scope, Limit: 5}} {
		if a, b := ids(h.Ready(ctx, q)), ids(cli.Ready(ctx, q)); !reflect.DeepEqual(a, b) {
			t.Errorf("ready %+v: http %v, cli %v", q, a, b)
		}
	}
	all := beads.ListQuery{Scope: scope, All: true, Limit: 0}
	listed := ids(h.List(ctx, all))
	if b := ids(cli.List(ctx, all)); !reflect.DeepEqual(listed, b) {
		t.Errorf("list --all: http %v, cli %v", listed, b)
	}
	// Default page limits truncate both sides; the same issues must survive.
	for _, q := range []beads.ListQuery{{Scope: scope}, {Scope: scope, All: true}, {Scope: scope, Limit: 7}, {Scope: scope, Limit: 60}} {
		a, b := ids(h.List(ctx, q)), ids(cli.List(ctx, q))
		if !reflect.DeepEqual(a, b) {
			t.Errorf("list %+v: http %v, cli %v", q, a, b)
		}
	}
	hs, err := h.Stats(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := cli.Stats(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if hs != cs {
		t.Errorf("stats: http %+v, cli %+v", hs, cs)
	}
	for _, id := range listed {
		hd, err := h.Show(ctx, id, scope)
		if err != nil {
			t.Fatalf("http show %s: %v", id, err)
		}
		cd, err := cli.Show(ctx, id, scope)
		if err != nil {
			t.Fatalf("cli show %s: %v", id, err)
		}
		if hd.Status != cd.Status || hd.Title != cd.Title || len(hd.Dependencies) != len(cd.Dependencies) || len(hd.Dependents) != len(cd.Dependents) {
			t.Errorf("show %s: http %+v\ncli %+v", id, hd.Issue, cd.Issue)
		}
	}
}
