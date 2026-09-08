// lb-0vu
package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

func testModel(t *testing.T) (Model, *app.MemClient) {
	t.Helper()
	f := app.NewMemClient()
	f.Add(domain.Issue{ID: "lb-1", Title: "Add typed bd adapter", Priority: 1, Type: domain.TypeTask})
	f.Add(domain.Issue{ID: "lb-2", Title: "Implement lb status", Priority: 1, Type: domain.TypeTask})
	f.Add(domain.Issue{ID: "lb-3", Title: "Shipped foundation", Priority: 2, Type: domain.TypeTask, Status: domain.StatusClosed})
	f.Dep("lb-2", "lb-1")
	svc := app.NewService(f, config.Default(), workspace.Workspace{
		RootPath: "/fake", BeadsDir: "/fake/.beads", BDVersion: "1.0.5",
	}, "operator", nil)
	m := New(svc, Options{Width: 120, Height: 32, ASCII: true})
	return m, f
}

func pump(m Model, cmd tea.Cmd) Model {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = pump(m, c)
			}
			return m
		}
		nm, next := m.Update(msg)
		m = nm.(Model)
		cmd = next
	}
	return m
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestReadyViewListsUnblockedIssues(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	view := m.View()
	if !strings.Contains(view, "Add typed bd adapter") {
		t.Fatalf("ready view missing unblocked issue:\n%s", view)
	}
	if strings.Contains(view, "Implement lb status") {
		t.Errorf("blocked issue leaked into ready view:\n%s", view)
	}
}

func TestClaimOpensConfirmThenMutates(t *testing.T) {
	m, f := testModel(t)
	m = pump(m, m.Init())
	nm, cmd := m.Update(key("c"))
	m = nm.(Model)
	if m.overlay != overlayConfirm {
		t.Fatalf("overlay = %v, want confirm", m.overlay)
	}
	if !strings.Contains(m.View(), "lb-1") {
		t.Fatalf("confirm modal must name the issue:\n%s", m.View())
	}
	nm, cmd = m.Update(key("y"))
	m = pump(nm.(Model), cmd)
	got, err := f.Show(context.Background(), "lb-1", beads.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusInProgress {
		t.Errorf("after confirm, status = %s, want in_progress", got.Status)
	}
}

func TestEnterOpensDetail(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(nm.(Model), cmd)
	if m.view != viewDetail {
		t.Fatalf("view = %v, want detail", m.view)
	}
	if !strings.Contains(m.View(), "Add typed bd adapter") {
		t.Errorf("detail missing title:\n%s", m.View())
	}
}

func TestQuitAtTopLevel(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q at top level should quit")
	}
}

func TestWideLayoutIsDualPaneWithBorders(t *testing.T) {
	m, _ := testModel(t)
	m = pump(m, m.Init())
	view := m.View()
	if !strings.Contains(view, "Preview") {
		t.Fatalf("wide layout must show a Preview pane:\n%s", view)
	}
	if !strings.Contains(view, "Ready") {
		t.Fatalf("list pane title missing:\n%s", view)
	}
	if !strings.ContainsAny(view, "+-|") {
		t.Fatalf("ASCII mode should draw box borders:\n%s", view)
	}
	if !strings.Contains(view, "❯") && !strings.Contains(view, ">") {
		t.Fatalf("selected row needs a caret:\n%s", view)
	}
}

func TestPanelTitleIsNotClipped(t *testing.T) {
	th := newTheme(Options{Color: true, ASCII: false})
	box := drawPanel(th, "Issues", "row", 22, 5, true)
	top := strings.Split(box, "\n")[0]
	vis := stripANSI(top)
	// lb-xqh
	if !strings.Contains(vis, "─ Issues ") && !strings.Contains(vis, "- Issues ") {
		t.Fatalf("title missing or not seated on the top rule: %q (raw %q)", vis, top)
	}
	if lipgloss.Width(top) != 22 {
		t.Errorf("top border width = %d, want 22", lipgloss.Width(top))
	}
	left, right := []rune(vis)[0], []rune(vis)[len([]rune(vis))-1]
	if left != '╭' && left != '┌' && left != '+' {
		t.Errorf("left corner overwritten: %q", vis)
	}
	if right != '╮' && right != '┐' && right != '+' {
		t.Errorf("right corner overwritten: %q", vis)
	}
	if strings.Contains(top, "m╭\x1b[0m Issues") || strings.Contains(top, "m+\x1b[0m Issues") {
		t.Fatalf("title is unstyled in a hole after the corner: %q", top)
	}

	thASCII := newTheme(Options{Color: false, ASCII: true})
	box = drawPanel(thASCII, "Issues", "row", 22, 5, true)
	vis = stripANSI(strings.Split(box, "\n")[0])
	if !strings.Contains(vis, "- Issues ") {
		t.Fatalf("ASCII title not seated on the top rule: %q", vis)
	}
}

