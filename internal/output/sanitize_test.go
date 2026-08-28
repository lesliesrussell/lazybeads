// lb-lou
package output

import (
	"strings"
	"testing"
)

// TestSanitizeNeutralizesEscapeSequences is the core security test: no output
// derived from issue data may retain a sequence a terminal would interpret.
func TestSanitizeNeutralizesEscapeSequences(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"CSI colour", "safe\x1b[31mred\x1b[0m"},
		{"cursor movement", "line\x1b[2Aoverwrite"},
		{"screen clear", "\x1b[2J\x1b[H"},
		{"OSC window title", "\x1b]0;pwned\x07"},
		{"OSC 52 clipboard", "\x1b]52;c;cGF5bG9hZA==\x07"},
		{"OSC 8 hyperlink", "\x1b]8;;http://evil.example\x07click\x1b]8;;\x07"},
		{"carriage return overwrite", "real output\rfake output"},
		{"8-bit CSI", "text[31m"},
		{"bidi override", "annotated ‮gnp.txt"},
		{"BEL", "ding\x07"},
		{"NUL", "a\x00b"},
		{"line separator", "a b c"},
	}
	forbidden := []rune{0x1b, 0x07, 0x00, '\r', 0x9b, '‮', ' ', ' '}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize(c.in)
			for _, bad := range forbidden {
				if strings.ContainsRune(got, bad) {
					t.Errorf("rune %U survived sanitization: %q", bad, got)
				}
			}
		})
	}
}

func TestSanitizePreservesOrdinaryText(t *testing.T) {
	cases := []string{
		"Add typed bd adapter",
		"café ☕ 日本語",
		"emoji family \U0001F469‍\U0001F469‍\U0001F466 stays joined",
		"path/to/file.go:42",
		"quotes \"and\" 'apostrophes'",
	}
	for _, in := range cases {
		if got := Sanitize(in); got != in {
			t.Errorf("Sanitize(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestSanitizeKeepsPermittedWhitespace(t *testing.T) {
	got := Sanitize("line one\nline two\tcolumn")
	if got != "line one\nline two\tcolumn" {
		t.Errorf("newlines and tabs should survive: %q", got)
	}
}

func TestSanitizeLineCollapsesToOneLine(t *testing.T) {
	got := SanitizeLine("first\nsecond\t\tthird   spaced")
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("list output must stay on one line: %q", got)
	}
	if got != "first second third spaced" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateUsesDisplayWidth(t *testing.T) {
	// Each CJK character occupies two terminal cells.
	got := Truncate("日本語テキスト", 6)
	if w := Width(got); w > 6 {
		t.Errorf("Truncate produced width %d, want at most 6 (%q)", w, got)
	}
	if got := Truncate("short", 40); got != "short" {
		t.Errorf("short text should be untouched: %q", got)
	}
	if got := Truncate("anything", 0); got != "" {
		t.Errorf("zero width should yield empty, got %q", got)
	}
}

func TestPadAlignsByDisplayWidth(t *testing.T) {
	for _, in := range []string{"ascii", "日本", "emoji \U0001F642", ""} {
		if got := Width(Pad(in, 12)); got != 12 {
			t.Errorf("Pad(%q,12) has width %d, want 12", in, got)
		}
	}
}

func TestMaxLinesReportsWithheldContent(t *testing.T) {
	got := MaxLines("a\nb\nc\nd\ne", 2)
	if !strings.Contains(got, "3 more lines") {
		t.Errorf("clipping should be disclosed: %q", got)
	}
	if got := MaxLines("a\nb", 5); got != "a\nb" {
		t.Errorf("short text should be untouched: %q", got)
	}
}

// FuzzSanitize asserts the invariant that no control sequence can survive,
// whatever the input.
func FuzzSanitize(f *testing.F) {
	seeds := []string{
		"", "plain", "\x1b[31m", "\x1b]52;c;AAAA\x07", "\r\n", "\x00",
		"日本語", "‮reversed", "0m", strings.Repeat("\x1b", 100),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Sanitize(in)
		for _, r := range got {
			if r == 0x1b || r == 0x07 || r == 0x00 || r == '\r' {
				t.Fatalf("Sanitize(%q) leaked control rune %U", in, r)
			}
			if r < 0x20 && r != '\n' && r != '\t' {
				t.Fatalf("Sanitize(%q) leaked C0 control %U", in, r)
			}
			if r >= 0x80 && r <= 0x9f {
				t.Fatalf("Sanitize(%q) leaked C1 control %U", in, r)
			}
		}
		if line := SanitizeLine(in); strings.ContainsAny(line, "\n\t") {
			t.Fatalf("SanitizeLine(%q) = %q contains a break", in, line)
		}
	})
}

// FuzzTruncate asserts the width bound always holds.
func FuzzTruncate(f *testing.F) {
	f.Add("hello", 3)
	f.Add("日本語", 4)
	f.Add("", 0)
	f.Fuzz(func(t *testing.T, s string, width int) {
		if width < 0 || width > 4096 {
			t.Skip()
		}
		got := Truncate(s, width)
		if w := Width(got); w > width {
			t.Fatalf("Truncate(%q,%d) width %d exceeds the bound", s, width, w)
		}
	})
}
