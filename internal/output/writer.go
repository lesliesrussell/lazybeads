// lb-lou
package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Format selects the rendering mode for a command's result.
type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

// ParseFormat validates a --format value.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "human", "text":
		return FormatHuman, nil
	case "json":
		return FormatJSON, nil
	case "jsonl", "ndjson":
		return FormatJSONL, nil
	case "yaml":
		return "", fmt.Errorf("yaml output is not implemented yet; use --format json")
	}
	return "", fmt.Errorf("unknown format %q (want human, json or jsonl)", s)
}

// Options controls every aspect of presentation.
type Options struct {
	Format  Format
	Color   string // auto | always | never
	ASCII   bool
	Quiet   bool
	Verbose bool
	Debug   bool
	Width   int
}

// Writer renders command output to a pair of streams.
type Writer struct {
	Out  io.Writer
	Err  io.Writer
	Opts Options

	color bool
	width int
}

// NewWriter builds a writer, resolving colour and width from the environment
// exactly once so rendering stays consistent within a command.
func NewWriter(out, errOut io.Writer, opts Options) *Writer {
	w := &Writer{Out: out, Err: errOut, Opts: opts}
	w.color = resolveColor(opts.Color, out)
	w.width = opts.Width
	if w.width <= 0 {
		w.width = terminalWidth(out)
	}
	return w
}

// resolveColor honours NO_COLOR and only enables styling on a real terminal
// when the mode is "auto". JSON output never carries styling.
func resolveColor(mode string, out io.Writer) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always":
		return true
	case "never":
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return isTerminal(out)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func terminalWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			return width
		}
	}
	if v := os.Getenv("COLUMNS"); v != "" {
		n := 0
		for _, r := range v {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			return n
		}
	}
	return 80
}

// Width returns the effective render width.
func (w *Writer) Width() int { return w.width }

// ColorEnabled reports whether styling will be emitted.
func (w *Writer) ColorEnabled() bool { return w.color }

// ASCIIOnly reports whether output must avoid box-drawing and symbols.
func (w *Writer) ASCIIOnly() bool { return w.Opts.ASCII }

// Print writes a preformatted line to stdout.
func (w *Writer) Print(s string) {
	fmt.Fprintln(w.Out, s)
}

// Printf writes a formatted line to stdout. Callers must sanitize any
// issue-derived values before they reach here.
func (w *Writer) Printf(format string, args ...any) {
	fmt.Fprintf(w.Out, format, args...)
}

// Blank writes an empty line unless output is quiet.
func (w *Writer) Blank() {
	if !w.Opts.Quiet {
		fmt.Fprintln(w.Out)
	}
}

// Note writes secondary commentary, suppressed by --quiet.
func (w *Writer) Note(s string) {
	if w.Opts.Quiet {
		return
	}
	fmt.Fprintln(w.Out, s)
}

// Warn writes a warning to stderr so it never pollutes piped stdout.
func (w *Writer) Warn(s string) {
	fmt.Fprintln(w.Err, w.Style(StyleWarning, w.Symbol(SymWarning)+" "+s))
}
