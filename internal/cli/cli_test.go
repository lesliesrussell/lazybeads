// lb-58x
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

func testCLI(t *testing.T, client *app.MemClient, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cfg := config.Default()
	ws := workspace.Workspace{RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.0.5", StorageMode: workspace.StorageEmbedded}
	svc := app.NewService(client, cfg, ws, "operator", nil)
	var out, errOut bytes.Buffer
	code = Execute(Options{
		Service: svc,
		Stdout:  &out,
		Stderr:  &errOut,
		Stdin:   bytes.NewReader(nil),
		Args:    args,
	})
	return out.String(), errOut.String(), code
}

func fixtureCLI(t *testing.T) *app.MemClient {
	t.Helper()
	f := app.NewMemClient()
	f.Add(domain.Issue{ID: "lb-1", Title: "Add typed bd adapter", Priority: 1, Type: domain.TypeTask, Labels: []string{"cli"}})
	f.Add(domain.Issue{ID: "lb-2", Title: "Implement lb status", Priority: 1, Type: domain.TypeTask})
	f.Dep("lb-2", "lb-1")
	return f
}

func TestStatusJSONEnvelopeContainsCounts(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, out)
	}
	for _, key := range []string{"schema_version", "command", "workspace", "data", "warnings", "generated_at"} {
		if _, ok := env[key]; !ok {
			t.Errorf("envelope missing %q", key)
		}
	}
	if env["command"] != "status" {
		t.Errorf("command = %v", env["command"])
	}
	data, _ := env["data"].(map[string]any)
	counts, _ := data["counts"].(map[string]any)
	if counts["ready"] != float64(1) {
		t.Errorf("ready = %v, want 1", counts["ready"])
	}
}

func TestStatusHumanMentionsReadyAndActor(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "status")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	for _, want := range []string{"lazybeads", "Beads", "Workspace", "Work", "Ready:", "Actor:", "operator"} {
		if !strings.Contains(out, want) {
			t.Errorf("status human missing %q in:\n%s", want, out)
		}
	}
}

func TestReadyJSONListsUnblockedIssue(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "ready", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	if !strings.Contains(out, `"lb-1"`) {
		t.Errorf("ready json should include lb-1:\n%s", out)
	}
	if strings.Contains(out, `"lb-2"`) {
		t.Errorf("ready json must not include blocked lb-2:\n%s", out)
	}
}

func TestShowHumanSuggestsClaim(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "show", "lb-1")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	if !strings.Contains(out, "Add typed bd adapter") {
		t.Errorf("show missing title:\n%s", out)
	}
	if !strings.Contains(out, "lb claim lb-1") {
		t.Errorf("show should suggest claim:\n%s", out)
	}
}

func TestListJSONAndSearchRanking(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "list", "--json")
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, out)
	}
	if !strings.Contains(out, `"lb-1"`) || !strings.Contains(out, `"lb-2"`) {
		t.Errorf("list json should include both issues:\n%s", out)
	}

	out, _, code = testCLI(t, fixtureCLI(t), "search", "adapter", "--json")
	if code != 0 {
		t.Fatalf("search exit %d: %s", code, out)
	}
	if !strings.Contains(out, `"lb-1"`) {
		t.Errorf("search adapter should find lb-1:\n%s", out)
	}
}

func TestSearchEmptyQueryExitsUsage(t *testing.T) {
	out, errOut, code := testCLI(t, fixtureCLI(t), "search")
	if code != domain.ExitUsage {
		t.Fatalf("exit %d, want %d; stdout=%s stderr=%s", code, domain.ExitUsage, out, errOut)
	}
}

func TestShowMissingIssueExitsRuntime(t *testing.T) {
	_, errOut, code := testCLI(t, fixtureCLI(t), "show", "nope")
	if code != domain.ExitRuntime {
		t.Fatalf("exit %d, want %d; stderr=%s", code, domain.ExitRuntime, errOut)
	}
	if !strings.Contains(errOut, "not found") {
		t.Errorf("stderr = %q, want a not-found diagnosis", errOut)
	}
}

func TestVersionPrintsProductVersion(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "version")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	if !strings.Contains(out, "lazybeads 0.1.0") {
		t.Errorf("version = %q", out)
	}
}

func TestNextJSONExplainsTheRecommendation(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "next", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("next --json is not JSON: %v\n%s", err, out)
	}
	if env["command"] != "next" {
		t.Errorf("command = %v", env["command"])
	}
	data, _ := env["data"].(map[string]any)
	rec, _ := data["recommendation"].(map[string]any)
	issue, _ := rec["issue"].(map[string]any)
	if issue["id"] != "lb-1" {
		t.Errorf("recommended %v, want lb-1", issue["id"])
	}
	factors, _ := rec["factors"].([]any)
	if len(factors) == 0 {
		t.Error("JSON recommendation must include factors")
	}
}

func TestNextHumanNamesTheTaskAndReason(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "next")
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, out)
	}
	for _, want := range []string{"Recommended next task", "lb-1", "Add typed bd adapter", "Reason:", "lb claim lb-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("next human missing %q in:\n%s", want, out)
		}
	}
}

func TestNextUnknownStrategyExitsUsage(t *testing.T) {
	_, errOut, code := testCLI(t, fixtureCLI(t), "next", "--strategy", "magic")
	if code != domain.ExitUsage {
		t.Fatalf("exit %d, want %d; stderr=%s", code, domain.ExitUsage, errOut)
	}
}

