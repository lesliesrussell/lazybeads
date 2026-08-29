// lb-0vu
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

type viewKind int

const (
	viewReady viewKind = iota
	viewFocus
	viewBlocked
	viewIssues
	viewDetail
	viewGraph
	viewActivity
	viewMemory
	viewHealth
	viewHelp
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayConfirm
	overlayFilter
	overlayPalette
	overlayReason
	overlayCreate
	overlayHelp
)

// Options configure a TUI session. Business data still comes from Service.
type Options struct {
	ASCII  bool
	Color  bool
	View   string
	Issue  string
	Width  int
	Height int
}

// Model is the Bubble Tea program. Every read and write goes through Service.
type Model struct {
	svc  *app.Service
	opts Options

	width, height int
	view          viewKind
	overlay       overlayKind
	loading       bool
	err           error
	status        string

	cursor int
	rows   []listRow
	filter string
	input  string

	ready    *app.ReadyResult
	focus    *app.FocusReport
	blocked  *app.BlockedResult
	issues   []domain.Issue
	detail   *app.ShowResult
	graph    *domain.IssueGraph
	activity *app.ActivityResult
	memories []domain.Memory
	health   *app.DoctorReport
	counts   *app.StatusReport

	confirm     confirmState
	prevView    viewKind
	selectedID  string
	showArgv    bool
	copied      string
	lastRefresh time.Time
	pane        int // 0 list, 1 preview — lazygit-style focused panel
}

type listRow struct {
	ID    string
	Title string
	Meta  string
	Issue domain.Issue
}

type confirmState struct {
	kind   string
	title  string
	body   string
	argv   []string
	issue  domain.Issue
	reason string
}

type readyMsg struct{ res *app.ReadyResult }
type focusMsg struct{ res *app.FocusReport }
type blockedMsg struct{ res *app.BlockedResult }
type issuesMsg struct{ issues []domain.Issue }
type showMsg struct{ res *app.ShowResult }
type graphMsg struct{ g *domain.IssueGraph }
type activityMsg struct{ res *app.ActivityResult }
type memoryMsg struct{ items []domain.Memory }
type healthMsg struct{ res *app.DoctorReport }
type statusMsg struct{ res *app.StatusReport }
type mutatedMsg struct{ res *app.MutationResult }
type errMsg struct{ err error }

// New builds a TUI model around an already-resolved Service.
func New(svc *app.Service, opts Options) Model {
	if opts.Width <= 0 {
		opts.Width = 80
	}
	if opts.Height <= 0 {
		opts.Height = 24
	}
	m := Model{svc: svc, opts: opts, width: opts.Width, height: opts.Height, view: viewReady, loading: true}
	switch strings.ToLower(opts.View) {
	case "focus":
		m.view = viewFocus
	case "blocked":
		m.view = viewBlocked
	case "issues", "all":
		m.view = viewIssues
	case "activity":
		m.view = viewActivity
	case "memory":
		m.view = viewMemory
	case "health", "status", "doctor":
		m.view = viewHealth
	}
	if opts.Issue != "" {
		m.selectedID = opts.Issue
		m.view = viewDetail
	}
	return m
}

// Init loads the current view and the header counts.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadView(), m.loadStatus())
}

func (m Model) loadView() tea.Cmd {
	svc := m.svc
	view := m.view
	id := m.selectedID
	filter := m.filter
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		switch view {
		case viewReady:
			res, err := svc.Ready(ctx, app.ReadyRequest{})
			if err != nil {
				return errMsg{err}
			}
			return readyMsg{res}
		case viewFocus:
			res, err := svc.Focus(ctx, app.FocusRequest{})
			if err != nil {
				return errMsg{err}
			}
			return focusMsg{res}
		case viewBlocked:
			res, err := svc.Blocked(ctx, app.BlockedRequest{})
			if err != nil {
				return errMsg{err}
			}
			return blockedMsg{res}
		case viewIssues:
			res, err := svc.List(ctx, app.ListRequest{Query: filter, All: true})
			if err != nil {
				return errMsg{err}
			}
			return issuesMsg{res.Issues}
		case viewDetail:
			if id == "" {
				return errMsg{fmt.Errorf("no issue selected")}
			}
			res, err := svc.Show(ctx, id, app.ShowRequest{})
			if err != nil {
				return errMsg{err}
			}
			return showMsg{res}
		case viewGraph:
			if id == "" {
				return errMsg{fmt.Errorf("no issue selected")}
			}
			g, err := svc.BuildGraph(ctx, app.GraphRequest{RootID: id, Direction: app.DirBoth})
			if err != nil {
				return errMsg{err}
			}
			return graphMsg{g}
		case viewActivity:
			res, err := svc.Activity(ctx, app.ActivityRequest{})
			if err != nil {
				return errMsg{err}
			}
			return activityMsg{res}
		case viewMemory:
			items, err := svc.MemoryList(ctx, "")
			if err != nil {
				return errMsg{err}
			}
			return memoryMsg{items}
		case viewHealth:
			res, err := svc.Doctor(ctx, app.DoctorRequest{})
			if err != nil {
				return errMsg{err}
			}
			return healthMsg{res}
		}
		return nil
	}
}

func (m Model) loadStatus() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := svc.Status(ctx)
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{res}
	}
}

