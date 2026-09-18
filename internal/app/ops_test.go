// lb-lh5
package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestFocusPutsClaimedWorkInActiveSection(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	claimedAt := now.Add(-30 * time.Minute)
	f.add(domain.Issue{
		ID: "lb-mine", Title: "My task", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &claimedAt,
	})
	f.add(domain.Issue{
		ID: "lb-theirs", Title: "Someone else", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "other"}, UpdatedAt: &claimedAt,
	})
	f.add(domain.Issue{ID: "lb-ready", Title: "Ready", Priority: 1, UpdatedAt: hoursAgo(1)})

	got, err := newTestService(f).Focus(context.Background(), FocusRequest{})
	if err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if len(got.Active) != 1 || got.Active[0].ID != "lb-mine" {
		t.Errorf("active = %v, want [lb-mine]", issueIDs(got.Active))
	}
	for _, i := range got.Active {
		if i.ID == "lb-theirs" {
			t.Error("another actor's claim must not appear in MY ACTIVE WORK")
		}
	}
}

func TestStaleListsQuietClaimedWork(t *testing.T) {
	now := freezeNow(t)
	f := newFakeClient()
	old := now.Add(-10 * time.Hour)
	fresh := now.Add(-time.Hour)
	f.add(domain.Issue{
		ID: "lb-stale", Title: "Abandoned", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &old,
	})
	f.add(domain.Issue{
		ID: "lb-fresh", Title: "Moving", Status: domain.StatusInProgress, Priority: 1,
		Assignee: &domain.Actor{Name: "operator"}, UpdatedAt: &fresh,
	})
	got, err := newTestService(f).Stale(context.Background(), StaleRequest{})
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].ID != "lb-stale" {
		t.Errorf("stale = %v, want [lb-stale]", issueIDs(got.Issues))
	}
}

func TestDoctorWarnsWhenActorMissing(t *testing.T) {
	svc := newTestService(newFakeClient())
	svc.Actor = ""
	got, err := svc.Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if got.Health.Status != domain.HealthWarning && got.Health.Status != domain.HealthError {
		t.Errorf("health = %s, want warning when actor is unset", got.Health.Status)
	}
	found := false
	for _, c := range got.Health.Checks {
		if c.Name == "actor" && c.Level == domain.HealthWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("checks = %+v, want an actor warning", got.Health.Checks)
	}
}

func TestDoctorFlagsOpenChildOfClosedParent(t *testing.T) {
	// lb-uvj
	f := newFakeClient()
	closed := domain.StatusClosed
	parent := "lb-epic"
	f.add(domain.Issue{ID: parent, Title: "Epic", Status: closed, Type: domain.TypeEpic, Priority: 2})
	f.add(domain.Issue{ID: "lb-child", Title: "Orphaned child", Status: domain.StatusOpen, Priority: 1, ParentID: &parent})
	f.add(domain.Issue{ID: "lb-loose", Title: "Claimed without owner", Status: domain.StatusInProgress, Priority: 1})
	got, err := newTestService(f).Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	var check *domain.HealthCheck
	for i := range got.Health.Checks {
		if got.Health.Checks[i].Name == "parent_status" {
			check = &got.Health.Checks[i]
			break
		}
	}
	if check == nil {
		t.Fatalf("missing parent_status check: %+v", got.Health.Checks)
	}
	if check.Level != domain.HealthWarning {
		t.Errorf("level = %s, want warning", check.Level)
	}
	joined := strings.Join(check.Items, "\n")
	if !strings.Contains(joined, "lb-child") || !strings.Contains(joined, "lb-epic") {
		t.Errorf("items = %v, want closed-parent child", check.Items)
	}
	if !strings.Contains(joined, "lb-loose") {
		t.Errorf("items = %v, want unclaimed in_progress", check.Items)
	}
}

func TestDoctorFixCreatesCache(t *testing.T) {
	// lb-uvj
	tmp := t.TempDir()
	t.Setenv("LB_CONFIG", filepath.Join(tmp, "cfg", "config.toml"))
	t.Setenv("LB_CACHE_DIR", filepath.Join(tmp, "cache"))
	got, err := newTestService(newFakeClient()).Doctor(context.Background(), DoctorRequest{Fix: true})
	if err != nil {
		t.Fatalf("Doctor --fix: %v", err)
	}
	if len(got.Fixes) == 0 {
		t.Fatal("expected fixes")
	}
	if _, err := os.Stat(filepath.Join(tmp, "cache")); err != nil {
		t.Fatalf("cache dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "cfg", "config.toml")); err != nil {
		t.Fatalf("config: %v", err)
	}
}

// stubBD installs an executable named bd on PATH that answers `bd version
// --json`, so doctor can be exercised without a real Beads install. lb-dhv
func stubBD(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bd")
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = \"version\" ]; then\n    echo '{\"version\":\"1.0.5\",\"schema_version\":1}'\n    exit 0\n  fi\ndone\necho 'Error: no beads database found' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub bd: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func doctorCheck(t *testing.T, report *DoctorReport, name string) domain.HealthCheck {
	t.Helper()
	for _, c := range report.Health.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing %q check: %+v", name, report.Health.Checks)
	return domain.HealthCheck{}
}

// lb-dhv
func TestDoctorWithoutClientDoesNotBlameMissingBDWhenBDExists(t *testing.T) {
	stubBD(t)
	svc := &Service{Config: config.Default()}
	got, err := svc.Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, c := range got.Health.Checks {
		if strings.Contains(c.Summary, "was not found") {
			t.Errorf("doctor claims bd is missing while bd is on PATH: %+v", c)
		}
	}
	version := doctorCheck(t, got, "bd_version")
	if version.Level == domain.HealthError {
		t.Errorf("bd_version = %+v, want a readable version", version)
	}
	if got.BDVersion != "1.0.5" {
		t.Errorf("BDVersion = %q, want 1.0.5", got.BDVersion)
	}
}

// lb-dhv
func TestDoctorWithoutClientStillReportsMissingBD(t *testing.T) {
	cfg := config.Default()
	cfg.General.BDBinary = "lb-test-no-such-beads-binary"
	got, err := (&Service{Config: cfg}).Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	check := doctorCheck(t, got, "bd")
	if check.Level != domain.HealthError {
		t.Errorf("bd check = %+v, want error", check)
	}
	if !strings.Contains(check.Summary, "was not found") {
		t.Errorf("summary = %q, want a not-found diagnosis", check.Summary)
	}
}

// Schema and JSON decoding cannot be judged without a workspace; doctor must
// say so rather than report a parse failure it never observed. lb-dhv
func TestDoctorWithoutClientDoesNotInventSchemaFailure(t *testing.T) {
	stubBD(t)
	got, err := (&Service{Config: config.Default()}).Doctor(context.Background(), DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, name := range []string{"schema", "json_parse"} {
		for _, c := range got.Health.Checks {
			if c.Name == name && c.Level == domain.HealthError {
				t.Errorf("%s reported as error without a workspace: %+v", name, c)
			}
		}
	}
}
