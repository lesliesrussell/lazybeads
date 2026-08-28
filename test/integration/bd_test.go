// lb-17y
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveBeadsWorkspace exercises lb against an ephemeral `bd init` workspace
// when bd is installed. CI should not require a network Beads install; skip
// otherwise.
func TestLiveBeadsWorkspace(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	lb := buildLB(t)
	dir := t.TempDir()
	run(t, dir, "bd", "init", "--prefix", "t", "--quiet")
	out := run(t, dir, lb, "status", "--json")
	if !strings.Contains(out, `"schema_version"`) {
		t.Fatalf("status json: %s", out)
	}
	create := run(t, dir, lb, "create", "integration root", "--type", "task", "--priority", "1", "--yes", "--json")
	if !strings.Contains(create, `"action": "create"`) && !strings.Contains(create, "Created") && !strings.Contains(create, `"id"`) {
		t.Fatalf("create: %s", create)
	}
}

func buildLB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "lb")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/lb")
	cmd.Dir = repoRoot(t)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "LB_NO_CONFIRM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