// Update is the Bubble Tea event loop. Overlays steal keys; mutations always
// confirm before calling Service.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case readyMsg:
		m.loading = false
		m.ready = msg.res
		m.rows = readyRows(msg.res, m.filter)
		m.clampCursor()
		m.lastRefresh = app.Now()
		return m, m.previewCmd()
	case focusMsg:
		m.loading = false
		m.focus = msg.res
		m.rows = focusRows(msg.res, m.filter)
		m.clampCursor()
		return m, m.previewCmd()
	case blockedMsg:
		m.loading = false
		m.blocked = msg.res
		m.rows = blockedRows(msg.res, m.filter)
		m.clampCursor()
		return m, m.previewCmd()
	case issuesMsg:
		m.loading = false
		m.issues = msg.issues
		m.rows = issueRows(msg.issues, m.filter)
		m.clampCursor()
		return m, m.previewCmd()
	case showMsg:
		m.loading = false
		m.detail = msg.res
		m.selectedID = msg.res.Detail.ID
	case graphMsg:
		m.loading = false
		m.graph = msg.g
	case activityMsg:
		m.loading = false
		m.activity = msg.res
		m.rows = activityRows(msg.res, m.filter)
		m.clampCursor()
	case memoryMsg:
		m.loading = false
		m.memories = msg.items
		m.rows = memoryRows(msg.items, m.filter)
		m.clampCursor()
	case healthMsg:
		m.loading = false
		m.health = msg.res
	case statusMsg:
		m.counts = msg.res
	case mutatedMsg:
		m.loading = false
		m.overlay = overlayNone
		m.status = msg.res.Message
		if msg.res.Issue.ID != "" {
			m.selectedID = msg.res.Issue.ID
		}
		return m, tea.Batch(m.loadView(), m.loadStatus())
	case errMsg:
		m.loading = false
		m.err = msg.err
		m.status = msg.err.Error()
	}
	return m, nil
}

func (m *Model) clampCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if n := len(m.visibleRows()); n == 0 {
		m.cursor = 0
		return
	} else if m.cursor >= n {
		m.cursor = n - 1
	}
}

func (m Model) visibleRows() []listRow {
	if m.filter == "" {
		return m.rows
	}
	status, query := app.ParseListFilter(m.filter)
	q := strings.ToLower(query)
	var out []listRow
	for _, r := range m.rows {
		if status != "" && !strings.EqualFold(string(r.Issue.Status), status) {
			continue
		}
		if q == "" {
			out = append(out, r)
			continue
		}
		if strings.Contains(strings.ToLower(r.ID), q) || strings.Contains(strings.ToLower(r.Title), q) || strings.Contains(strings.ToLower(r.Meta), q) || strings.Contains(strings.ToLower(string(r.Issue.Status)), q) {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) wide() bool {
	return m.width >= 100 && (m.view == viewReady || m.view == viewFocus || m.view == viewBlocked || m.view == viewIssues || m.view == viewActivity || m.view == viewMemory)
}

func (m Model) previewCmd() tea.Cmd {
	if !m.wide() {
		return nil
	}
	row, ok := m.currentRow()
	if !ok || row.ID == "" {
		return nil
	}
	if m.detail != nil && m.detail.Detail.ID == row.ID {
		return nil
	}
	svc := m.svc
	id := row.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := svc.Show(ctx, id, app.ShowRequest{})
		if err != nil {
			return nil
		}
		return showMsg{res}
	}
}

func (m Model) currentRow() (listRow, bool) {
	rows := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return listRow{}, false
	}
	return rows[m.cursor], true
}

func readyRows(res *app.ReadyResult, filter string) []listRow {
	if res == nil {
		return nil
	}
	out := make([]listRow, 0, len(res.Issues))
	for _, item := range res.Issues {
		out = append(out, listRow{
			ID:    item.Issue.ID,
			Title: item.Issue.Title,
			Meta:  fmt.Sprintf("+%d  %s", item.Downstream, output.RelativeTime(item.Issue.Age(app.Now()))),
			Issue: item.Issue,
		})
	}
	return out
}

func issueRows(issues []domain.Issue, _ string) []listRow {
	out := make([]listRow, 0, len(issues))
	for _, i := range issues {
		out = append(out, listRow{
			ID:    i.ID,
			Title: i.Title,
			Meta:  fmt.Sprintf("%s  %s", i.Priority.Label(), i.Status),
			Issue: i,
		})
	}
	return out
}

func focusRows(res *app.FocusReport, _ string) []listRow {
	if res == nil {
		return nil
	}
	var out []listRow
	add := func(label string, issues []domain.Issue) {
		for _, i := range issues {
			out = append(out, listRow{ID: i.ID, Title: i.Title, Meta: label + "  " + i.Priority.Label(), Issue: i})
		}
	}
	add("active", res.Active)
	add("touched", res.RecentlyTouched)
	add("unblocked", res.RecentlyUnblocked)
	add("attention", res.NeedsAttention)
	return out
}

func blockedRows(res *app.BlockedResult, _ string) []listRow {
	if res == nil {
		return nil
	}
	var out []listRow
	for _, g := range res.Groups {
		for _, i := range g.Issues {
			out = append(out, listRow{
				ID: i.ID, Title: i.Title,
				Meta:  "blocked by " + g.Blocker.ID,
				Issue: i,
			})
		}
	}
	return out
}

func activityRows(res *app.ActivityResult, _ string) []listRow {
	if res == nil {
		return nil
	}
	var out []listRow
	for _, ev := range res.Events {
		id := ""
		if ev.IssueID != nil {
			id = *ev.IssueID
		}
		out = append(out, listRow{ID: id, Title: ev.Summary, Meta: string(ev.Kind)})
	}
	return out
}

func memoryRows(items []domain.Memory, _ string) []listRow {
	var out []listRow
	for _, mem := range items {
		out = append(out, listRow{ID: mem.ID, Title: mem.Content, Meta: "memory"})
	}
	return out
}
