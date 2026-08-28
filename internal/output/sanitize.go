// lb-lou
package output

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Sanitize makes attacker-influenced text safe to write to a terminal.
//
// Issue titles, descriptions, labels, event text and memory content are all
// authored by collaborators or agents. Without this, a crafted title could move
// the cursor, rewrite earlier output, set the window title, or drive OSC 52 to
// overwrite the operator's clipboard.
//
// Control characters are replaced rather than dropped so that text length stays
// visually honest and a tampered string is visible as such.
func Sanitize(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			// Permitted whitespace; callers decide whether to collapse it.
			b.WriteRune(r)
		case r == '\r':
			// A bare carriage return lets text overwrite the current line.
			b.WriteRune('\n')
		case r == 0x7f:
			b.WriteRune('�')
		case r < 0x20:
			// Includes ESC (0x1b), which begins every escape sequence, and BEL
			// (0x07), which terminates an OSC string.
			b.WriteRune('�')
		case r >= 0x80 && r <= 0x9f:
			// C1 controls: 0x9b is a single-byte CSI introducer in 8-bit mode.
			b.WriteRune('�')
		case r == '\u2028' || r == '\u2029':
			// Unicode line separators break single-line layout.
			b.WriteRune(' ')
		case r == utf8.RuneError:
			b.WriteRune('�')
		case unicode.Is(unicode.Cf, r) && r != '\u200d':
			// Format characters include the bidirectional overrides used to
			// visually reorder text. Zero-width joiner is kept so emoji
			// sequences survive intact.
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SanitizeLine sanitizes text and collapses it onto a single line, for use in
// dense list output where a newline would corrupt the layout.
func SanitizeLine(s string) string {
	s = Sanitize(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(collapseSpaces(s))
}

func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Truncate shortens text to a display width, measured in terminal cells rather
// than bytes or runes, so wide characters do not overflow a column.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return runewidth.Truncate(s, width, "…")
}

// Pad right-pads text to an exact display width, truncating when it is too long
// so columns stay aligned regardless of the characters involved.
func Pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = Truncate(s, width)
	if gap := width - runewidth.StringWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// Width returns the terminal cell width of a string.
func Width(s string) int { return runewidth.StringWidth(s) }

// MaxLines clips multi-line text to n lines, appending a marker when content
// was withheld so the operator knows to open the full view.
func MaxLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n… (" + itoa(len(lines)-n) + " more lines)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
