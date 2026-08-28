// lb-1td
package config

import (
	"fmt"
	"sort"
	"strings"
)

// ValidStrategies lists the recommendation strategies the engine implements.
var ValidStrategies = []string{"priority", "leverage", "age", "balanced", "random"}

// Validate normalizes and range-checks a configuration, returning an error that
// names the offending key.
func Validate(cfg *Config) error {
	switch strings.ToLower(cfg.General.Color) {
	case "", "auto", "always", "never":
		cfg.General.Color = strings.ToLower(cfg.General.Color)
		if cfg.General.Color == "" {
			cfg.General.Color = "auto"
		}
	default:
		return fmt.Errorf("general.color must be auto, always or never (got %q)", cfg.General.Color)
	}

	if cfg.General.BDBinary == "" {
		cfg.General.BDBinary = "bd"
	}
	if cfg.General.StaleAfter <= 0 {
		return fmt.Errorf("general.stale_after must be positive")
	}
	if cfg.General.Timeout <= 0 {
		return fmt.Errorf("general.timeout must be positive")
	}
	if cfg.General.RefreshInterval <= 0 {
		return fmt.Errorf("general.refresh_interval must be positive")
	}

	cfg.Ranking.Strategy = strings.ToLower(strings.TrimSpace(cfg.Ranking.Strategy))
	if cfg.Ranking.Strategy == "" {
		cfg.Ranking.Strategy = "balanced"
	}
	if !containsString(ValidStrategies, cfg.Ranking.Strategy) {
		return fmt.Errorf("ranking.strategy must be one of %s (got %q)",
			strings.Join(ValidStrategies, ", "), cfg.Ranking.Strategy)
	}
	if cfg.Ranking.MaxGraphDepth <= 0 {
		cfg.Ranking.MaxGraphDepth = 8
	}
	if cfg.Ranking.MaxGraphNodes <= 0 {
		cfg.Ranking.MaxGraphNodes = 500
	}

	switch strings.ToLower(cfg.Keys.Layout) {
	case "", "logical", "physical":
		cfg.Keys.Layout = strings.ToLower(cfg.Keys.Layout)
		if cfg.Keys.Layout == "" {
			cfg.Keys.Layout = "logical"
		}
	default:
		return fmt.Errorf("keys.layout must be logical or physical (got %q)", cfg.Keys.Layout)
	}

	return nil
}

// KeyCollision reports one key bound to more than one action in a scope.
type KeyCollision struct {
	Scope   string
	Key     string
	Actions []string
}

func (c KeyCollision) String() string {
	return fmt.Sprintf("%s: %q is bound to %s", c.Scope, c.Key, strings.Join(c.Actions, " and "))
}

// DetectKeyCollisions finds keys bound to multiple actions within a scope.
// Collisions across scopes are legitimate, because scopes are modal.
func DetectKeyCollisions(k Keys) []KeyCollision {
	var out []KeyCollision
	scopes := []struct {
		name     string
		bindings map[string][]string
	}{
		{"keys.global", k.Global},
		{"keys.list", k.List},
		{"keys.detail", k.Detail},
	}
	for _, scope := range scopes {
		byKey := map[string][]string{}
		for action, keys := range scope.bindings {
			for _, key := range keys {
				byKey[key] = append(byKey[key], action)
			}
		}
		keys := make([]string, 0, len(byKey))
		for key := range byKey {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if actions := byKey[key]; len(actions) > 1 {
				sort.Strings(actions)
				out = append(out, KeyCollision{Scope: scope.name, Key: key, Actions: actions})
			}
		}
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
