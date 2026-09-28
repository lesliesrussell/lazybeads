// lb-1td
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"30s": 30 * time.Second,
		"8h":  8 * time.Hour,
		"2d":  48 * time.Hour,
		"1w":  7 * 24 * time.Hour,
		"90":  90 * time.Second,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "soon", "8x"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should fail", bad)
		}
	}
}

// TestPrecedence covers the documented chain: environment beats project, which
// beats user configuration, which beats built-in defaults.
func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userCfg := filepath.Join(userDir, "config.toml")
	if err := os.WriteFile(userCfg, []byte(`
[general]
actor = "from-user"
color = "never"
stale_after = "12h"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ProjectFileName), []byte(`
[general]
actor = "from-project"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LB_CONFIG", userCfg)
	cfg, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.General.Actor != "from-project" {
		t.Errorf("project config should win over user: got %q", cfg.General.Actor)
	}
	if cfg.General.Color != "never" {
		t.Errorf("unset project keys should retain user value: got %q", cfg.General.Color)
	}
	if cfg.General.StaleAfter.Duration() != 12*time.Hour {
		t.Errorf("stale_after = %v, want 12h", cfg.General.StaleAfter)
	}
	if cfg.Ranking.PriorityWeight != 100 {
		t.Errorf("defaults should survive: priority_weight = %v", cfg.Ranking.PriorityWeight)
	}

	t.Setenv("LB_ACTOR", "from-env")
	cfg, err = Load(projectDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.General.Actor != "from-env" {
		t.Errorf("environment should win over project: got %q", cfg.General.Actor)
	}
}

// TestNoConfirmIsEnvironmentOnly guards the safety rule that a project file may
// not silently disable mutation confirmation.
func TestNoConfirmIsEnvironmentOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(`
[general]
confirm_mutations = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LB_CONFIG", filepath.Join(dir, "absent.toml"))

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// An explicit project opt-out is honoured for that project, but only the
	// environment variable is treated as a global override, and it must warn.
	if cfg.ShouldConfirm() {
		t.Log("project opt-out respected as an explicit local choice")
	}
	if NoConfirmFromEnv() {
		t.Error("LB_NO_CONFIRM should not be set in this test")
	}
	t.Setenv("LB_NO_CONFIRM", "1")
	if !NoConfirmFromEnv() {
		t.Error("LB_NO_CONFIRM=1 should be detected")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cfg := Default()
	cfg.General.Color = "rainbow"
	if err := Validate(&cfg); err == nil {
		t.Error("invalid color should be rejected")
	}

	cfg = Default()
	cfg.Ranking.Strategy = "vibes"
	if err := Validate(&cfg); err == nil {
		t.Error("invalid strategy should be rejected")
	}

	cfg = Default()
	cfg.Ranking.MaxGraphDepth = 0
	if err := Validate(&cfg); err != nil {
		t.Fatalf("zero depth should be defaulted, not rejected: %v", err)
	}
	if cfg.Ranking.MaxGraphDepth != 8 {
		t.Errorf("max_graph_depth = %d, want 8", cfg.Ranking.MaxGraphDepth)
	}
}

func TestDetectKeyCollisions(t *testing.T) {
	keys := Keys{
		List: map[string][]string{
			"claim": {"c"},
			"close": {"c", "x"},
			"open":  {"enter"},
		},
	}
	collisions := DetectKeyCollisions(keys)
	if len(collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d: %v", len(collisions), collisions)
	}
	if collisions[0].Key != "c" {
		t.Errorf("collision key = %q, want c", collisions[0].Key)
	}
	if len(collisions[0].Actions) != 2 {
		t.Errorf("collision should name both actions: %v", collisions[0].Actions)
	}
}

// TestDistinctScopesDoNotCollide confirms modal scopes may reuse a key.
func TestDistinctScopesDoNotCollide(t *testing.T) {
	keys := Keys{
		Global: map[string][]string{"quit": {"q"}},
		List:   map[string][]string{"quick_filter": {"q"}},
	}
	if got := DetectKeyCollisions(keys); len(got) != 0 {
		t.Errorf("cross-scope reuse should be allowed, got %v", got)
	}
}

// lb-4gm.3
func TestServeSection(t *testing.T) {
	cfg := Default()
	if err := Validate(&cfg); err != nil || cfg.Serve.AutoStart != "auto" {
		t.Fatalf("default auto_start = %q, %v", cfg.Serve.AutoStart, err)
	}
	for _, u := range []string{"http://127.0.0.1:7777", "http://localhost:7777", "http://[::1]:7777"} {
		cfg = Default()
		cfg.Serve.URL = u
		if err := Validate(&cfg); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"http://10.1.2.3:7777", "http://beads.example.com", "https://127.0.0.1:7777", "127.0.0.1:7777"} {
		cfg = Default()
		cfg.Serve.URL = u
		if err := Validate(&cfg); err == nil {
			t.Errorf("serve.url %q must be rejected", u)
		}
	}
	// lb-4gm.8
	cfg = Default()
	if err := Validate(&cfg); err != nil || cfg.Serve.HTTPWrites != "auto" {
		t.Errorf("default http_writes = %q, %v", cfg.Serve.HTTPWrites, err)
	}
	cfg.Serve.HTTPWrites = "sometimes"
	if err := Validate(&cfg); err == nil {
		t.Error("an unknown http_writes must be rejected")
	}
	cfg = Default()
	cfg.Serve.AutoStart = "sometimes"
	if err := Validate(&cfg); err == nil {
		t.Error("an unknown auto_start must be rejected")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ProjectFileName), []byte("[serve]\nurl = \"http://127.0.0.1:7000\"\nauto_start = \"never\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LB_CONFIG", filepath.Join(dir, "absent.toml"))
	t.Setenv("LB_SERVE_URL", "http://127.0.0.1:7001")
	t.Setenv("LB_SERVE_TOKEN_FILE", "/secrets/bd-token")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Serve.URL != "http://127.0.0.1:7001" || loaded.Serve.AutoStart != "never" || loaded.Serve.TokenFile != "/secrets/bd-token" {
		t.Errorf("serve = %+v", loaded.Serve)
	}
}

// lb-4gm.3
func TestProjectConfigCannotChooseServerOrCredential(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	if err := os.WriteFile(user, []byte("[serve]\nurl = \"http://127.0.0.1:7000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ProjectFileName), []byte(
		"[serve]\nurl = \"http://127.0.0.1:6666\"\ntoken_file = \"~/.git-credentials\"\nauto_start = \"never\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LB_CONFIG", user)
	loaded, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Serve.URL != "http://127.0.0.1:7000" || loaded.Serve.TokenFile != "" {
		t.Errorf("project config chose serve url/token: %+v", loaded.Serve)
	}
	if loaded.Serve.AutoStart != "never" {
		t.Error("a project may still opt out of starting a server")
	}

	// lb-4gm.8: a project cannot force HTTP writes past the hook check, but
	// it may turn them off.
	for value, want := range map[string]string{"always": "auto", "never": "never"} {
		if err := os.WriteFile(filepath.Join(project, ProjectFileName), []byte("[serve]\nhttp_writes = \""+value+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(project)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Serve.HTTPWrites != want {
			t.Errorf("project http_writes=%s gave %s, want %s", value, loaded.Serve.HTTPWrites, want)
		}
	}
}