func TestIssuesViewKeepsFullPanelTitle(t *testing.T) {
	m, _ := testModel(t)
	m.opts.Color = true
	m.opts.ASCII = false
	m = pump(m, m.Init())
	nm, cmd := m.Update(key("i"))
	m = pump(nm.(Model), cmd)
	var titled string
	for _, line := range strings.Split(m.View(), "\n") {
		vis := stripANSI(line)
		if strings.Contains(vis, "Issues") && (strings.Contains(vis, "─") || strings.Contains(vis, "-")) {
			titled = vis
			break
		}
	}
	if titled == "" {
		t.Fatalf("no Issues panel title in:\n%s", m.View())
	}
	if !strings.Contains(titled, " Issues ") && !strings.Contains(titled, "Issues") {
		t.Fatalf("clipped panel title: %q", titled)
	}
	if strings.Contains(titled, "Issue ") && !strings.Contains(titled, "Issues") {
		t.Fatalf("left title was truncated to %q", titled)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	skip := false
	for _, r := range s {
		if r == 0x1b {
			skip = true
			continue
		}
		if skip {
			if r >= 0x40 && r <= 0x7e && r != '[' {
				skip = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestIssuesViewMarksClosedAndFiltersStatus(t *testing.T) {
	// lb-ank
	m, _ := testModel(t)
	m.opts.ASCII = true
	m = pump(m, m.Init())
	nm, cmd := m.Update(key("i"))
	m = pump(nm.(Model), cmd)
	view := stripANSI(m.View())
	if !strings.Contains(view, "closed") || !strings.Contains(view, "lb-3") {
		t.Fatalf("issues view must show closed status on the row:\n%s", view)
	}
	if !strings.Contains(view, "+ closed") && !strings.Contains(view, "closed") {
		t.Fatalf("closed glyph+label missing:\n%s", view)
	}

	m.filter = "closed"
	m = pump(m, m.loadView())
	filtered := stripANSI(m.View())
	if !strings.Contains(filtered, "lb-3") {
		t.Fatalf("/closed must keep closed issues:\n%s", filtered)
	}
	if strings.Contains(filtered, "lb-1") {
		t.Fatalf("/closed must hide open issues:\n%s", filtered)
	}

	m.filter = "status:open"
	m = pump(m, m.loadView())
	openOnly := stripANSI(m.View())
	if strings.Contains(openOnly, "lb-3") {
		t.Fatalf("status:open leaked closed issue:\n%s", openOnly)
	}
	if !strings.Contains(openOnly, "lb-1") {
		t.Fatalf("status:open missing open issue:\n%s", openOnly)
	}
}

func TestNarrowLayoutHidesPreview(t *testing.T) {
	m, _ := testModel(t)
	m.width = 80
	m = pump(m, m.Init())
	view := m.View()
	if strings.Contains(view, "Preview") {
		t.Fatalf("narrow layout should be a single pane:\n%s", view)
	}
}

// lb-aio
func longIssue(id string) domain.Issue {
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "body line %02d\n", i)
	}
	b.WriteString("TAILMARK")
	return domain.Issue{ID: id, Title: "Very long issue", Priority: 1, Type: domain.TypeTask, Description: b.String()}
}

func TestDetailViewScrollsToContentBelowTheFold(t *testing.T) {
	m, f := testModel(t)
	f.Add(longIssue("lb-9"))
	m.opts.ASCII = true
	m.view = viewDetail
	m.selectedID = "lb-9"
	m = pump(m, m.loadView())

	first := stripANSI(m.View())
	if strings.Contains(first, "TAILMARK") {
		t.Fatalf("test issue is not taller than the pane:\n%s", first)
	}
	if !strings.Contains(first, "body line 01") {
		t.Fatalf("detail should start at the top:\n%s", first)
	}

	nm, _ := m.Update(key("G"))
	m = nm.(Model)
	bottom := stripANSI(m.View())
	if !strings.Contains(bottom, "TAILMARK") {
		t.Fatalf("G must reach the end of the detail body:\n%s", bottom)
	}

	nm, _ = m.Update(key("G"))
	m = nm.(Model)
	if again := stripANSI(m.View()); again != bottom {
		t.Errorf("scroll ran past the end of the content")
	}

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = nm.(Model)
	if top := stripANSI(m.View()); !strings.Contains(top, "body line 01") || strings.Contains(top, "TAILMARK") {
		t.Errorf("home must return to the top of the body:\n%s", top)
	}
}

func TestPreviewPaneScrollsWhenFocused(t *testing.T) {
	m, f := testModel(t)
	f.Add(longIssue("lb-9"))
	m.opts.ASCII = true
	m = pump(m, m.Init())
	nm, cmd := m.Update(key("i"))
	m = pump(nm.(Model), cmd)
	for i, r := range m.visibleRows() {
		if r.ID == "lb-9" {
			m.cursor = i
		}
	}
	m = pump(m, m.previewCmd())
	if !m.wide() {
		t.Fatal("test frame should be wide enough for a preview pane")
	}

	nm, _ = m.Update(key("j"))
	if nm.(Model).scroll != 0 {
		t.Fatal("j in the list pane must move the cursor, not scroll the preview")
	}

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = nm.(Model)
	if m.pane != 1 {
		t.Fatalf("tab should focus the preview pane, got pane %d", m.pane)
	}
	if strings.Contains(stripANSI(m.View()), "TAILMARK") {
		t.Fatal("preview should start clipped for a long issue")
	}
	nm, _ = m.Update(key("G"))
	m = nm.(Model)
	if m.scroll == 0 {
		t.Fatal("G in the preview pane must scroll it")
	}
	if !strings.Contains(stripANSI(m.View()), "TAILMARK") {
		t.Fatalf("preview scroll never reached the tail:\n%s", m.View())
	}
}
