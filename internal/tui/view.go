// lb-0vu
package tui

import (
	"fmt"
	"strings"

	"github.com/lesliesrussell/lazybeads/internal/output"
)

// View renders the current TUI frame. Status is never colour-only.
func (m Model) View() string {
	if m.width < 20 {
		m.width = 80
	}
	if m.height < 10 {
		m.height = 24
	}
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	if m.overlay == overlayHelp {
		b.WriteString(m.renderHelp())
	} else {
		b.WriteString(m.renderBody())
	}
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	if m.overlay == overlayConfirm {
		b.WriteString("\n")
		b.WriteString(m.renderConfirm())
	}
	if m.overlay == overlayFilter || m.overlay == overlayPalette || m.overlay == overlayReason || m.overlay == overlayCreate {
		b.WriteString("\n")
		b.WriteString(m.renderPrompt())
	}
	return b.String()
}

func (m Model) renderHeader() string {
	name := "lazybeads"
	if m.svc != nil && m.svc.Workspace.RootPath != "" {
		name = m.svc.Workspace.RootPath
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	ready, active, blocked := "?", "?", "?"
	health := "ok"
	if m.counts != nil {
		ready = fmt.Sprintf("%d", m.counts.Counts.Ready)
		active = fmt.Sprintf("%d", m.counts.Counts.InProgress)
		blocked = fmt.Sprintf("%d", m.counts.Counts.Blocked)
		health = string(m.counts.Health.Status)
	}
	line := fmt.Sprintf("lb · %s  ready %s · active %s · blocked %s · health %s",
		output.SanitizeLine(name), ready, active, blocked, health)
	return m.truncate(line)
}

func (m Model) renderTabs() string {
	return m.truncate("[r] Ready  [f] Focus  [b] Blocked  [i] Issues  [a] Activity  [m] Memory  [h] Health  [:] Commands  [?] Help")
}

func (m Model) renderBody() string {
	if m.loading {
		return "loading…"
	}
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	if m.view == viewDetail {
		return m.renderDetail()
	}
	if m.view == viewGraph {
		return m.renderGraph()
	}
	if m.view == viewHealth {
		return m.renderHealth()
	}
	return m.renderList()
}

func (m Model) renderList() string {
	rows := m.visibleRows()
	if len(rows) == 0 {
		return "No matching items. Press R to refresh, / to clear filter."
	}
	narrow := m.width < 100
	var b strings.Builder
	title := m.viewTitle()
	if m.filter != "" {
		title += "  filter: " + m.filter
	}
	b.WriteString(title)
	b.WriteString("\n")
	maxRows := m.height - 8
	if maxRows < 3 {
		maxRows = 3
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(rows) {
		end = len(rows)
	}
	idw := 8
	for _, r := range rows {
		if n := len(r.ID); n > idw {
			idw = n
		}
	}
	if idw > 20 {
		idw = 20
	}
	for i := start; i < end; i++ {
		r := rows[i]
		mark := "  "
		if i == m.cursor {
			mark = "> "
		}
		title := output.SanitizeLine(r.Title)
		line := fmt.Sprintf("%s%s  %s  %s", mark, pad(output.SanitizeLine(r.ID), idw), title, r.Meta)
		b.WriteString(m.truncate(line))
		b.WriteString("\n")
		if !narrow && i == m.cursor && r.Issue.Description != "" {
			b.WriteString("    ")
			b.WriteString(m.truncate(output.SanitizeLine(r.Issue.Description)))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderDetail() string {
	if m.detail == nil {
		return "no issue loaded"
	}
	d := m.detail.Detail
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", d.Priority.Label(), output.SanitizeLine(d.ID))
	b.WriteString(output.SanitizeLine(d.Title))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Status: %s  Type: %s\n", d.Status, d.Type)
	if d.Assignee != nil {
		fmt.Fprintf(&b, "Assignee: %s\n", output.SanitizeLine(d.Assignee.String()))
	} else {
		b.WriteString("Assignee: unclaimed\n")
	}
	if d.Description != "" {
		b.WriteString("\n")
		b.WriteString(output.Wrap(output.Sanitize(d.Description), m.width-2))
		b.WriteString("\n")
	}
	b.WriteString("\nBlockers: ")
	if n := len(d.Blockers()); n == 0 {
		b.WriteString("none")
	} else {
		for i, dep := range d.Blockers() {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(dep.Issue.ID)
		}
	}
	fmt.Fprintf(&b, "  Dependents: %d\n", len(d.Dependents))
	if len(m.detail.Suggested) > 0 {
		b.WriteString("\nNext:\n")
		for _, s := range m.detail.Suggested {
			b.WriteString("  ")
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderGraph() string {
	if m.graph == nil {
		return "no graph"
	}
	root, ok := m.graph.Nodes[m.graph.RootID]
	if !ok {
		return "empty graph"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s [%s]\n", root.ID, output.SanitizeLine(root.Title), root.Status)
	edges := m.graph.OutEdges(m.graph.RootID)
	for i, e := range edges {
		prefix := "|-- "
		if i == len(edges)-1 {
			prefix = "`-- "
		}
		node := m.graph.Nodes[e.ToID]
		fmt.Fprintf(&b, "%s%s  %s · %s [%s]\n", prefix, e.Type, node.ID, output.SanitizeLine(node.Title), node.Status)
	}
	return b.String()
}

func (m Model) renderHealth() string {
	if m.health == nil {
		return "no health report"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "health %s\n", m.health.Health.Status)
	for _, c := range m.health.Health.Checks {
		fmt.Fprintf(&b, "%s  %s  %s\n", c.Level, c.Name, c.Summary)
	}
	return b.String()
}

func (m Model) renderHelp() string {
	return strings.Join([]string{
		"Keys",
		"  j/k or arrows   move",
		"  enter           inspect",
		"  c claim   u unclaim   x close   n create",
		"  r ready  f focus  b blocked  i issues",
		"  a activity  m memory  h health  g graph",
		"  / filter  : command  R refresh  ? help",
		"  y copy id   q quit (or back)",
		"Mutations always confirm. TUI calls the same Service as `lb`.",
	}, "\n")
}

func (m Model) renderFooter() string {
	msg := "enter inspect · c claim · x close · / filter · : command · ? help · q quit"
	if m.status != "" {
		msg = m.status + "  ·  " + msg
	}
	return m.truncate(msg)
}

func (m Model) renderConfirm() string {
	var b strings.Builder
	b.WriteString(m.confirm.title)
	b.WriteString("\n")
	b.WriteString(m.confirm.body)
	b.WriteString("\n")
	if m.showArgv && len(m.confirm.argv) > 0 {
		b.WriteString(strings.Join(m.confirm.argv, " "))
		b.WriteString("\n")
	}
	b.WriteString("[y] Confirm  [n/esc] Cancel  [v] View raw command")
	return b.String()
}

func (m Model) renderPrompt() string {
	label := "filter"
	switch m.overlay {
	case overlayPalette:
		label = "command"
	case overlayReason:
		label = "close reason"
	case overlayCreate:
		label = "title"
	}
	return label + "> " + m.input
}

func (m Model) viewTitle() string {
	switch m.view {
	case viewReady:
		n := 0
		if m.ready != nil {
			n = m.ready.Total
		}
		return fmt.Sprintf("READY · %d tasks", n)
	case viewFocus:
		return "FOCUS"
	case viewBlocked:
		return "BLOCKED"
	case viewIssues:
		return "ISSUES"
	case viewActivity:
		return "ACTIVITY"
	case viewMemory:
		return "MEMORY"
	default:
		return "lb"
	}
}

func (m Model) truncate(s string) string {
	return output.Truncate(s, m.width)
}

func pad(s string, width int) string {
	return output.Pad(s, width)
}
