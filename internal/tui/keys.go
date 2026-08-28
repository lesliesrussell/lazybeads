// lb-0vu
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.overlay {
	case overlayFilter, overlayPalette, overlayReason, overlayCreate:
		return m.handleInputKey(k, msg)
	case overlayConfirm, overlayHelp:
		return m.handleOverlayKey(k)
	}
	return m.handleNavKey(k)
}

func (m Model) handleNavKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q":
		if m.view == viewDetail || m.view == viewGraph || m.view == viewHelp {
			m.view = m.prevView
			return m, m.loadView()
		}
		return m, tea.Quit
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.clampCursor()
			return m, nil
		}
		if m.view == viewDetail || m.view == viewGraph || m.view == viewHelp {
			m.view = m.prevView
			return m, m.loadView()
		}
		return m, nil
	case "j", "down":
		m.cursor++
		m.clampCursor()
		return m, m.previewCmd()
	case "k", "up":
		m.cursor--
		m.clampCursor()
		return m, m.previewCmd()
	case "ctrl+d":
		m.cursor += 10
		m.clampCursor()
	case "ctrl+u":
		m.cursor -= 10
		m.clampCursor()
	case "G":
		if n := len(m.visibleRows()); n > 0 {
			m.cursor = n - 1
		}
	case "home":
		m.cursor = 0
	case "r":
		return m.switchView(viewReady)
	case "f":
		return m.switchView(viewFocus)
	case "b":
		return m.switchView(viewBlocked)
	case "i":
		return m.switchView(viewIssues)
	case "a":
		return m.switchView(viewActivity)
	case "m":
		return m.switchView(viewMemory)
	case "h":
		return m.switchView(viewHealth)
	case "g":
		if row, ok := m.currentRow(); ok {
			m.selectedID = row.ID
		}
		if m.selectedID == "" {
			return m, nil
		}
		m.prevView = m.view
		m.view = viewGraph
		m.loading = true
		return m, m.loadView()
	case "enter":
		row, ok := m.currentRow()
		if !ok || row.ID == "" {
			return m, nil
		}
		m.selectedID = row.ID
		m.prevView = m.view
		m.view = viewDetail
		m.loading = true
		return m, m.loadView()
	case "c":
		return m.beginConfirm("claim")
	case "u":
		return m.beginConfirm("unclaim")
	case "x":
		m.overlay = overlayReason
		m.input = ""
		m.confirm.kind = "close"
		if row, ok := m.currentRow(); ok {
			m.selectedID = row.ID
			m.confirm.issue = row.Issue
		}
	case "n":
		m.overlay = overlayCreate
		m.input = ""
	case "R":
		m.loading = true
		m.err = nil
		return m, tea.Batch(m.loadView(), m.loadStatus())
	case "/":
		m.overlay = overlayFilter
		m.input = m.filter
	case ":":
		m.overlay = overlayPalette
		m.input = ""
	case "?":
		m.overlay = overlayHelp
	case "tab":
		if m.wide() {
			m.pane = 1 - m.pane
		}
		return m, nil
	case "y":
		if row, ok := m.currentRow(); ok {
			m.copied = row.ID
			m.status = "copied " + row.ID
		} else if m.selectedID != "" {
			m.copied = m.selectedID
			m.status = "copied " + m.selectedID
		}
	case "Y":
		id := m.selectedID
		if row, ok := m.currentRow(); ok {
			id = row.ID
		}
		if id != "" {
			m.copied = "lb show " + id
			m.status = "copied lb show " + id
		}
	}
	return m, nil
}

func (m Model) switchView(v viewKind) (tea.Model, tea.Cmd) {
	m.view = v
	m.cursor = 0
	m.overlay = overlayNone
	m.loading = true
	m.err = nil
	return m, m.loadView()
}

