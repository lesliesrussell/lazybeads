// lb-3t3
package tui

import (
	"bytes"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/lesliesrussell/lazybeads/internal/output"
)

// theme is a lazygit-like palette: green focused border, dim inactive
// panels, inverted selected row, cyan keycaps. Colour is optional; borders
// remain so the chrome still reads on a dumb terminal.
type theme struct {
	ascii bool
	color bool

	focused   lipgloss.Style
	inactive  lipgloss.Style
	selected  lipgloss.Style
	normal    lipgloss.Style
	title     lipgloss.Style
	dim       lipgloss.Style
	key       lipgloss.Style
	ok        lipgloss.Style
	warn      lipgloss.Style
	err       lipgloss.Style
	p0        lipgloss.Style
	p1        lipgloss.Style
	p2        lipgloss.Style
	header    lipgloss.Style
	tabOn     lipgloss.Style
	tabOff    lipgloss.Style
	statusBar lipgloss.Style
	modal     lipgloss.Style
	borderOn  lipgloss.Style
	borderOff lipgloss.Style
}

func newTheme(opts Options) theme {
	th := theme{ascii: opts.ASCII, color: opts.Color}
	border := lipgloss.RoundedBorder()
	if opts.ASCII {
		border = lipgloss.ASCIIBorder()
	}
	ns := lipgloss.NewStyle
	if !opts.Color {
		r := lipgloss.NewRenderer(&bytes.Buffer{})
		r.SetColorProfile(termenv.Ascii)
		ns = r.NewStyle
	}

	if opts.Color {
		th.focused = ns().Border(border).BorderForeground(lipgloss.Color("10"))
		th.inactive = ns().Border(border).BorderForeground(lipgloss.Color("240"))
		th.selected = ns().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4")).Bold(true)
		th.normal = ns()
		th.title = ns().Bold(true).Foreground(lipgloss.Color("15"))
		th.dim = ns().Foreground(lipgloss.Color("245"))
		th.key = ns().Foreground(lipgloss.Color("14")).Bold(true)
		th.ok = ns().Foreground(lipgloss.Color("10"))
		th.warn = ns().Foreground(lipgloss.Color("11"))
		th.err = ns().Foreground(lipgloss.Color("9"))
		th.p0 = ns().Foreground(lipgloss.Color("9")).Bold(true)
		th.p1 = ns().Foreground(lipgloss.Color("9"))
		th.p2 = ns().Foreground(lipgloss.Color("11"))
		th.header = ns().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("236"))
		th.tabOn = ns().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Padding(0, 1)
		th.tabOff = ns().Foreground(lipgloss.Color("250")).Padding(0, 1)
		th.statusBar = ns().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236"))
		th.modal = ns().Border(border).BorderForeground(lipgloss.Color("11")).Padding(1, 2)
		th.borderOn = ns().Foreground(lipgloss.Color("10")).Bold(true)
		th.borderOff = ns().Foreground(lipgloss.Color("240"))
	} else {
		th.focused = ns().Border(border).Bold(true)
		th.inactive = ns().Border(border)
		th.selected = ns().Reverse(true).Bold(true)
		th.normal = ns()
		th.title = ns().Bold(true)
		th.dim = ns()
		th.key = ns().Bold(true)
		th.ok = ns()
		th.warn = ns()
		th.err = ns()
		th.p0 = ns().Bold(true)
		th.p1 = ns()
		th.p2 = ns()
		th.header = ns().Reverse(true)
		th.tabOn = ns().Reverse(true).Bold(true).Padding(0, 1)
		th.tabOff = ns().Padding(0, 1)
		th.statusBar = ns().Reverse(true)
		th.modal = ns().Border(border).Padding(1, 2).Bold(true)
		th.borderOn = ns().Bold(true)
		th.borderOff = ns()
	}
	return th
}

func (th theme) glyph(sym output.Symbol) string {
	if th.ascii {
		return map[output.Symbol]string{
			output.SymReady: "*", output.SymWarning: "!", output.SymError: "x",
			output.SymClosed: "+", output.SymBlocked: "o", output.SymInProgress: ">",
			output.SymBullet: "-", output.SymArrow: "->",
		}[sym]
	}
	return map[output.Symbol]string{
		output.SymReady: "●", output.SymWarning: "!", output.SymError: "×",
		output.SymClosed: "✓", output.SymBlocked: "⊘", output.SymInProgress: "◐",
		output.SymBullet: "·", output.SymArrow: "→",
	}[sym]
}

func (th theme) prio(p int) lipgloss.Style {
	switch p {
	case 0:
		return th.p0
	case 1:
		return th.p1
	case 2:
		return th.p2
	}
	return th.dim
}
