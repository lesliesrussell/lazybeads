// lb-1td
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectFileName is the per-repository configuration file.
const ProjectFileName = ".lazybeads.toml"

// UserConfigPath returns the platform-conventional user configuration file.
func UserConfigPath() (string, error) {
	if explicit := os.Getenv("LB_CONFIG"); explicit != "" {
		return explicit, nil
	}
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "lazybeads", "config.toml"), nil
	case "windows":
		if dir := os.Getenv("AppData"); dir != "" {
			return filepath.Join(dir, "lazybeads", "config.toml"), nil
		}
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazybeads", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "lazybeads", "config.toml"), nil
}

// FindProjectConfig walks upward from dir looking for a project configuration
// file, stopping at the filesystem root.
func FindProjectConfig(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ProjectFileName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Load resolves configuration with the documented precedence: environment over
// project over user over built-in defaults. Command flags are applied by the
// CLI layer afterwards, since only it knows which flags were actually set.
func Load(startDir string) (Config, error) {
	cfg := Default()

	userPath, err := UserConfigPath()
	if err == nil && userPath != "" {
		if applied, err := mergeFile(&cfg, userPath); err != nil {
			return cfg, err
		} else if applied {
			cfg.Sources = append(cfg.Sources, userPath)
		}
	}

	if projectPath := FindProjectConfig(startDir); projectPath != "" {
		if applied, err := mergeFile(&cfg, projectPath); err != nil {
			return cfg, err
		} else if applied {
			cfg.Sources = append(cfg.Sources, projectPath)
		}
	}

	applyEnv(&cfg)

	if err := Validate(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// mergeFile decodes a TOML file over cfg. A missing file is not an error; an
// unreadable or malformed one is, and the path is always named.
func mergeFile(cfg *Config, path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading config %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return false, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return true, nil
}

// applyEnv layers environment variables over file configuration.
func applyEnv(cfg *Config) {
	if v := os.Getenv("LB_BD_BIN"); v != "" {
		cfg.General.BDBinary = v
	}
	if v := os.Getenv("LB_ACTOR"); v != "" {
		cfg.General.Actor = v
	}
	if v := os.Getenv("LB_COLOR"); v != "" {
		cfg.General.Color = v
	}
	if v := os.Getenv("LB_PROJECT"); v != "" {
		cfg.Workspace.Project = v
	}
	if v := os.Getenv("LB_BEADS_DIR"); v != "" {
		cfg.Workspace.BeadsDir = v
	}
	if v := os.Getenv("LB_RIG"); v != "" {
		cfg.Workspace.Rig = v
	}
	if v := os.Getenv("LB_TIMEOUT"); v != "" {
		if d, err := ParseDuration(v); err == nil {
			cfg.General.Timeout = Duration(d)
		}
	}
	// LB_NO_CONFIRM is deliberately environment-only: project configuration must
	// never be able to silently disable confirmation for a cloned repository.
	if truthy(os.Getenv("LB_NO_CONFIRM")) {
		no := false
		cfg.General.ConfirmMutations = &no
	}
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// NoConfirmFromEnv reports whether confirmation was disabled by environment, so
// the CLI can warn about it in interactive use.
func NoConfirmFromEnv() bool { return truthy(os.Getenv("LB_NO_CONFIRM")) }
