// lb-17y
package golden

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/cli"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

func TestCLIGoldens(t *testing.T) {
	now := time.Date(2026, 8, 28, 15, 11, 0, 0, time.UTC)
	app.Now = func() time.Time { return now }
	output.Clock = func() time.Time { return now }
	t.Cleanup(func() {
		app.Now = time.Now
		output.Clock = time.Now
	})
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "80")
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	f := app.NewMemClient()
	f.Add(domain.Issue{ID: "lb-1", Title: "Add typed bd adapter", Priority: 1, Type: domain.TypeTask, Labels: []string{"cli"}})
	f.Add(domain.Issue{ID: "lb-2", Title: "Implement lb status", Priority: 1, Type: domain.TypeTask})
	f.Dep("lb-2", "lb-1")

	cases := []struct {
		name string
		args []string
	}{
		{"status.txt", []string{"status", "--ascii", "--color", "never"}},
		{"status.json", []string{"status", "--json"}},
		{"ready.txt", []string{"ready", "--ascii", "--color", "never"}},
		{"ready.json", []string{"ready", "--json"}},
		{"next.txt", []string{"next", "--ascii", "--color", "never"}},
		{"next.json", []string{"next", "--json"}},
		{"why.txt", []string{"why", "lb-2", "--ascii", "--color", "never"}},
		{"why.json", []string{"why", "lb-2", "--json"}},
		{"graph.txt", []string{"graph", "lb-1", "--ascii", "--color", "never"}},
		{"doctor.txt", []string{"doctor", "--ascii", "--color", "never"}},
		{"doctor.json", []string{"doctor", "--json"}},
		{"ready-narrow.txt", []string{"ready", "--ascii", "--color", "never"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "ready-narrow.txt" {
				t.Setenv("COLUMNS", "40")
			}
			got := runCLI(t, f, tc.args...)
			path := filepath.Join("testdata", tc.name)
			if os.Getenv("UPDATE_GOLDENS") == "1" {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s (run UPDATE_GOLDENS=1): %v", path, err)
			}
			if got != string(want) {
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}
		})
	}
}

func runCLI(t *testing.T, client *app.MemClient, args ...string) string {
	t.Helper()
	cfg := config.Default()
	refreshed := time.Date(2026, 8, 28, 15, 11, 0, 0, time.UTC)
	ws := workspace.Workspace{
		RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.0.5",
		StorageMode: workspace.StorageEmbedded, LastRefreshedAt: refreshed,
	}
	svc := app.NewService(client, cfg, ws, "operator", nil)
	var out, errOut bytes.Buffer
	code := cli.Execute(cli.Options{
		Service: svc,
		Stdout:  &out,
		Stderr:  &errOut,
		Stdin:   bytes.NewReader(nil),
		Args:    args,
	})
	if code != 0 {
		t.Fatalf("lb %v exit %d\nstderr: %s\nstdout: %s", args, code, errOut.String(), out.String())
	}
	return out.String()
}
