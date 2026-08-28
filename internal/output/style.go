// lb-lou
package output

import "strings"

// Style names a semantic role rather than a colour, so a monochrome or
// NO_COLOR terminal degrades cleanly.
type Style int

const (
	StyleNone Style = iota
	StyleBold
	StyleDim
	StyleTitle
	StyleID
	StyleOK
	StyleWarning
	StyleError
	StyleInfo
	StyleP0
	StyleP1
	StyleP2
	StyleAccent
)

var ansiCodes = map[Style]string{
	StyleBold:    "1",
	StyleDim:     "2",
	StyleTitle:   "1",
	StyleID:      "36",
	StyleOK:      "32",
	StyleWarning: "33",
	StyleError:   "31",
	StyleInfo:    "34",
	StyleP0:      "1;31",
	StyleP1:      "31",
	StyleP2:      "33",
	StyleAccent:  "35",
}

// Style applies terminal styling when colour is enabled. It is only ever
// applied to text LazyBeads itself authored, or to already-sanitized content.
func (w *Writer) Style(s Style, text string) string {
	if !w.color || s == StyleNone {
		return text
	}
	code, ok := ansiCodes[s]
	if !ok {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// Symbol identifies a status glyph.
type Symbol int

const (
	SymReady Symbol = iota
	SymWarning
	SymError
	SymClosed
	SymBlocked
	SymInProgress
	SymBullet
	SymArrow
)

var unicodeSymbols = map[Symbol]string{
	SymReady:      "●",
	SymWarning:    "!",
	SymError:      "×",
	SymClosed:     "✓",
	SymBlocked:    "⊘",
	SymInProgress: "◐",
	SymBullet:     "·",
	SymArrow:      "→",
}

var asciiSymbols = map[Symbol]string{
	SymReady:      "*",
	SymWarning:    "!",
	SymError:      "x",
	SymClosed:     "+",
	SymBlocked:    "o",
	SymInProgress: ">",
	SymBullet:     "-",
	SymArrow:      "->",
}

// Symbol returns the glyph for a status, honouring --ascii.
//
// Colour alone never communicates status: every symbol is paired with a word by
// the callers that render state.
func (w *Writer) Symbol(s Symbol) string {
	if w.Opts.ASCII {
		if v, ok := asciiSymbols[s]; ok {
			return v
		}
		return "-"
	}
	if v, ok := unicodeSymbols[s]; ok {
		return v
	}
	return "-"
}

// TreeGlyphs are the connectors used to draw dependency trees.
type TreeGlyphs struct {
	Branch string // intermediate child
	Last   string // final child
	Pipe   string // vertical continuation
	Space  string // blank continuation
}

// Tree returns the connector set for the current mode.
func (w *Writer) Tree() TreeGlyphs {
	if w.Opts.ASCII {
		return TreeGlyphs{Branch: "|-- ", Last: "`-- ", Pipe: "|   ", Space: "    "}
	}
	return TreeGlyphs{Branch: "├── ", Last: "└── ", Pipe: "│   ", Space: "    "}
}

// Rule draws a horizontal separator at the current width.
func (w *Writer) Rule() string {
	ch := "─"
	if w.Opts.ASCII {
		ch = "-"
	}
	n := w.width
	if n > 80 {
		n = 80
	}
	return strings.Repeat(ch, n)
}
