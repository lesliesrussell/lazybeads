// lb-58x
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/output"
	"github.com/lesliesrussell/lazybeads/internal/version"
)

func (rt *runtime) emit(command string, data any, warnings []string) error {
	switch rt.format {
	case output.FormatJSON:
		return rt.out.EmitJSON(command, rt.svc.WorkspaceRef(), data, warnings)
	case output.FormatJSONL:
		switch v := data.(type) {
		case *app.ReadyResult:
			return rt.out.EmitJSONL(output.IssuesAsAny(v.Issues))
		case *app.ListResult:
			return rt.out.EmitJSONL(output.IssuesAsAny(v.Issues))
		case *app.SearchResult:
			return rt.out.EmitJSONL(output.IssuesAsAny(v.Issues))
		default:
			return rt.out.EmitJSONL([]any{data})
		}
	}
	return nil
}

func renderStatus(w *output.Writer, report *app.StatusReport) {
	bd := report.BDVersion
	if bd == "" {
		bd = "unknown"
	}
	w.Print(fmt.Sprintf("lazybeads %s · Beads %s · %s",
		version.Version, bd, output.SanitizeLine(report.Workspace.RootPath)))
	w.Blank()
	w.Print(w.Style(output.StyleBold, "Workspace"))
	sync := "unknown"
	w.Print("  Storage: " + report.Storage + " · sync: " + sync)
	actor := report.Actor
	if actor == "" {
		actor = "(unconfigured)"
	}
	w.Print("  Actor:   " + output.SanitizeLine(actor))
	updated := "unknown"
	if report.UpdatedAt != nil {
		updated = output.Ago(report.UpdatedAt, report.GeneratedAt)
	}
	w.Print("  Updated: " + updated)
	w.Blank()
	w.Print(w.Style(output.StyleBold, "Work"))
	w.Print(fmt.Sprintf("  Ready:       %d", report.Counts.Ready))
	w.Print(fmt.Sprintf("  In progress: %d", report.Counts.InProgress))
	w.Print(fmt.Sprintf("  Blocked:     %d", report.Counts.Blocked))
	w.Print(fmt.Sprintf("  Open:        %d", report.Counts.Open))
	w.Print(fmt.Sprintf("  Closed:      %d", report.Counts.Closed))
	if len(report.Attention) > 0 {
		w.Blank()
		w.Print(w.Style(output.StyleBold, "Attention"))
		for _, item := range report.Attention {
			w.Print("  " + w.Symbol(output.SymWarning) + " " + item.Summary)
		}
	}
	w.Blank()
	w.Note("Run `lb next` for a recommended task.")
}

func renderReady(w *output.Writer, result *app.ReadyResult, now time.Time) {
	n := result.Total
	noun := "task"
	if n != 1 {
		noun = "tasks"
	}
	w.Print(fmt.Sprintf("%s · %d %s", w.Heading("Ready"), n, noun))
	w.Blank()
	if n == 0 {
		w.Print("No claimable work is currently available.")
		return
	}
	issues := make([]domain.Issue, 0, len(result.Issues))
	for _, item := range result.Issues {
		issues = append(issues, item.Issue)
	}
	idWidth := output.IDWidth(issues)
	for _, item := range result.Issues {
		w.Print(w.IssueLine(item.Issue, now, idWidth))
	}
}

func renderShow(w *output.Writer, result *app.ShowResult, now time.Time, events bool) {
	d := result.Detail
	w.Print(w.PriorityLabel(d.Priority) + "  " + w.Style(output.StyleID, output.SanitizeLine(d.ID)))
	w.Print(w.Style(output.StyleTitle, output.SanitizeLine(d.Title)))
	w.Blank()
	w.Print(w.Field("Status", w.StatusLabel(d.Status)))
	w.Print(w.Field("Type", string(d.Type)))
	assignee := "(unclaimed)"
	if d.Assignee != nil && d.Assignee.String() != "" {
		assignee = output.SanitizeLine(d.Assignee.String())
	}
	w.Print(w.Field("Assignee", assignee))
	if d.ParentID != nil {
		w.Print(w.Field("Parent", output.SanitizeLine(*d.ParentID)))
	}
	if len(d.Labels) > 0 {
		w.Print(w.Field("Labels", output.SanitizeLine(strings.Join(d.Labels, ", "))))
	}
	w.Print(w.Field("Created", output.Ago(d.CreatedAt, now)))
	w.Print(w.Field("Updated", output.Ago(d.UpdatedAt, now)))
	if d.CloseReason != nil {
		w.Print(w.Field("Closed", output.SanitizeLine(*d.CloseReason)))
	}
	if desc := strings.TrimSpace(d.Description); desc != "" {
		w.Blank()
		w.Print(w.Heading("Description"))
		w.Print(output.Wrap(output.Sanitize(desc), w.Width()))
	}
	w.Blank()
	w.Print(w.Heading("Blockers"))
	blockers := d.Blockers()
	if len(blockers) == 0 {
		w.Print("  none")
	} else {
		for _, b := range blockers {
			w.Print("  " + w.Style(output.StyleID, output.SanitizeLine(b.Issue.ID)) + "  " + output.SanitizeLine(b.Issue.Title))
		}
	}
	w.Print(w.Heading("Dependents"))
	if len(d.Dependents) == 0 {
		w.Print("  none")
	} else {
		for _, dep := range d.Dependents {
			w.Print("  " + w.Style(output.StyleID, output.SanitizeLine(dep.Issue.ID)) + "  " + output.SanitizeLine(dep.Issue.Title))
		}
	}
	if events {
		w.Blank()
		w.Print(w.Heading("Events"))
		if len(result.Events) == 0 {
			w.Print("  none")
		} else {
			for _, ev := range result.Events {
				w.Print("  " + output.SanitizeLine(ev.Summary))
			}
		}
	}
	w.NextSteps(result.Suggested...)
}

func renderIssueTable(w *output.Writer, heading string, issues []domain.Issue, extra string) {
	n := len(issues)
	noun := "issue"
	if n != 1 {
		noun = "issues"
	}
	line := fmt.Sprintf("%s · %d %s", w.Heading(heading), n, noun)
	if extra != "" {
		line += " · " + extra
	}
	w.Print(line)
	w.Blank()
	if n == 0 {
		w.Print("No matching issues.")
		return
	}
	idWidth := output.IDWidth(issues)
	table := w.NewTable(
		output.Column{Header: "P", Width: 3, Style: output.StyleNone},
		output.Column{Header: "ID", Width: idWidth, Style: output.StyleID},
		output.Column{Header: "Status", Width: 12},
		output.Column{Header: "Title", Width: 0},
	)
	for _, issue := range issues {
		table.AddRow(
			issue.Priority.Label(),
			output.SanitizeLine(issue.ID),
			string(issue.Status),
			output.SanitizeLine(issue.Title),
		)
	}
	table.Render(false)
}
