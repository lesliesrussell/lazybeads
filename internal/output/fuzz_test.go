// lb-17y
package output

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func FuzzIssueLine(f *testing.F) {
	f.Add("lb-1", "a title", 80)
	f.Add("id", "\x1b[31m", 40)
	f.Fuzz(func(t *testing.T, id, title string, width int) {
		if width < 0 {
			width = -width
		}
		width %= 400
		w, _, _ := testWriter(Options{Width: width, Color: "never", ASCII: true})
		issue := domain.Issue{ID: id, Title: title, Priority: 1}
		now := time.Date(2026, 8, 28, 15, 11, 0, 0, time.UTC)
		line := w.IssueLine(issue, now, 8)
		if strings.ContainsRune(line, 0x1b) {
			t.Fatalf("ANSI leaked into a no-color line: %q", line)
		}
		_ = utf8.ValidString(line)
	})
}
