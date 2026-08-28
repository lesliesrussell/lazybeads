// lb-lou
package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// RelativeTime renders a duration the way an operator reads it: coarse and
// short. Sub-minute values collapse to "just now" rather than churning.
func RelativeTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		m := int(d.Minutes()) % 60
		if h := int(d.Hours()); m == 0 {
			return fmt.Sprintf("%dh", h)
		} else {
			return fmt.Sprintf("%dh %dm", h, m)
		}
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
}

// Ago renders a timestamp as an elapsed interval, or "unknown" when absent.
func Ago(t *time.Time, now time.Time) string {
	if t == nil {
		return "unknown"
	}
	rel := RelativeTime(now.Sub(*t))
	// lb-58x
	if rel == "just now" {
		return rel
	}
	return rel + " ago"
}

// StatusLabel pairs a symbol with the status word, so status is never conveyed
// by colour alone.
func (w *Writer) StatusLabel(status domain.IssueStatus) string {
	text := string(status)
	if text == "" {
		text = "unknown"
	}
	var sym Symbol
	var style Style
	switch {
	case strings.EqualFold(text, string(domain.StatusClosed)):
		sym, style = SymClosed, StyleDim
	case strings.EqualFold(text, string(domain.StatusInProgress)):
		sym, style = SymInProgress, StyleInfo
	case strings.EqualFold(text, string(domain.StatusBlocked)):
		sym, style = SymBlocked, StyleWarning
	case strings.EqualFold(text, string(domain.StatusOpen)):
		sym, style = SymReady, StyleOK
	default:
		sym, style = SymBullet, StyleNone
	}
	return w.Style(style, w.Symbol(sym)+" "+text)
}

// PriorityLabel renders P0..P4 with severity styling and no colour dependence.
func (w *Writer) PriorityLabel(p domain.Priority) string {
	label := p.Label()
	switch p {
	case 0:
		return w.Style(StyleP0, label)
	case 1:
		return w.Style(StyleP1, label)
	case 2:
		return w.Style(StyleP2, label)
	}
	return w.Style(StyleDim, label)
}

// Heading renders a section title in the dense uppercase style used by the
// list commands.
func (w *Writer) Heading(text string) string {
	return w.Style(StyleBold, strings.ToUpper(text))
}

// IssueLine renders one issue as a compact row: priority, id, title, impact and
// age. All issue-derived text is sanitized and width-bounded here.
//
// When the terminal is too narrow to hold everything, the trailing metadata is
// dropped before the title is squeezed, and the ID is never sacrificed: it is
// the only part of the row the operator needs in order to act on the issue.
func (w *Writer) IssueLine(issue domain.Issue, now time.Time, idWidth int) string {
	const minTitleWidth = 12

	impact := fmt.Sprintf("+%d downstream", issue.DependentCount)
	age := "unknown age"
	if issue.CreatedAt != nil {
		age = RelativeTime(issue.Age(now))
	}
	suffix := fmt.Sprintf("%s %s %s", impact, w.Symbol(SymBullet), age)

	prefix := w.PriorityLabel(issue.Priority) + "  " +
		w.Style(StyleID, Pad(SanitizeLine(issue.ID), idWidth)) + "  "
	// Styling adds invisible bytes, so measure the unstyled equivalent.
	prefixWidth := Width(issue.Priority.Label()) + 2 + idWidth + 2

	title := SanitizeLine(issue.Title)
	available := w.Width() - prefixWidth

	// Try to keep the metadata, but only while a usable title still fits.
	if titleWidth := available - Width(suffix) - 2; titleWidth >= minTitleWidth {
		return prefix + Pad(title, titleWidth) + "  " + w.Style(StyleDim, suffix)
	}
	if available < 1 {
		available = 1
	}
	return prefix + Truncate(title, available)
}

// IDWidth returns a column width that fits the widest ID in a set, bounded so a
// pathological ID cannot consume the line.
func IDWidth(issues []domain.Issue) int {
	width := 8
	for _, i := range issues {
		if n := Width(i.ID); n > width {
			width = n
		}
	}
	if width > 24 {
		width = 24
	}
	return width
}

// HealthSymbol maps a health level onto a glyph plus its word.
func (w *Writer) HealthSymbol(level domain.HealthLevel) string {
	switch level {
	case domain.HealthOK:
		return w.Style(StyleOK, w.Symbol(SymClosed))
	case domain.HealthWarning:
		return w.Style(StyleWarning, w.Symbol(SymWarning))
	case domain.HealthError:
		return w.Style(StyleError, w.Symbol(SymError))
	case domain.HealthInfo:
		return w.Style(StyleInfo, w.Symbol(SymBullet))
	default:
		return w.Style(StyleDim, "?")
	}
}

// NextSteps renders the contextual command suggestions that close most views.
func (w *Writer) NextSteps(commands ...string) {
	if len(commands) == 0 || w.Opts.Quiet {
		return
	}
	w.Blank()
	w.Print(w.Style(StyleDim, "Next:"))
	for _, c := range commands {
		w.Print("  " + w.Style(StyleAccent, c))
	}
}

// Field renders an aligned "label: value" detail line.
func (w *Writer) Field(label, value string) string {
	return w.Style(StyleDim, Pad(label+":", 12)) + " " + value
}

// Wrap breaks text to a width on word boundaries, preserving existing
// paragraph breaks. Input must already be sanitized.
func Wrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line.Len() == 0:
				line.WriteString(word)
			case Width(line.String())+1+Width(word) <= width:
				line.WriteString(" ")
				line.WriteString(word)
			default:
				out = append(out, line.String())
				line.Reset()
				line.WriteString(word)
			}
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return strings.Join(out, "\n")
}

// Indent prefixes every line of text with pad.
func Indent(text, pad string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
