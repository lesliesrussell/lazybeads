// lb-lou
package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func testWriter(opts Options) (*Writer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	if opts.Width == 0 {
		opts.Width = 80
	}
	if opts.Color == "" {
		opts.Color = "never"
	}
	return NewWriter(&out, &errOut, opts), &out, &errOut
}

func TestEnvelopeShape(t *testing.T) {
	Clock = func() time.Time { return time.Date(2026, 8, 27, 15, 11, 0, 0, time.UTC) }
	defer func() { Clock = time.Now }()

	w, out, _ := testWriter(Options{Format: FormatJSON})
	ws := domain.WorkspaceRef{RootPath: "/code/lazybeads"}
	if err := w.EmitJSON("next", ws, map[string]any{"issue": "lb-1"}, nil); err != nil {
		t.Fatal(err)
	}

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	for _, key := range []string{"schema_version", "command", "workspace", "data", "warnings", "generated_at"} {
		if _, ok := env[key]; !ok {
			t.Errorf("envelope is missing required key %q", key)
		}
	}
	if env["schema_version"] != float64(domain.SchemaVersion) {
		t.Errorf("schema_version = %v, want %d", env["schema_version"], domain.SchemaVersion)
	}
	if env["generated_at"] != "2026-08-27T15:11:00Z" {
		t.Errorf("generated_at = %v", env["generated_at"])
	}
	// Warnings must always be present as an array, never null, so consumers can
	// iterate without a nil check.
	if _, ok := env["warnings"].([]any); !ok {
		t.Errorf("warnings should be an array, got %T", env["warnings"])
	}
	// The workspace ref must carry an explicit null rig rather than omitting it.
	wsOut := env["workspace"].(map[string]any)
	if _, ok := wsOut["rig"]; !ok {
		t.Error("workspace.rig should be present (null when unset)")
	}
}

