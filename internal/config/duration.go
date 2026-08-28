// lb-1td
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a TOML-friendly time.Duration that also accepts day suffixes,
// which Go's own parser rejects.
type Duration time.Duration

// UnmarshalText decodes "8h", "30s", "2d" and bare second counts.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalText renders the canonical Go duration string.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// Duration converts back to the standard library type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String renders the duration for display.
func (d Duration) String() string { return time.Duration(d).String() }

// ParseDuration accepts Go duration syntax plus a "d" (day) and "w" (week)
// suffix, and treats a bare number as seconds.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Second)), nil
	}
	unit := s[len(s)-1]
	if unit == 'd' || unit == 'w' {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		mult := 24 * float64(time.Hour)
		if unit == 'w' {
			mult *= 7
		}
		return time.Duration(n * mult), nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return v, nil
}
