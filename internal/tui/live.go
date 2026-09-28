// lb-4gm.6
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// liveDebounce gathers a burst of journal records (a close and the
// blocked-state updates committed with it) into one redraw.
const liveDebounce = 200 * time.Millisecond

type liveMsg struct{}    // the mirror applied records or changed state
type refreshMsg struct{} // the debounce elapsed: reload quietly
type pollMsg struct{}    // the polling interval elapsed

// waitChange blocks until the service's mirror moves. With no mirror it
// returns nil, so nothing waits.
func (m Model) waitChange() tea.Cmd {
	if m.svc == nil {
		return nil
	}
	ch := m.svc.Changes()
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		<-ch
		return liveMsg{}
	}
}

// pollTick paces refreshes when no journal drives them.
func (m Model) pollTick() tea.Cmd {
	if m.opts.PollEvery <= 0 {
		return nil
	}
	return tea.Tick(m.opts.PollEvery, func(time.Time) tea.Msg { return pollMsg{} })
}

// liveView reports whether a view is built from reads the mirror answers.
// Health, memory and activity run bd commands of their own, so records do
// not redraw them; the header counts still update.
func liveView(v viewKind) bool {
	switch v {
	case viewReady, viewFocus, viewBlocked, viewIssues, viewDetail, viewGraph:
		return true
	}
	return false
}

// listView is a tab whose rows a live reload replaces.
func listView(v viewKind) bool {
	switch v {
	case viewReady, viewFocus, viewBlocked, viewIssues:
		return true
	}
	return false
}

// accepts reports whether rows loaded for view still belong on screen: a
// reply for a tab the user has since left is dropped. The detail and graph
// views sit over the list they were opened from, which may still refresh.
func (m Model) accepts(view viewKind) bool {
	if m.view == view {
		return true
	}
	return (m.view == viewDetail || m.view == viewGraph) && m.prevView == view
}

// quietReload reloads without the loading indicator, keeping the cursor on
// the same issue even when rows reorder.
func (m Model) quietReload() (Model, tea.Cmd) {
	if listView(m.view) {
		if row, ok := m.currentRow(); ok {
			m.keepID = row.ID
		}
	}
	cmds := []tea.Cmd{m.loadStatus()}
	if liveView(m.view) {
		cmds = append(cmds, m.loadView())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) updateLive(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg.(type) {
	case liveMsg:
		var cmds []tea.Cmd
		if !m.refreshPending {
			m.refreshPending = true
			cmds = append(cmds, tea.Tick(liveDebounce, func(time.Time) tea.Msg { return refreshMsg{} }))
		}
		return m, tea.Batch(append(cmds, m.waitChange())...), true
	case refreshMsg:
		m.refreshPending = false
		m, cmd := m.quietReload()
		return m, cmd, true
	case pollMsg:
		if m.svc != nil && m.svc.LiveMode().Kind == "live" {
			return m, m.pollTick(), true // the journal is doing the work
		}
		if m.svc != nil {
			m.svc.InvalidateCache()
		}
		m, cmd := m.quietReload()
		return m, tea.Batch(cmd, m.pollTick()), true
	}
	return m, nil, false
}

// liveBadge is the header's note on how the views stay current.
func (m Model) liveBadge() string {
	if m.svc == nil {
		return ""
	}
	mode := m.svc.LiveMode()
	badge := map[string]string{"live": "● live", "syncing": "◌ syncing", "polling": "↻ polling"}[mode.Kind]
	if m.opts.ASCII {
		badge = map[string]string{"live": "* live", "syncing": "~ syncing", "polling": "@ polling"}[mode.Kind]
	}
	if mode.Via == "bd serve" {
		badge += " via bd serve"
	}
	return badge
}
