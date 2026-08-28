// lb-17y
package config

import "testing"

func FuzzParseDuration(f *testing.F) {
	f.Add("30s")
	f.Add("8h")
	f.Add("2d")
	f.Add("nope")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseDuration(s)
	})
}

func FuzzValidateConfig(f *testing.F) {
	f.Add("auto", "balanced")
	f.Add("never", "priority")
	f.Add("purple", "magic")
	f.Fuzz(func(t *testing.T, color, strategy string) {
		cfg := Default()
		cfg.General.Color = color
		cfg.Ranking.Strategy = strategy
		_ = Validate(&cfg)
	})
}
