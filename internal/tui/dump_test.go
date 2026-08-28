// lb-xqh
package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDumpIssuesFrame(t *testing.T) {
	m, _ := testModel(t)
	m.opts.Color = true
	m.opts.ASCII = false
	m.width, m.height = 120, 24
	m = pump(m, m.Init())
	nm, cmd := m.Update(key("i"))
	m = pump(nm.(Model), cmd)

	view := m.View()
	t.Logf("view width=%d height=%d wide=%v pane=%d", m.width, m.height, m.wide(), m.pane)
	for i, line := range strings.Split(view, "\n") {
		if i > 8 {
			break
		}
		vis := stripANSI(line)
		t.Logf("L%02d visW=%3d rawW=%3d |%s|", i, lipgloss.Width(line), len(line), vis)
	}

	th := newTheme(m.opts)
	bodyH := 18
	leftW := m.width * 2 / 5
	rightW := m.width - leftW
	left := m.panel(th, m.viewTitle(), m.renderList(th, leftW-2, bodyH-2), leftW, bodyH, true)
	right := m.panel(th, "Preview", m.renderPreview(th, rightW-2), rightW, bodyH, false)
	joined := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	ltop := strings.Split(left, "\n")[0]
	rtop := strings.Split(right, "\n")[0]
	jtop := strings.Split(joined, "\n")[0]
	t.Logf("leftW=%d rightW=%d leftTopVis=%d rightTopVis=%d joinedTopVis=%d want=%d",
		leftW, rightW, lipgloss.Width(ltop), lipgloss.Width(rtop), lipgloss.Width(jtop), m.width)
	t.Logf("leftTop  |%s|", stripANSI(ltop))
	t.Logf("rightTop |%s|", stripANSI(rtop))
	t.Logf("joinTop  |%s|", stripANSI(jtop))
	if !strings.Contains(stripANSI(ltop), "─ Issues ") {
		t.Fatalf("left title not on the top rule: %q", stripANSI(ltop))
	}
	if lipgloss.Width(jtop) != m.width {
		t.Fatalf("joined top width %d, want %d", lipgloss.Width(jtop), m.width)
	}
}

func TestDumpIssuesAtCommonWidths(t *testing.T) {
	for _, w := range []int{80, 90, 100, 110, 132, 160} {
		m, _ := testModel(t)
		m.opts.Color = true
		m.opts.ASCII = false
		m.width, m.height = w, 24
		m = pump(m, m.Init())
		nm, cmd := m.Update(key("i"))
		m = pump(nm.(Model), cmd)
		lines := strings.Split(m.View(), "\n")
		t.Logf("==== width %d wide=%v lines=%d ====", w, m.wide(), len(lines))
		for i, line := range lines {
			if i > 5 {
				break
			}
			wvis := lipgloss.Width(line)
			mark := ""
			if wvis != w {
				mark = fmt.Sprintf("  WIDTH MISMATCH want=%d", w)
			}
			t.Logf("L%02d w=%3d |%s|%s", i, wvis, stripANSI(line), mark)
			if i == 2 {
				vis := stripANSI(line)
				if !strings.Contains(vis, "─ Issues") {
					t.Errorf("width %d: Issues title not on the top rule: %q", w, vis)
				}
				if wvis != w {
					t.Errorf("width %d: line 2 width %d, want %d", w, wvis, w)
				}
			}
		}
	}
}