func TestClaimWithoutYesRefusesNonInteractive(t *testing.T) {
	_, errOut, code := testCLI(t, fixtureCLI(t), "claim", "lb-1")
	if code != domain.ExitUsage {
		t.Fatalf("exit %d, want %d; stderr=%s", code, domain.ExitUsage, errOut)
	}
	if !strings.Contains(errOut, "--yes") {
		t.Errorf("stderr = %q, want a --yes hint", errOut)
	}
}

func TestClaimYesUpdatesIssue(t *testing.T) {
	f := fixtureCLI(t)
	out, errOut, code := testCLI(t, f, "claim", "lb-1", "--yes")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, "Claimed lb-1") {
		t.Errorf("stdout = %s", out)
	}
}

func TestClaimDryRunDoesNotMutate(t *testing.T) {
	f := fixtureCLI(t)
	out, _, code := testCLI(t, f, "claim", "lb-1", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if !strings.Contains(out, "Dry run") && !strings.Contains(out, "bd ") {
		t.Errorf("dry-run output = %s", out)
	}
	got, err := f.Show(context.Background(), "lb-1", beads.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusOpen {
		t.Errorf("dry-run mutated status to %s", got.Status)
	}
}

func TestCloseYesListsNewlyReady(t *testing.T) {
	f := fixtureCLI(t)
	out, errOut, code := testCLI(t, f, "close", "lb-1", "--reason", "done", "--yes")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, "Closed lb-1") {
		t.Errorf("stdout = %s", out)
	}
	if !strings.Contains(out, "lb-2") {
		t.Errorf("expected newly ready lb-2 in:\n%s", out)
	}
}

func TestCreateYes(t *testing.T) {
	f := fixtureCLI(t)
	out, errOut, code := testCLI(t, f, "create", "Handle schema mismatch", "--type", "bug", "--priority", "1", "--yes")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("stdout = %s", out)
	}
}

func TestDepAddDryRunKeepsArgumentOrder(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "dep", "add", "lb-2", "lb-1", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if !strings.Contains(out, "dep add lb-2 lb-1") {
		t.Errorf("argv order lost in:\n%s", out)
	}
}

func TestCompletionBashWorksWithoutWorkspace(t *testing.T) {
	// lb-wlu
	missing := t.TempDir()
	var out, errOut bytes.Buffer
	code := Execute(Options{
		Stdout: &out,
		Stderr: &errOut,
		Stdin:  bytes.NewReader(nil),
		Args:   []string{"--project", missing, "completion", "bash"},
	})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	script := out.String()
	if !strings.Contains(script, "lb") || (!strings.Contains(script, "complete") && !strings.Contains(script, "COMPREPLY")) {
		t.Fatalf("expected a bash completion script, got:\n%s", script)
	}
}

func TestManPageWorksWithoutWorkspace(t *testing.T) {
	// lb-wlu
	missing := t.TempDir()
	var out, errOut bytes.Buffer
	code := Execute(Options{
		Stdout: &out,
		Stderr: &errOut,
		Stdin:  bytes.NewReader(nil),
		Args:   []string{"--project", missing, "man"},
	})
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	page := out.String()
	if !strings.Contains(page, ".SH NAME") {
		t.Fatalf("man page missing NAME section:\n%s", page)
	}
	if !strings.Contains(page, "lb") {
		t.Fatalf("man page missing lb:\n%s", page)
	}
}

func TestDoctorFixWritesCompletions(t *testing.T) {
	// lb-uvj
	tmp := t.TempDir()
	t.Setenv("LB_CONFIG", filepath.Join(tmp, "config.toml"))
	t.Setenv("LB_CACHE_DIR", filepath.Join(tmp, "cache"))
	t.Setenv("LB_DATA_DIR", filepath.Join(tmp, "data"))
	out, errOut, code := testCLI(t, fixtureCLI(t), "doctor", "--fix")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, errOut, out)
	}
	bash := filepath.Join(tmp, "data", "completions", "lb.bash")
	if _, err := os.Stat(bash); err != nil {
		t.Fatalf("completion script: %v\nstdout=%s", err, out)
	}
	if !strings.Contains(out, "Fixes") && !strings.Contains(out, "wrote") {
		t.Errorf("expected fix report in:\n%s", out)
	}
}

func TestDoctorJSONHasHealth(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "doctor", "--json")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if !strings.Contains(out, `"health"`) {
		t.Errorf("doctor json missing health:\n%s", out)
	}
}

func TestFocusJSONListsActiveWork(t *testing.T) {
	f := fixtureCLI(t)
	_, err := f.Claim(context.Background(), "lb-1", beads.ClaimInput{Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := testCLI(t, f, "focus", "--json")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if !strings.Contains(out, `"lb-1"`) {
		t.Errorf("focus json should include claimed lb-1:\n%s", out)
	}
}

func TestWhyReadyIssue(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "why", "lb-1")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if !strings.Contains(out, "ready") {
		t.Errorf("why = %s", out)
	}
}

func TestSyncNeverSaysCleanWithoutData(t *testing.T) {
	out, _, code := testCLI(t, fixtureCLI(t), "sync", "status")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, out)
	}
	if strings.Contains(strings.ToLower(out), "clean") {
		t.Errorf("must not invent clean sync: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "unknown") {
		t.Errorf("expected unknown sync, got %s", out)
	}
}
