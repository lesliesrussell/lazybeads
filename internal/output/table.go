// lb-lou
package output

import "strings"

// Column describes one field in a dense table.
type Column struct {
	Header string
	// Width is the fixed display width. Zero means "take the remaining space",
	// and at most one column may be flexible.
	Width int
	Style Style
	// Right aligns the cell to the right edge of its column.
	Right bool
}

// Table renders aligned rows bounded by the terminal width. Every cell is
// truncated by display width, so wide characters never break alignment.
type Table struct {
	w       *Writer
	columns []Column
	rows    [][]string
	styles  [][]Style
}

// NewTable starts a table with the given columns.
func (w *Writer) NewTable(columns ...Column) *Table {
	return &Table{w: w, columns: columns}
}

// AddRow appends a row. Cells must already be sanitized.
func (t *Table) AddRow(cells ...string) {
	t.rows = append(t.rows, cells)
	t.styles = append(t.styles, nil)
}

// AddStyledRow appends a row with per-cell style overrides.
func (t *Table) AddStyledRow(cells []string, styles []Style) {
	t.rows = append(t.rows, cells)
	t.styles = append(t.styles, styles)
}

// Rows reports how many rows are pending.
func (t *Table) Rows() int { return len(t.rows) }

// resolveWidths distributes the terminal width across columns, giving the
// remainder to the single flexible column.
func (t *Table) resolveWidths() []int {
	widths := make([]int, len(t.columns))
	fixed, flexIdx := 0, -1
	for i, c := range t.columns {
		widths[i] = c.Width
		if c.Width == 0 {
			flexIdx = i
			continue
		}
		fixed += c.Width
	}
	gaps := 2 * (len(t.columns) - 1)
	if flexIdx >= 0 {
		remaining := t.w.Width() - fixed - gaps
		if remaining < 8 {
			remaining = 8
		}
		widths[flexIdx] = remaining
	}
	return widths
}

// Render writes the table, omitting the header when it carries no information.
func (t *Table) Render(showHeader bool) {
	if len(t.rows) == 0 {
		return
	}
	widths := t.resolveWidths()

	if showHeader {
		var b strings.Builder
		for i, c := range t.columns {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(Pad(c.Header, widths[i]))
		}
		t.w.Print(t.w.Style(StyleDim, strings.TrimRight(b.String(), " ")))
	}

	for r, row := range t.rows {
		var b strings.Builder
		for i := range t.columns {
			if i >= len(row) {
				break
			}
			if i > 0 {
				b.WriteString("  ")
			}
			cell := row[i]
			if t.columns[i].Right {
				cell = padLeft(cell, widths[i])
			} else {
				cell = Pad(cell, widths[i])
			}
			style := t.columns[i].Style
			if t.styles[r] != nil && i < len(t.styles[r]) && t.styles[r][i] != StyleNone {
				style = t.styles[r][i]
			}
			b.WriteString(t.w.Style(style, cell))
		}
		t.w.Print(strings.TrimRight(b.String(), " "))
	}
}

func padLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = Truncate(s, width)
	if gap := width - Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}