// TestJSONNeverContainsANSI guards the contract that machine output is clean
// even when colour is forced on.
func TestJSONNeverContainsANSI(t *testing.T) {
	w, out, _ := testWriter(Options{Format: FormatJSON, Color: "always"})
	err := w.EmitJSON("show", domain.WorkspaceRef{RootPath: "/p"}, map[string]any{
		"title": "issue with \x1b[31mescapes\x1b[0m",
	}, []string{"a warning"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte{0x1b}) {
		t.Errorf("raw ESC leaked into JSON: %q", out.String())
	}
	// Go's encoder escapes the byte as \u001b, which is safe for a parser but
	// must be handled by the caller before display.
	if !strings.Contains(out.String(), `\u001b`) {
		t.Error("expected the escape to be encoded as \\u001b rather than dropped")
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	w, out, _ := testWriter(Options{Format: FormatJSON})
	err := w.EmitJSONError("status", domain.ErrorPayload{
		Code:    "bd_not_found",
		Message: "Beads CLI (`bd`) was not found.",
		Hint:    "Install Beads or set LB_BD_BIN.",
	})
	if err != nil {
		t.Fatal(err)
	}
	var env domain.ErrorEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("error envelope is not valid JSON: %v", err)
	}
	if env.SchemaVersion != domain.SchemaVersion {
		t.Errorf("schema_version = %d", env.SchemaVersion)
	}
	if env.Error.Code != "bd_not_found" {
		t.Errorf("code = %q", env.Error.Code)
	}
	// upstream_stderr must be present as an explicit null when there is none.
	if !strings.Contains(out.String(), `"upstream_stderr": null`) {
		t.Errorf("upstream_stderr should be explicit null: %s", out.String())
	}
}

func TestParseFormat(t *testing.T) {
	for _, in := range []string{"", "human", "JSON", "jsonl"} {
		if _, err := ParseFormat(in); err != nil {
			t.Errorf("ParseFormat(%q): %v", in, err)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("yaml should be rejected with guidance until implemented")
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("unknown formats should be rejected")
	}
}

func TestNoColorIsHonoured(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if resolveColor("auto", &bytes.Buffer{}) {
		t.Error("NO_COLOR must disable colour in auto mode")
	}
	if !resolveColor("always", &bytes.Buffer{}) {
		t.Error("an explicit --color always still wins, as documented")
	}
	if resolveColor("never", &bytes.Buffer{}) {
		t.Error("never must disable colour")
	}
}

func TestNonTerminalDefaultsToNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if resolveColor("auto", &bytes.Buffer{}) {
		t.Error("a pipe is not a terminal; auto should not colourize")
	}
}

// TestStatusNeverReliesOnColorAlone checks the accessibility rule: with colour
// disabled, the status word and a distinct symbol still identify the state.
func TestStatusNeverReliesOnColorAlone(t *testing.T) {
	w, _, _ := testWriter(Options{Color: "never"})
	seen := map[string]bool{}
	for _, s := range []domain.IssueStatus{
		domain.StatusOpen, domain.StatusInProgress, domain.StatusBlocked, domain.StatusClosed,
	} {
		label := w.StatusLabel(s)
		if !strings.Contains(label, string(s)) {
			t.Errorf("label %q should contain the status word %q", label, s)
		}
		if strings.ContainsRune(label, 0x1b) {
			t.Errorf("colour should be disabled: %q", label)
		}
		symbol := strings.Fields(label)[0]
		if seen[symbol] {
			t.Errorf("symbol %q is reused; symbols must distinguish states", symbol)
		}
		seen[symbol] = true
	}
}

func TestASCIIModeAvoidsNonASCII(t *testing.T) {
	w, _, _ := testWriter(Options{ASCII: true, Color: "never"})
	glyphs := []string{
		w.Symbol(SymReady), w.Symbol(SymWarning), w.Symbol(SymError),
		w.Symbol(SymClosed), w.Symbol(SymBlocked), w.Symbol(SymInProgress),
		w.Symbol(SymBullet), w.Symbol(SymArrow),
		w.Tree().Branch, w.Tree().Last, w.Tree().Pipe, w.Rule(),
	}
	for _, g := range glyphs {
		for _, r := range g {
			if r > 127 {
				t.Errorf("ASCII mode emitted %q (rune %U)", g, r)
			}
		}
	}
}

func TestRelativeTime(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{26 * time.Minute, "26m"},
		{2*time.Hour + 14*time.Minute, "2h 14m"},
		{3 * time.Hour, "3h"},
		{3 * 24 * time.Hour, "3d"},
		{90 * 24 * time.Hour, "3mo"},
		{-5 * time.Minute, "just now"},
	}
	for _, c := range cases {
		if got := RelativeTime(c.in); got != c.want {
			t.Errorf("RelativeTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := Ago(nil, time.Now()); got != "unknown" {
		t.Errorf("a missing timestamp should read %q, got %q", "unknown", got)
	}
}

// TestIssueLineFitsNarrowTerminal covers the 80-column floor and the degraded
// narrow case.
func TestIssueLineFitsNarrowTerminal(t *testing.T) {
	issue := domain.Issue{
		ID:             "lb-a3f8.2",
		Title:          strings.Repeat("a very long title ", 20),
		Priority:       1,
		DependentCount: 3,
	}
	created := time.Now().Add(-2 * time.Hour)
	issue.CreatedAt = &created

	for _, width := range []int{200, 100, 80, 60, 40} {
		w, _, _ := testWriter(Options{Width: width, Color: "never"})
		line := w.IssueLine(issue, time.Now(), 10)
		if got := Width(line); got > width {
			t.Errorf("at width %d the line is %d cells wide: %q", width, got, line)
		}
		if !strings.Contains(line, "lb-a3f8.2") {
			t.Errorf("the ID must never be truncated away: %q", line)
		}
	}
}

func TestTableAlignsAndBoundsWidth(t *testing.T) {
	w, out, _ := testWriter(Options{Width: 60, Color: "never"})
	table := w.NewTable(
		Column{Header: "PRI", Width: 3},
		Column{Header: "ID", Width: 10},
		Column{Header: "TITLE"},
		Column{Header: "AGE", Width: 6, Right: true},
	)
	table.AddRow("P1", "lb-a3f8.2", "日本語のタイトルがとても長い場合", "2h")
	table.AddRow("P2", "lb-f5c9", "short", "1d")
	table.Render(true)

	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if got := Width(line); got > 60 {
			t.Errorf("row exceeds terminal width (%d): %q", got, line)
		}
	}
	if !strings.Contains(out.String(), "PRI") {
		t.Error("header should be rendered when requested")
	}
}

func TestEmptyTableRendersNothing(t *testing.T) {
	w, out, _ := testWriter(Options{})
	w.NewTable(Column{Header: "ID", Width: 5}).Render(true)
	if out.Len() != 0 {
		t.Errorf("an empty table should emit nothing, got %q", out.String())
	}
}

func TestWrapRespectsWidthAndParagraphs(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog.\n\nSecond paragraph here."
	got := Wrap(text, 20)
	for _, line := range strings.Split(got, "\n") {
		if Width(line) > 20 {
			t.Errorf("line exceeds width: %q", line)
		}
	}
	if !strings.Contains(got, "\n\n") {
		t.Error("paragraph breaks should be preserved")
	}
}

func TestWarningsGoToStderr(t *testing.T) {
	w, out, errOut := testWriter(Options{Color: "never"})
	w.Warn("stale claim detected")
	if out.Len() != 0 {
		t.Errorf("warnings must not pollute stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "stale claim detected") {
		t.Errorf("warning missing from stderr: %q", errOut.String())
	}
}

func TestQuietSuppressesCommentary(t *testing.T) {
	w, out, _ := testWriter(Options{Quiet: true})
	w.Note("run lb next")
	w.NextSteps("lb claim lb-1")
	if out.Len() != 0 {
		t.Errorf("--quiet should suppress commentary, got %q", out.String())
	}
}
