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
			if ce, ok := beads.AsCommandError(err); ok && ce.Kind == beads.ErrSchemaMismatch {
				h.Checks = append(h.Checks, domain.HealthCheck{
					Name: "schema", Level: domain.HealthError,
					Summary: "Beads schema is incompatible with this LazyBeads",
					Detail:  err.Error(),
					Hint:    "Upgrade bd and follow Beads' own migration; LazyBeads will not run bd migrate.",
				})
			} else {
				h.Checks = append(h.Checks, domain.HealthCheck{
					Name: "json_parse", Level: domain.HealthError,
					Summary: "Beads JSON output could not be parsed",
					Detail:  err.Error(),
				})
			}
		} else {
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "json_parse", Level: domain.HealthOK,
				Summary: "Beads JSON output decodes",
			})
			h.Checks = append(h.Checks, domain.HealthCheck{
				Name: "schema", Level: domain.HealthOK,
				Summary: "Beads schema is compatible (tested " + version.TestedBeads + ")",
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
		s.checkParentStatus(ctx, h)
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

// lb-uvj
func (s *Service) checkParentStatus(ctx context.Context, h *domain.Health) {
	issues, err := s.Client.List(ctx, beads.ListQuery{Scope: s.Scope(), All: true})
	if err != nil {
		return
	}
	byID := make(map[string]domain.Issue, len(issues))
	for _, i := range issues {
		byID[i.ID] = i
	}
	var items []string
	for _, i := range issues {
		if i.ParentID != nil {
			if parent, ok := byID[*i.ParentID]; ok && parent.IsClosed() && !i.IsClosed() {
				items = append(items, i.ID+" is "+string(i.Status)+" under closed parent "+parent.ID)
			}
		}
		if i.IsInProgress() && (i.Assignee == nil || i.Assignee.String() == "") {
			items = append(items, i.ID+" is in_progress without an assignee")
		}
	}
	if len(items) == 0 {
		h.Checks = append(h.Checks, domain.HealthCheck{
			Name: "parent_status", Level: domain.HealthOK,
			Summary: "No parent/status inconsistencies",
		})
		return
	}
	h.Checks = append(h.Checks, domain.HealthCheck{
		Name: "parent_status", Level: domain.HealthWarning,
		Summary: fmt.Sprintf("%d parent/status issue(s)", len(items)),
		Items:   items,
		Hint:    "LazyBeads will not mutate these; inspect with lb show and fix through bd.",
	})
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

// applyDoctorFixes writes a user config file if missing and creates the cache
// directory. It never touches Beads.
func applyDoctorFixes() ([]string, error) {
	var fixes []string
	path, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		fixes = append(fixes, "config already exists at "+path)
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if !os.IsExist(err) {
				return nil, err
			}
			fixes = append(fixes, "config already exists at "+path)
		} else {
			defer f.Close()
			if err := toml.NewEncoder(f).Encode(config.Default()); err != nil {
				return nil, err
			}
			fixes = append(fixes, "wrote "+path)
		}
	}
	cache, err := config.UserCacheDir()
	if err != nil {
		return fixes, err
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return fixes, err
	}
	fixes = append(fixes, "cache directory "+cache)
	return fixes, nil
}
