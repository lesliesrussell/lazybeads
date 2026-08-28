// lb-lh5
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/version"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

// DoctorRequest controls `lb doctor`.
type DoctorRequest struct {
	Fix     bool
	Verbose bool
}

// DoctorReport is the health diagnosis. --fix may append local-only repairs.
type DoctorReport struct {
	Health    domain.Health `json:"health"`
	Version   string        `json:"lazybeads_version"`
	BDVersion string        `json:"bd_version,omitempty"`
	Fixes     []string      `json:"fixes,omitempty"`
	SetupErr  string        `json:"setup_error,omitempty"`
}

// Doctor runs the documented checks. It never migrates Beads or mutates issues.
func (s *Service) Doctor(ctx context.Context, req DoctorRequest) (*DoctorReport, error) {
	report := &DoctorReport{
		Version:   version.Version,
		BDVersion: s.Workspace.BDVersion,
	}
	h := &report.Health

	if s.Client != nil {
		if v, err := s.Client.Version(ctx, s.Scope()); err != nil {
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "bd_version", Level: domain.HealthError,
				Summary: "bd version is not readable",
				Detail:  err.Error(),
				Hint:    "Install Beads or set LB_BD_BIN.",
			})
		} else {
			report.BDVersion = v.Version
			level := domain.HealthOK
			hint := ""
			if !supportedBD(v.Version) {
				level = domain.HealthWarning
				hint = "LazyBeads is tested against Beads 1.0.x; continue with capability gating."
			}
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "bd_version", Level: level,
				Summary: "Beads " + v.Version,
				Hint:    hint,
			})
		}

		if _, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), Limit: 1}); err != nil {
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "json_parse", Level: domain.HealthError,
				Summary: "Beads JSON output could not be parsed",
				Detail:  err.Error(),
			})
		} else {
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "json_parse", Level: domain.HealthOK,
				Summary: "Beads JSON output decodes",
			})
		}
	} else {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "bd", Level: domain.HealthError,
			Summary: "Beads CLI (`bd`) was not found.",
			Hint:    "Install Beads or set LB_BD_BIN.",
		})
	}

	if s.Workspace.RootPath == "" && s.Workspace.BeadsDir == "" {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "workspace", Level: domain.HealthError,
			Summary: "No Beads workspace was found from this directory.",
			Hint:    "Run `bd init` here, or pass --project/--beads-dir.",
		})
	} else {
		storage := string(s.Workspace.StorageMode)
		if storage == "" {
			storage = "unknown"
		}
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "workspace", Level: domain.HealthOK,
			Summary: "Workspace " + s.Workspace.RootPath,
			Detail:  "storage " + storage,
		})
	}

	if s.Actor == "" {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "actor", Level: domain.HealthWarning,
			Summary: "Current actor is not configured",
			Hint:    "Set LB_ACTOR, --actor, or general.actor in config.",
		})
	} else {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "actor", Level: domain.HealthOK,
			Summary: "Actor " + s.Actor,
		})
	}

	if len(s.Config.Sources) > 0 {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "config", Level: domain.HealthOK,
			Summary: "Configuration loaded",
			Items:   append([]string(nil), s.Config.Sources...),
		})
	} else {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "config", Level: domain.HealthInfo,
			Summary: "Using built-in defaults (no config file)",
		})
	}

	if collisions := config.DetectKeyCollisions(s.Config.Keys); len(collisions) > 0 {
		items := make([]string, 0, len(collisions))
		for _, c := range collisions {
			items = append(items, c.String())
		}
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "keybindings", Level: domain.HealthWarning,
			Summary: "Keybinding collisions",
			Items:   items,
		})
	}

	if workspace.EditorCommand() == nil {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "editor", Level: domain.HealthInfo,
			Summary: "No $VISUAL/$EDITOR set; `lb edit --open-in-editor` is unavailable",
		})
	}

	if s.Client != nil {
		if stale, err := s.staleClaims(ctx, Now()); err == nil && len(stale) > 0 {
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "stale_claims", Level: domain.HealthWarning,
				Summary: fmt.Sprintf("%d stale claim%s", len(stale), plural(len(stale))),
				Items:   issueIDs(stale),
			})
		}
		if cycles, err := s.Client.Cycles(ctx, s.Scope()); err == nil && len(cycles) > 0 {
			items := make([]string, 0, len(cycles))
			for _, c := range cycles {
				items = append(items, strings.Join(c, " -> "))
			}
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "cycles", Level: domain.HealthWarning,
				Summary: fmt.Sprintf("%d dependency cycle(s)", len(cycles)),
				Items:   items,
			})
		}
		if sync, err := s.SyncInspect(ctx); err == nil {
			summary := "Sync: unknown (LazyBeads could not determine remote status)"
			if sync.Available {
				summary = "Sync status available"
				if sync.Detail != "" {
					summary = "Sync: " + truncate(sync.Detail, 80)
				}
			}
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "sync", Level: domain.HealthInfo, Summary: summary, Detail: sync.Detail,
			})
		}
	}

	h.Reduce()

	if req.Fix {
		fixes, err := applyDoctorFixes()
		if err != nil {
			return report, err
		}
		report.Fixes = fixes
	}
	return report, nil
}

func supportedBD(v string) bool {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
	return strings.HasPrefix(v, "1.0.") || v == "1.0" || v == "1"
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// applyDoctorFixes writes a user config file if missing. It never touches Beads.
func applyDoctorFixes() ([]string, error) {
	path, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		return []string{"config already exists at " + path}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return []string{"config already exists at " + path}, nil
		}
		return nil, err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(config.Default()); err != nil {
		return nil, err
	}
	return []string{"wrote " + path}, nil
}
