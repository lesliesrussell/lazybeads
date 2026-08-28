// lb-1td
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/beads"
)

// StorageMode describes how Beads persists this workspace.
type StorageMode string

const (
	StorageUnknown  StorageMode = "unknown"
	StorageEmbedded StorageMode = "embedded"
	StorageServer   StorageMode = "server"
)

// Workspace is the resolved target of every command.
type Workspace struct {
	RootPath        string      `json:"root_path"`
	BeadsDir        string      `json:"beads_dir"`
	Rig             string      `json:"rig,omitempty"`
	BDBinary        string      `json:"bd_binary"`
	BDVersion       string      `json:"bd_version,omitempty"`
	StorageMode     StorageMode `json:"storage_mode"`
	GitRoot         *string     `json:"git_root,omitempty"`
	IsGitFree       bool        `json:"is_git_free"`
	IsStealth       bool        `json:"is_stealth"`
	IsRemoteBacked  bool        `json:"is_remote_backed"`
	Prefix          string      `json:"prefix,omitempty"`
	LastRefreshedAt time.Time   `json:"last_refreshed_at"`
}

// ID returns the canonical identity used to key the mutation lock. Two
// selections that reach the same database must produce the same ID.
func (w Workspace) ID() string {
	if w.BeadsDir != "" {
		return "beads:" + w.BeadsDir
	}
	if w.RootPath != "" {
		return "root:" + w.RootPath
	}
	return "unknown"
}

// Selection is the user's explicit workspace request, in precedence order:
// BeadsDir, then the BEADS_DIR environment variable, then Project, then the
// working directory, then Rig, then a configured default.
type Selection struct {
	BeadsDir       string
	Project        string
	Rig            string
	WorkingDir     string
	ConfigProject  string
	ConfigBeadsDir string
	ConfigRig      string
}

// Resolve applies the documented resolution order and then asks bd to confirm
// the target. LazyBeads never inspects Beads storage internals itself.
func Resolve(ctx context.Context, client beads.Client, sel Selection, binary string) (Workspace, error) {
	scope := beads.Scope{}

	switch {
	case sel.BeadsDir != "":
		scope.BeadsDir = mustAbs(sel.BeadsDir)
	case os.Getenv("BEADS_DIR") != "":
		scope.BeadsDir = mustAbs(os.Getenv("BEADS_DIR"))
	case sel.ConfigBeadsDir != "":
		scope.BeadsDir = mustAbs(sel.ConfigBeadsDir)
	}

	switch {
	case sel.Project != "":
		scope.Project = mustAbs(sel.Project)
	case sel.WorkingDir != "":
		scope.Project = mustAbs(sel.WorkingDir)
	case sel.ConfigProject != "":
		scope.Project = mustAbs(sel.ConfigProject)
	}

	if scope.Project != "" {
		if st, err := os.Stat(scope.Project); err != nil || !st.IsDir() {
			return Workspace{}, &beads.CommandError{
				Kind:      beads.ErrWorkspaceNotFound,
				Operation: "resolve workspace",
				Cause:     fmt.Errorf("%s is not a directory", scope.Project),
				Hint:      "Check the --project path.",
			}
		}
	}

	scope.Rig = firstNonEmpty(sel.Rig, sel.ConfigRig)

	ws := Workspace{
		RootPath:    scope.Project,
		BeadsDir:    scope.BeadsDir,
		Rig:         scope.Rig,
		BDBinary:    binary,
		StorageMode: StorageUnknown,
	}

	info, err := client.Info(ctx, scope)
	if err != nil {
		return ws, err
	}
	if info.BeadsDir != "" {
		ws.BeadsDir = info.BeadsDir
		// bd reports .beads; the repository root is its parent.
		if filepath.Base(info.BeadsDir) == ".beads" {
			ws.RootPath = filepath.Dir(info.BeadsDir)
		}
	}
	ws.Prefix = info.Prefix
	ws.StorageMode = classifyStorage(info)
	if ws.RootPath == "" {
		ws.RootPath = scope.Project
	}

	if v, err := client.Version(ctx, scope); err == nil {
		ws.BDVersion = v.Version
	}

	ws.GitRoot = findGitRoot(ws.RootPath)
	ws.IsGitFree = ws.GitRoot == nil
	ws.IsStealth = detectStealth(ws.GitRoot)
	ws.LastRefreshedAt = time.Now()
	return ws, nil
}

// classifyStorage maps bd's reported database location onto a storage mode.
// An unrecognized shape stays "unknown" rather than being guessed.
func classifyStorage(info beads.Info) StorageMode {
	mode := strings.ToLower(info.Mode)
	switch {
	case strings.Contains(mode, "server"):
		return StorageServer
	case strings.Contains(mode, "embedded"), strings.Contains(mode, "direct"):
		return StorageEmbedded
	}
	path := strings.ToLower(info.DatabasePath)
	switch {
	case strings.Contains(path, "embeddeddolt"):
		return StorageEmbedded
	case strings.HasPrefix(path, "mysql://"), strings.Contains(path, "://"):
		return StorageServer
	}
	return StorageUnknown
}

// Scope converts a resolved workspace back into an execution scope.
func (w Workspace) Scope(timeout time.Duration) beads.Scope {
	return beads.Scope{
		Project:  w.RootPath,
		BeadsDir: w.BeadsDir,
		Rig:      w.Rig,
		Timeout:  timeout,
	}
}

func findGitRoot(start string) *string {
	if start == "" {
		return nil
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			out := dir
			return &out
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// detectStealth reports whether beads files are excluded from the repository,
// which is how `bd init --stealth` configures per-repo invisible usage.
func detectStealth(gitRoot *string) bool {
	if gitRoot == nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(*gitRoot, ".git", "info", "exclude"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), ".beads")
}

func mustAbs(p string) string {
	if p == "" {
		return ""
	}
	if expanded, err := expandHome(p); err == nil {
		p = expanded
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
