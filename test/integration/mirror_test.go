// lb-4gm.5
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/mirror"
)

// TestLiveMirrorMatchesBD drives a real workspace through agent-style bd
// commands while a mirror follows its journal, and after every step checks
// that each view the mirror answers equals what bd itself answers.
func TestLiveMirrorMatchesBD(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	dir := t.TempDir()
	run(t, dir, "bd", "init", "--prefix", "m", "--quiet")
	run(t, dir, "bd", "config", "set", "events-journal", "true")
	scope := beads.Scope{Project: dir}
	cli := beads.NewCLI(beads.NewRunner(""), "")
	checkMirror(t, dir, cli, scope)
}

func bdID(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out := run(t, dir, "bd", append(args, "--json")...)
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &v); err != nil || v.ID == "" {
		t.Fatalf("no id from bd %v: %s", args, out)
	}
	return v.ID
}

func checkMirror(t *testing.T, dir string, cli beads.Client, scope beads.Scope) {
	m := mirror.New(cli, scope)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	epic := bdID(t, dir, "create", "epic", "--type", "epic", "-p", "2")
	a := bdID(t, dir, "create", "alpha", "-p", "1", "--parent", epic)
	b := bdID(t, dir, "create", "beta", "-p", "0", "-t", "bug")
	c := bdID(t, dir, "create", "gamma", "-p", "3")
	d := bdID(t, dir, "create", "delta", "-p", "2", "-l", "ui")
	waitLive(t, m)
	waitCaughtUp(t, m, cli, scope)
	compare(t, "initial", m, cli, scope)

	steps := []struct {
		name string
		args []string
	}{
		{"b waits on a", []string{"dep", "add", b, a}},
		{"label a", []string{"label", "add", a, "ui"}},
		{"claim a", []string{"update", a, "--claim"}},
		{"reprioritise d", []string{"update", d, "--priority", "0"}},
		{"assign c", []string{"update", c, "--assignee", "sam"}},
		{"comment on d", []string{"comment", d, "looks good"}},
		{"close a", []string{"close", a, "--reason", "done"}},
		{"reopen a", []string{"reopen", a}},
		{"unlabel d", []string{"label", "remove", d, "ui"}},
		{"drop dependency", []string{"dep", "remove", b, a}},
		{"close c", []string{"close", c, "--reason", "wontfix", "--force"}},
		{"delete d", []string{"delete", d, "--force"}},
	}
	for _, s := range steps {
		run(t, dir, "bd", s.args...)
		waitCaughtUp(t, m, cli, scope)
		compare(t, s.name, m, cli, scope)
	}

	// Edges and blocked state that bd derives while the mirror is live.
	kid := bdID(t, dir, "create", "late child", "--parent", epic)
	waitCaughtUp(t, m, cli, scope)
	compare(t, "child created while live", m, cli, scope)
	blocker := bdID(t, dir, "create", "blocker", "-p", "1")
	waiter := bdID(t, dir, "create", "waiter", "-p", "1")
	for _, s := range []struct {
		name string
		args []string
	}{
		{"waiter blocked", []string{"dep", "add", waiter, blocker}},
		{"blocker closed by status", []string{"update", blocker, "--status", "closed"}},
		{"blocker reopened", []string{"update", blocker, "--status", "open"}},
		{"blocker deleted", []string{"delete", blocker, "--force"}},
		{"child reparented away", []string{"dep", "remove", kid, epic}},
	} {
		run(t, dir, "bd", s.args...)
		waitCaughtUp(t, m, cli, scope)
		compare(t, s.name, m, cli, scope)
	}
}

func waitLive(t *testing.T, m *mirror.Mirror) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for m.Status().State != mirror.StateLive {
		if time.Now().After(deadline) {
			t.Fatalf("mirror never went live: %+v", m.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitCaughtUp(t *testing.T, m *mirror.Mirror, cli beads.Client, scope beads.Scope) {
	t.Helper()
	head, err := beads.JournalHead(context.Background(), cli, scope)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for m.Status().Checkpoint < head || m.Status().State != mirror.StateLive {
		if time.Now().After(deadline) {
			t.Fatalf("mirror stuck at %+v, head %d", m.Status(), head)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// shape is what a view shows of an issue.
type shape struct {
	ID, Status, Type, Assignee, Parent string
	Priority                           int
	Labels                             string
	Deps, Dependents                   int
}

func shapes(issues []domain.Issue) []shape {
	out := make([]shape, 0, len(issues))
	for _, i := range issues {
		labels := append([]string(nil), i.Labels...)
		sort.Strings(labels)
		s := shape{ID: i.ID, Status: string(i.Status), Type: string(i.Type), Priority: int(i.Priority),
			Labels: strings.Join(labels, ","), Deps: i.DependencyCount, Dependents: i.DependentCount}
		if i.Assignee != nil {
			s.Assignee = i.Assignee.String()
		}
		if i.ParentID != nil {
			s.Parent = *i.ParentID
		}
		out = append(out, s)
	}
	return out
}

func idList(issues []domain.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.ID)
	}
	return out
}

func compare(t *testing.T, step string, m *mirror.Mirror, cli beads.Client, scope beads.Scope) {
	t.Helper()
	ctx := context.Background()
	must := func(v []domain.Issue, err error) []domain.Issue {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return v
	}
	for _, q := range []beads.ListQuery{{Scope: scope}, {Scope: scope, All: true}} {
		got, want := must(m.List(ctx, q)), must(cli.List(ctx, q))
		if !reflect.DeepEqual(shapes(got), shapes(want)) {
			t.Errorf("%s: list all=%v\n mirror %+v\n bd     %+v", step, q.All, shapes(got), shapes(want))
		}
	}
	if got, want := idList(must(m.Ready(ctx, beads.ReadyQuery{Scope: scope}))), idList(must(cli.Ready(ctx, beads.ReadyQuery{Scope: scope}))); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: ready mirror %v, bd %v", step, got, want)
	}
	got, want := idList(must(m.Blocked(ctx, scope))), idList(must(cli.Blocked(ctx, scope)))
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: blocked mirror %v, bd %v", step, got, want)
	}
	ms, err := m.Stats(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := cli.Stats(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if ms != cs {
		t.Errorf("%s: stats mirror %+v, bd %+v", step, ms, cs)
	}
	if t.Failed() {
		t.FailNow()
	}
	_ = fmt.Sprint
}

// TestLiveMirrorOverHTTP runs the same scenario with the mirror reading and
// following bd serve. It needs LB_TEST_BD_SERVE_URL, and adds (and deletes)
// issues in that server's workspace.
func TestLiveMirrorOverHTTP(t *testing.T) {
	url := os.Getenv("LB_TEST_BD_SERVE_URL")
	if url == "" {
		t.Skip("LB_TEST_BD_SERVE_URL not set")
	}
	cli := beads.NewCLI(beads.NewRunner(""), "")
	h, err := beads.NewHTTP(context.Background(), beads.HTTPConfig{BaseURL: url}, cli)
	if err != nil {
		t.Fatal(err)
	}
	root := h.ServerInfo().RepoRoot
	checkMirror(t, root, h, beads.Scope{Project: root})
}