func (m Model) handleInputKey(k string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.overlay = overlayNone
		m.input = ""
		return m, nil
	case "enter":
		switch m.overlay {
		case overlayFilter:
			m.filter = m.input
			m.overlay = overlayNone
			m.cursor = 0
			m.clampCursor()
			if m.view == viewIssues {
				m.loading = true
				return m, m.loadView()
			}
		case overlayReason:
			if strings.TrimSpace(m.input) == "" {
				m.status = "a close reason is required"
				return m, nil
			}
			m.confirm.reason = strings.TrimSpace(m.input)
			m.overlay = overlayConfirm
			m.confirm.kind = "close"
			m.confirm.title = "Close " + m.selectedID + "?"
			m.confirm.body = "Reason: " + m.confirm.reason
			return m, nil
		case overlayCreate:
			if strings.TrimSpace(m.input) == "" {
				m.status = "a title is required"
				return m, nil
			}
			m.overlay = overlayConfirm
			m.confirm.kind = "create"
			m.confirm.title = "Create issue?"
			m.confirm.body = "Title: " + strings.TrimSpace(m.input)
			m.confirm.reason = strings.TrimSpace(m.input)
			return m, nil
		case overlayPalette:
			return m.runPalette(strings.TrimSpace(m.input))
		}
		return m, nil
	case "backspace":
		if m.input != "" {
			_, size := utf8.DecodeLastRuneInString(m.input)
			m.input = m.input[:len(m.input)-size]
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.input += string(msg.Runes)
	}
	return m, nil
}

func (m Model) handleOverlayKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "n":
		m.overlay = overlayNone
		m.showArgv = false
		return m, nil
	case "q":
		if m.overlay == overlayHelp {
			m.overlay = overlayNone
			return m, nil
		}
	case "y":
		if m.overlay == overlayConfirm {
			return m.executeConfirm()
		}
	case "v":
		if m.overlay == overlayConfirm {
			m.showArgv = !m.showArgv
		}
	case "?":
		m.overlay = overlayNone
	}
	return m, nil
}

func (m Model) beginConfirm(kind string) (tea.Model, tea.Cmd) {
	row, ok := m.currentRow()
	id := m.selectedID
	issue := row.Issue
	if ok {
		id = row.ID
	}
	if id == "" {
		m.status = "no issue selected"
		return m, nil
	}
	m.selectedID = id
	m.overlay = overlayConfirm
	m.showArgv = false
	m.confirm = confirmState{
		kind:  kind,
		issue: issue,
		title: kind + " " + id + "?",
	}
	switch kind {
	case "claim":
		m.confirm.body = "This will ask Beads to atomically claim:\n  Issue:  " + id + " — " + issue.Title +
			"\n  Actor:  " + m.svc.Actor + "\n  Change: → in_progress; assignee → " + m.svc.Actor
	case "unclaim":
		m.confirm.body = "Release claim on " + id + " — " + issue.Title
	}
	return m, nil
}

func (m Model) executeConfirm() (tea.Model, tea.Cmd) {
	svc := m.svc
	id := m.selectedID
	kind := m.confirm.kind
	reason := m.confirm.reason
	m.loading = true
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			res *app.MutationResult
			err error
		)
		switch kind {
		case "claim":
			res, err = svc.Claim(ctx, id, app.MutateOptions{Actor: svc.Actor})
		case "unclaim":
			res, err = svc.Unclaim(ctx, id, app.MutateOptions{})
		case "close":
			res, err = svc.Close(ctx, id, reason, app.MutateOptions{})
		case "create":
			res, err = svc.Create(ctx, beads.CreateIssueInput{Title: reason, Type: "task"}, app.MutateOptions{})
		default:
			err = fmt.Errorf("unknown action %s", kind)
		}
		if err != nil {
			return errMsg{err}
		}
		return mutatedMsg{res}
	}
}

func (m Model) runPalette(cmd string) (tea.Model, tea.Cmd) {
	m.overlay = overlayNone
	cmd = strings.ToLower(cmd)
	switch {
	case cmd == "q" || cmd == "quit":
		return m, tea.Quit
	case strings.HasPrefix(cmd, "ready"):
		return m.switchView(viewReady)
	case strings.HasPrefix(cmd, "focus"):
		return m.switchView(viewFocus)
	case strings.HasPrefix(cmd, "blocked"):
		return m.switchView(viewBlocked)
	case strings.HasPrefix(cmd, "issue"):
		return m.switchView(viewIssues)
	case strings.HasPrefix(cmd, "activity"):
		return m.switchView(viewActivity)
	case strings.HasPrefix(cmd, "memory"):
		return m.switchView(viewMemory)
	case strings.HasPrefix(cmd, "health") || cmd == "doctor" || cmd == "status":
		return m.switchView(viewHealth)
	case strings.HasPrefix(cmd, "help"):
		m.overlay = overlayHelp
		return m, nil
	case strings.HasPrefix(cmd, "claim"):
		return m.beginConfirm("claim")
	case strings.HasPrefix(cmd, "refresh"):
		m.loading = true
		return m, tea.Batch(m.loadView(), m.loadStatus())
	}
	m.status = "unknown command: " + cmd
	return m, nil
}
