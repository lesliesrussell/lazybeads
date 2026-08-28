// lb-58x
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/app"
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
