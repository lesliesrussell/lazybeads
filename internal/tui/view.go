// lb-3t3
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

// View paints a lazygit-style tiled UI: stats header, context tabs, a list
// panel beside a live preview, and a keybinding status bar. Overlays sit in
// a centered modal. Status is never colour-only.
func (m Model) View() string {
	m = m.normalized()
	th := newTheme(m.opts)
	base := m.renderChrome(th)
	switch m.overlay {
	case overlayConfirm:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.renderConfirm(th),
			lipgloss.WithWhitespaceChars(" "))
	case overlayHelp:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.renderHelp(th),
			lipgloss.WithWhitespaceChars(" "))
	case overlayFilter, overlayPalette, overlayReason, overlayCreate:
		prompt := m.renderPrompt(th)
		return lipgloss.JoinVertical(lipgloss.Left, base, prompt)
	}
	return base
}

func (m Model) renderChrome(th theme) string {
	header := th.header.Width(m.width).Render(m.headerLine())
	tabs := m.renderTabs(th)
	footer := th.statusBar.Width(m.width).Render(m.footerLine(th))
	body := m.renderBody(th, m.bodyHeight(th))
	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer)
}

// lb-aio
// normalized clamps the frame to a sane minimum so every renderer and the
// scroll arithmetic in contentExtent agree on the same dimensions.
func (m Model) normalized() Model {
	if m.width < 40 {
		m.width = 80
	}
	if m.height < 12 {
		m.height = 24
	}
	return m
}

// bodyHeight is the row budget left for the body panels once the header,
// tabs and status bar have taken their share.
func (m Model) bodyHeight(th theme) int {
	h := m.height -
		lipgloss.Height(th.header.Width(m.width).Render(m.headerLine())) -
		lipgloss.Height(m.renderTabs(th)) -
		lipgloss.Height(th.statusBar.Width(m.width).Render(m.footerLine(th)))
	if h < 5 {
		h = 5
	}
	return h
}

// paneWidths splits the frame between the list and the preview.
func (m Model) paneWidths() (left, right int) {
	left = m.width * 2 / 5
	if left < 36 {
		left = 36
	}
	if left > m.width-40 {
		left = m.width / 2
	}
	return left, m.width - left
}

func (m Model) headerLine() string {
	name := "lazybeads"
	if m.svc != nil && m.svc.Workspace.RootPath != "" {
		name = m.svc.Workspace.RootPath
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	ready, active, blocked, health := "?", "?", "?", "?"
	if m.counts != nil {
		ready = fmt.Sprintf("%d", m.counts.Counts.Ready)
		active = fmt.Sprintf("%d", m.counts.Counts.InProgress)
		blocked = fmt.Sprintf("%d", m.counts.Counts.Blocked)
		health = string(m.counts.Health.Status)
	}
	return fmt.Sprintf(" lb  %s   ready %s   in-progress %s   blocked %s   health %s ",
		output.SanitizeLine(name), ready, active, blocked, health)
}

func (m Model) renderTabs(th theme) string {
	tabs := []struct {
		key  string
		name string
		v    viewKind
	}{
		{"r", "Ready", viewReady},
		{"f", "Focus", viewFocus},
		{"b", "Blocked", viewBlocked},
		{"i", "Issues", viewIssues},
		{"a", "Activity", viewActivity},
		{"m", "Memory", viewMemory},
		{"h", "Health", viewHealth},
	}
	var parts []string
	active := m.view
	if active == viewDetail || active == viewGraph {
		active = viewReady
	}
	for _, t := range tabs {
		label := t.key + " " + t.name
		if active == t.v {
			parts = append(parts, th.tabOn.Render(label))
		} else {
			parts = append(parts, th.tabOff.Render(label))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return lipgloss.NewStyle().Width(m.width).Render(row)
}

func (m Model) renderBody(th theme, height int) string {
	// lb-aio: full-frame content panes scroll rather than dropping the tail.
	if m.view == viewDetail {
		return m.scrolledPanel(th, "Issue", m.renderDetail(th, m.width-2), m.width, height, true)
	}
	if m.view == viewGraph {
		return m.scrolledPanel(th, "Graph", m.renderGraph(th), m.width, height, true)
	}
	if m.view == viewHealth {
		return m.scrolledPanel(th, "Health", m.renderHealth(th), m.width, height, true)
	}
	if m.wide() {
		leftW, rightW := m.paneWidths()
		left := m.panel(th, m.viewTitle(), m.renderList(th, leftW-2, height-2), leftW, height, m.pane == 0)
		right := m.scrolledPanel(th, "Preview", m.renderPreview(th, rightW-2), rightW, height, m.pane == 1)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return m.panel(th, m.viewTitle(), m.renderList(th, m.width-2, height-2), m.width, height, true)
}

// lb-aio
// scrolledPanel draws a content pane through the model's scroll offset and
// advertises the visible window in the panel title, so a reader can tell
// there is more below and how far down they are.
func (m Model) scrolledPanel(th theme, title, content string, width, height int, focused bool) string {
	view, label := clipScroll(content, m.scroll, height-2)
	if label != "" {
		title += " · " + label
	}
	return m.panel(th, title, view, width, height, focused)
}

// lb-aio
// clipScroll windows content at offset, returning the visible lines and a
// "first-last/total" label (empty when everything already fits).
func clipScroll(content string, offset, height int) (string, string) {
	if height < 1 {
		height = 1
	}
	lines := contentLines(content)
	if len(lines) <= height {
		return content, ""
	}
	if maxOff := len(lines) - height; offset > maxOff {
		offset = maxOff
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + height
	return strings.Join(lines[offset:end], "\n"), fmt.Sprintf("%d-%d/%d", offset+1, end, len(lines))
}

// contentLines splits rendered pane content the same way drawPanel does.
func contentLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(content, "\n"), "\n")
}

// lb-aio
// scrollsContent reports whether movement keys drive the content pane
// instead of the list cursor.
func (m Model) scrollsContent() bool {
	switch m.view {
	case viewDetail, viewGraph, viewHealth:
		return true
	}
	return m.wide() && m.pane == 1
}

// lb-aio
// contentExtent re-renders the scrollable pane so key handling can clamp the
// offset against the same line count the frame will draw.
func (m Model) contentExtent() (lines, viewport int) {
	m = m.normalized()
	th := newTheme(m.opts)
	viewport = m.bodyHeight(th) - 2
	if viewport < 1 {
		viewport = 1
	}
	var content string
	switch m.view {
	case viewDetail:
		content = m.renderDetail(th, m.width-2)
	case viewGraph:
		content = m.renderGraph(th)
	case viewHealth:
		content = m.renderHealth(th)
	default:
		_, rightW := m.paneWidths()
		content = m.renderPreview(th, rightW-2)
	}
	return len(contentLines(content)), viewport
}

// lb-aio
// clampScroll keeps the offset inside the content and pins it to zero for
// panes that do not scroll.
func (m *Model) clampScroll() {
	if !m.scrollsContent() {
		m.scroll = 0
		return
	}
	lines, viewport := m.contentExtent()
	maxOff := lines - viewport
	if maxOff < 0 {
		maxOff = 0
	}
	if m.scroll > maxOff {
		m.scroll = maxOff
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m Model) panel(th theme, title, content string, width, height int, focused bool) string {
	return drawPanel(th, title, content, width, height, focused)
}

// lb-q9m
// drawPanel paints a lazygit-style box with the title in the top border.
// The chrome is drawn as plain box-drawing characters and then coloured, so
// a title can never punch through ANSI sequences or clip a corner.
func drawPanel(th theme, title, content string, width, height int, focused bool) string {
	border := lipgloss.RoundedBorder()
	if th.ascii {
		border = lipgloss.ASCIIBorder()
	}
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}
	innerW := width - 2
	innerH := height - 2
	paint := th.borderOff
	if focused {
		paint = th.borderOn
	}

	// lb-xqh
	titleText := strings.TrimSpace(title)
	label := " " + titleText + " "
	lead := 1
	if lead+output.Width(label) > innerW {
		room := innerW - lead - 2
		if room < 1 {
			lead = 0
			room = innerW - 2
		}
		if room < 1 {
			label = output.Truncate(titleText, max(1, innerW))
		} else {
			label = " " + output.Truncate(titleText, room) + " "
		}
	}
	fill := innerW - lead - output.Width(label)
	if fill < 0 {
		fill = 0
	}
	top := paint.Render(border.TopLeft+strings.Repeat(border.Top, lead)) +
		th.title.Render(label) +
		paint.Render(strings.Repeat(border.Top, fill)+border.TopRight)
	bot := paint.Render(border.BottomLeft + strings.Repeat(border.Bottom, innerW) + border.BottomRight)
	sideL := paint.Render(border.Left)
	sideR := paint.Render(border.Right)

	raw := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if content == "" {
		raw = nil
	}
	var b strings.Builder
	b.WriteString(top)
	b.WriteByte('\n')
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(raw) {
			line = raw[i]
		}
		b.WriteString(sideL)
		b.WriteString(fitCellWidth(line, innerW))
		b.WriteString(sideR)
		b.WriteByte('\n')
	}
	b.WriteString(bot)
	return b.String()
}

func fitCellWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) > width {
		s = lipgloss.NewStyle().MaxWidth(width).Inline(true).Render(s)
	}
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (m Model) renderList(th theme, width, height int) string {
	if m.loading {
		return th.dim.Render("Loading…")
	}
	if m.err != nil {
		return th.err.Render(m.err.Error())
	}
	rows := m.visibleRows()
	if len(rows) == 0 {
		return th.dim.Render("No matching items. R refresh  / filter")
	}
	if height < 1 {
		height = 1
	}
	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
	}
	idw := 8
	for _, r := range rows {
		if n := output.Width(r.ID); n > idw {
			idw = n
		}
	}
	if idw > 16 {
		idw = 16
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		r := rows[i]
		prio := r.Issue.Priority.Label()
		if r.Issue.ID == "" {
			prio = "  "
		}
		id := output.Pad(output.SanitizeLine(r.ID), idw)
		title := output.SanitizeLine(r.Title)
		// lb-ank
		prefix := prio + "  " + id + "  "
		if r.Issue.ID != "" {
			prefix += output.Pad(th.statusCell(r.Issue.Status), 14) + "  "
		}
		extra := ""
		if r.Meta != "" && (r.Issue.ID == "" || !strings.Contains(strings.ToLower(r.Meta), strings.ToLower(string(r.Issue.Status)))) {
			extra = "  " + output.SanitizeLine(r.Meta)
		}
		inner := width - 2
		if inner < 8 {
			inner = 8
		}
		titleW := inner - output.Width(prefix) - output.Width(extra)
		if titleW < 4 {
			titleW = 4
		}
		title = output.Truncate(title, titleW)
		rest := prefix + title + extra
		if output.Width(rest) > inner {
			rest = output.Truncate(rest, inner)
		}
		caret := "❯ "
		if th.ascii {
			caret = "> "
		}
		if i == m.cursor {
			b.WriteString(th.selected.Width(width).Render(caret + rest))
		} else if r.Issue.IsClosed() {
			b.WriteString(th.dim.Width(width).Render("  " + rest))
		} else {
			b.WriteString("  " + rest)
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderPreview(th theme, width int) string {
	row, ok := m.currentRow()
	if !ok {
		return th.dim.Render("No selection")
	}
	if m.detail != nil && m.detail.Detail.ID == row.ID {
		return m.renderIssueFrom(th, m.detail.Detail.Issue, m.detail, width)
	}
	if row.Issue.ID != "" {
		return m.renderIssueFrom(th, row.Issue, nil, width)
	}
	var b strings.Builder
	b.WriteString(th.title.Render(output.SanitizeLine(row.Title)))
	b.WriteString("\n")
	b.WriteString(th.dim.Render(row.Meta))
	return b.String()
}

func (m Model) renderDetail(th theme, width int) string {
	if m.detail == nil {
		if row, ok := m.currentRow(); ok && row.Issue.ID != "" {
			return m.renderIssueFrom(th, row.Issue, nil, width)
		}
		return th.dim.Render("No issue loaded")
	}
	return m.renderIssueFrom(th, m.detail.Detail.Issue, m.detail, width)
}

func (m Model) renderIssueFrom(th theme, d domain.Issue, show *app.ShowResult, width int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", th.prio(int(d.Priority)).Render(d.Priority.Label()), th.title.Render(output.SanitizeLine(d.ID)))
	b.WriteString(th.title.Render(output.SanitizeLine(d.Title)))
	b.WriteString("\n\n")
	status := string(d.Status)
	if status == "" {
		status = "unknown"
	}
	assignee := "unclaimed"
	if d.Assignee != nil && d.Assignee.String() != "" {
		assignee = output.SanitizeLine(d.Assignee.String())
	}
	b.WriteString(th.dim.Render("Status") + "  " + status + "\n")
	b.WriteString(th.dim.Render("Type  ") + "  " + string(d.Type) + "\n")
	b.WriteString(th.dim.Render("Owner ") + "  " + assignee + "\n")
	if len(d.Labels) > 0 {
		b.WriteString(th.dim.Render("Labels") + "  " + output.SanitizeLine(strings.Join(d.Labels, ", ")) + "\n")
	}
	if d.Description != "" {
		b.WriteString("\n")
		b.WriteString(output.Wrap(output.Sanitize(d.Description), width))
		b.WriteString("\n")
	}
	if show != nil {
		blockers := show.Detail.Blockers()
		b.WriteString("\n")
		b.WriteString(th.dim.Render("Blockers   "))
		if len(blockers) == 0 {
			b.WriteString("none")
		} else {
			for i, dep := range blockers {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(dep.Issue.ID)
			}
		}
		fmt.Fprintf(&b, "\n%s %d\n", th.dim.Render("Dependents"), len(show.Detail.Dependents))
		if len(show.Suggested) > 0 {
			b.WriteString("\n")
			b.WriteString(th.dim.Render("Next"))
			b.WriteString("\n")
			for _, s := range show.Suggested {
				b.WriteString("  " + s + "\n")
			}
		}
	}
	return b.String()
}

func (m Model) renderGraph(th theme) string {
	if m.graph == nil {
		return th.dim.Render("No graph")
	}
	root, ok := m.graph.Nodes[m.graph.RootID]
	if !ok {
		return th.dim.Render("Empty graph")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s [%s]\n", root.ID, output.SanitizeLine(root.Title), root.Status)
	edges := m.graph.OutEdges(m.graph.RootID)
	branch, last := "├── ", "└── "
	if m.opts.ASCII {
		branch, last = "|-- ", "`-- "
	}
	for i, e := range edges {
		prefix := branch
		if i == len(edges)-1 {
			prefix = last
		}
		node := m.graph.Nodes[e.ToID]
		fmt.Fprintf(&b, "%s%s  %s · %s [%s]\n", prefix, e.Type, node.ID, output.SanitizeLine(node.Title), node.Status)
	}
	return b.String()
}

func (m Model) renderHealth(th theme) string {
	if m.health == nil {
		return th.dim.Render("No health report")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "health %s\n\n", m.health.Health.Status)
	for _, c := range m.health.Health.Checks {
		mark := th.glyph(output.SymBullet)
		st := th.dim
		switch c.Level {
		case domain.HealthOK:
			mark, st = th.glyph(output.SymClosed), th.ok
		case domain.HealthWarning:
			mark, st = th.glyph(output.SymWarning), th.warn
		case domain.HealthError:
			mark, st = th.glyph(output.SymError), th.err
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", st.Render(mark), c.Name, c.Summary)
	}
	return b.String()
}

func (m Model) renderHelp(th theme) string {
	body := strings.Join([]string{
		th.title.Render("Keys"),
		"",
		"  " + th.key.Render("j/k") + "  move          " + th.key.Render("tab") + "  other pane",
		"  " + th.key.Render("tab") + " then " + th.key.Render("j/k") + "  scroll preview   " + th.key.Render("ctrl+d/u") + "  page   " + th.key.Render("G/home") + "  ends",
		"  " + th.key.Render("enter") + "  inspect      " + th.key.Render("g") + "  graph",
		"  " + th.key.Render("c") + "  claim          " + th.key.Render("x") + "  close",
		"  " + th.key.Render("u") + "  unclaim        " + th.key.Render("n") + "  create",
		"  " + th.key.Render("r/f/b/i/a/m/h") + "  switch view",
		"  " + th.key.Render("/") + "  filter (closed, status:open)   " + th.key.Render(":") + "  command",
		"  " + th.key.Render("[/]") + "  cycle status filter (issues view)",
		"  " + th.key.Render("R") + "  refresh        " + th.key.Render("?") + "  help",
		"  " + th.key.Render("y") + "  copy id        " + th.key.Render("Y") + "  copy lb show <id>",
		"  " + th.key.Render("q") + "  back / quit",
		"",
		th.dim.Render("Mutations confirm. TUI calls the same Service as lb."),
	}, "\n")
	return th.modal.Width(52).Render(body)
}

func (m Model) footerLine(th theme) string {
	key := func(k, label string) string {
		return th.key.Render(k) + " " + label
	}
	msg := strings.Join([]string{
		key("j/k", "move"),
		key("enter", "inspect"),
		key("c", "claim"),
		key("x", "close"),
		key("/", "filter"),
		key(":", "command"),
		key("?", "help"),
		key("q", "quit"),
	}, "  ")
	if m.status != "" {
		msg = output.SanitizeLine(m.status) + "   " + msg
	}
	return output.Truncate(" "+msg+" ", m.width)
}

func (m Model) renderConfirm(th theme) string {
	var b strings.Builder
	b.WriteString(th.title.Render(m.confirm.title))
	b.WriteString("\n\n")
	b.WriteString(m.confirm.body)
	b.WriteString("\n\n")
	if m.showArgv && len(m.confirm.argv) > 0 {
		b.WriteString(th.dim.Render(strings.Join(m.confirm.argv, " ")))
		b.WriteString("\n\n")
	}
	b.WriteString(th.key.Render("y") + " confirm   " + th.key.Render("n") + " cancel   " + th.key.Render("v") + " argv")
	return th.modal.Width(min(72, m.width-4)).Render(b.String())
}

func (m Model) renderPrompt(th theme) string {
	label := "filter"
	switch m.overlay {
	case overlayPalette:
		label = "command"
	case overlayReason:
		label = "close reason"
	case overlayCreate:
		label = "title"
	}
	return th.focused.Width(m.width - 2).Render(th.key.Render(label) + "> " + m.input + "█")
}

func (m Model) viewTitle() string {
	switch m.view {
	case viewReady:
		n := 0
		if m.ready != nil {
			n = m.ready.Total
		}
		t := fmt.Sprintf("Ready · %d", n)
		if m.filter != "" {
			t += " /" + m.filter
		}
		return t
	case viewFocus:
		return "Focus"
	case viewBlocked:
		return "Blocked"
	case viewIssues:
		// lb-zhz
		if m.statusFilter != "" {
			return "Issues · " + m.statusFilter
		}
		return "Issues · all"
	case viewActivity:
		return "Activity"
	case viewMemory:
		return "Memory"
	default:
		return "lb"
	}
}
