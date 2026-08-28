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
		case *app.MutationResult:
			return rt.out.EmitJSONL([]any{v})
		case *app.NextResult:
			items := []any{}
			if v.Recommendation != nil {
				items = append(items, v.Recommendation)
			}
			for _, alt := range v.Alternatives {
				items = append(items, alt)
			}
			return rt.out.EmitJSONL(items)
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

// lb-rd7
func renderNext(w *output.Writer, result *app.NextResult, now time.Time, verbose bool) {
	if result.Recommendation == nil {
		w.Print("No claimable work is currently available.")
		w.Blank()
		if result.BlockedCount > 0 {
			w.Print(fmt.Sprintf("Open tasks are blocked by %d unresolved issue%s.", result.BlockedCount, pluralNoun(result.BlockedCount)))
			w.Print("Run `lb blocked` to inspect them.")
		}
		return
	}
	rec := result.Recommendation
	issue := rec.Issue
	w.Print(w.Style(output.StyleBold, "Recommended next task"))
	w.Blank()
	w.Print(w.PriorityLabel(issue.Priority) + "  " + w.Style(output.StyleID, output.SanitizeLine(issue.ID)) + "  " + output.SanitizeLine(issue.Title))
	claim := "unclaimed"
	if issue.Assignee != nil && issue.Assignee.String() != "" {
		claim = "claimed by " + output.SanitizeLine(issue.Assignee.String())
	}
	w.Print("Status: ready · " + claim + " · type: " + string(issue.Type))
	w.Print("Reason: " + reasonFromFactors(rec.Factors))
	w.Print("Age: " + output.RelativeTime(issue.Age(now)))
	if verbose {
		w.Blank()
		w.Print(w.Style(output.StyleDim, fmt.Sprintf("strategy %s · score %.1f", rec.Strategy, rec.Score)))
		for _, f := range rec.Factors {
			w.Print(fmt.Sprintf("  %s  value %.1f × %.0f = %.1f  %s", f.Kind, f.Value, f.Weight, f.Contribution, f.Explanation))
		}
	}
	if len(result.Alternatives) > 0 {
		w.Blank()
		w.Print(w.Style(output.StyleBold, "Also consider"))
		for _, alt := range result.Alternatives {
			w.Print("  " + w.PriorityLabel(alt.Issue.Priority) + "  " + w.Style(output.StyleID, output.SanitizeLine(alt.Issue.ID)) + "  " + output.SanitizeLine(alt.Issue.Title))
		}
	}
	steps := []string{"lb show " + issue.ID, "lb why " + issue.ID}
	if issue.Assignee == nil || issue.Assignee.String() == "" {
		steps = append([]string{"lb claim " + issue.ID}, steps...)
	}
	w.NextSteps(steps...)
}

func reasonFromFactors(factors []app.Factor) string {
	parts := make([]string, 0, len(factors))
	for _, f := range factors {
		if f.Explanation == "" {
			continue
		}
		parts = append(parts, strings.TrimSuffix(f.Explanation, "."))
	}
	if len(parts) == 0 {
		return "highest ranked ready work"
	}
	return strings.Join(parts, "; ") + "."
}

func pluralNoun(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func renderMutation(w *output.Writer, result *app.MutationResult) {
	if result.DryRun {
		w.Print("Dry run: would execute")
		w.Print("  " + strings.Join(result.Argv, " "))
		for _, warn := range result.Warnings {
			w.Warn(warn)
		}
		return
	}
	w.Print(result.Message + ".")
	if result.Issue.ID != "" {
		w.Blank()
		w.Print(w.PriorityLabel(result.Issue.Priority) + "  " + w.Style(output.StyleID, output.SanitizeLine(result.Issue.ID)) + "  " + output.SanitizeLine(result.Issue.Title))
		w.Print(w.StatusLabel(result.Issue.Status))
		if result.Issue.Assignee != nil && result.Issue.Assignee.String() != "" {
			w.Print("Assignee: " + output.SanitizeLine(result.Issue.Assignee.String()))
		}
	}
	for _, warn := range result.Warnings {
		w.Warn(warn)
	}
	if len(result.NewlyReady) > 0 {
		w.Blank()
		w.Print(w.Style(output.StyleBold, "Newly ready"))
		for _, i := range result.NewlyReady {
			w.Print("  " + w.PriorityLabel(i.Priority) + " " + w.Style(output.StyleID, output.SanitizeLine(i.ID)) + " · " + output.SanitizeLine(i.Title))
		}
	}
}

func renderDepList(w *output.Writer, id string, blockers, dependents []domain.Dependency) {
	w.Print(w.Style(output.StyleBold, "Dependencies for "+output.SanitizeLine(id)))
	w.Blank()
	w.Print(w.Heading("Blockers"))
	if len(blockers) == 0 {
		w.Print("  none")
	} else {
		for _, d := range blockers {
			w.Print("  " + string(d.Type) + "  " + w.Style(output.StyleID, output.SanitizeLine(d.Issue.ID)) + "  " + output.SanitizeLine(d.Issue.Title))
		}
	}
	w.Print(w.Heading("Dependents"))
	if len(dependents) == 0 {
		w.Print("  none")
	} else {
		for _, d := range dependents {
			w.Print("  " + string(d.Type) + "  " + w.Style(output.StyleID, output.SanitizeLine(d.Issue.ID)) + "  " + output.SanitizeLine(d.Issue.Title))
		}
	}
}

func renderFocus(w *output.Writer, report *app.FocusReport, now time.Time) {
	if !report.ActorConfigured {
		w.Note("Actor is unconfigured. Set LB_ACTOR or --actor.")
		w.Blank()
	}
	section := func(title string, issues []domain.Issue) {
		w.Print(w.Heading(title))
		if len(issues) == 0 {
			w.Print("  none")
			w.Blank()
			return
		}
		idWidth := output.IDWidth(issues)
		for _, i := range issues {
			extra := output.Ago(i.UpdatedAt, now)
			w.Print("  " + w.IssueLine(i, now, idWidth) + "  " + w.Style(output.StyleDim, extra))
		}
		w.Blank()
	}
	section("My active work", report.Active)
	section("Recently touched", report.RecentlyTouched)
	section("Recently unblocked", report.RecentlyUnblocked)
	section("Needs attention", report.NeedsAttention)
}

func renderActivity(w *output.Writer, result *app.ActivityResult) {
	if len(result.Events) == 0 {
		w.Print("No recent activity.")
		return
	}
	for _, ev := range result.Events {
		id := ""
		if ev.IssueID != nil {
			id = *ev.IssueID + "  "
		}
		when := ev.Timestamp.UTC().Format("15:04")
		if !ev.Timestamp.IsZero() {
			when = output.RelativeTime(app.Now().Sub(ev.Timestamp))
		}
		w.Print(w.Style(output.StyleDim, when) + "  " + string(ev.Kind) + "  " + w.Style(output.StyleID, output.SanitizeLine(id)) + output.SanitizeLine(ev.Summary))
	}
}

func renderDoctor(w *output.Writer, report *app.DoctorReport, verbose bool) {
	w.Print(fmt.Sprintf("lazybeads %s · doctor", report.Version))
	w.Print("Health: " + w.HealthSymbol(report.Health.Status) + " " + string(report.Health.Status))
	w.Blank()
	for _, c := range report.Health.Checks {
		line := w.HealthSymbol(c.Level) + " " + c.Name + "  " + c.Summary
		w.Print(line)
		if verbose && c.Detail != "" {
			w.Print("    " + c.Detail)
		}
		if c.Hint != "" {
			w.Print("    " + w.Style(output.StyleDim, c.Hint))
		}
		for _, item := range c.Items {
			w.Print("    - " + item)
		}
	}
	if len(report.Fixes) > 0 {
		w.Blank()
		w.Print(w.Style(output.StyleBold, "Fixes"))
		for _, f := range report.Fixes {
			w.Print("  " + f)
		}
	}
}

func renderWhy(w *output.Writer, exp *app.Explanation) {
	if exp.Ready {
		w.Print(output.SanitizeLine(exp.IssueID) + " is ready.")
		w.Blank()
		w.Print("No open blockers were found.")
		if exp.TransitiveUnlocks > 0 {
			w.Print(fmt.Sprintf("It unlocks %d open task%s.", exp.TransitiveUnlocks, pluralNoun(exp.TransitiveUnlocks)))
		}
	} else if exp.Closed {
		w.Print(output.SanitizeLine(exp.IssueID) + " is closed.")
	} else {
		w.Print(output.SanitizeLine(exp.IssueID) + " is blocked.")
		w.Blank()
		w.Print(w.Heading("Immediate blocker"))
		if len(exp.ImmediateBlockers) == 0 {
			w.Print("  none")
		} else {
			for _, b := range exp.ImmediateBlockers {
				w.Print("  " + w.Style(output.StyleID, output.SanitizeLine(b.ID)) + " · " + output.SanitizeLine(b.Title))
				w.Print("    Status: " + b.Status)
			}
		}
		if len(exp.RootBlockers) > 0 {
			w.Blank()
			w.Print(w.Heading("Nearest currently actionable item"))
			b := exp.RootBlockers[0]
			w.Print("  " + w.Style(output.StyleID, output.SanitizeLine(b.ID)) + " · " + output.SanitizeLine(b.Title))
		}
	}
	for _, warn := range exp.Warnings {
		w.Warn(warn)
	}
}

func renderGraphTree(w *output.Writer, g *domain.IssueGraph) {
	root, ok := g.Nodes[g.RootID]
	if !ok {
		w.Print("empty graph")
		return
	}
	w.Print(fmt.Sprintf("%s · %s [%s]", root.ID, output.SanitizeLine(root.Title), root.Status))
	glyphs := w.Tree()
	edges := g.OutEdges(g.RootID)
	for i, e := range edges {
		last := i == len(edges)-1
		prefix := glyphs.Branch
		if last {
			prefix = glyphs.Last
		}
		node := g.Nodes[e.ToID]
		w.Print(prefix + string(e.Type) + "  " + node.ID + " · " + output.SanitizeLine(node.Title) + " [" + string(node.Status) + "]")
	}
	if g.Truncated && g.Truncation != nil {
		w.Note(g.Truncation.Reason)
	}
}

func graphDOT(g *domain.IssueGraph) string {
	var b strings.Builder
	b.WriteString("digraph beads {\n")
	for id, n := range g.Nodes {
		fmt.Fprintf(&b, "  %q [label=%q];\n", id, n.ID+" "+n.Title)
	}
	for _, e := range g.Edges {
		fmt.Fprintf(&b, "  %q -> %q [label=%q];\n", e.FromID, e.ToID, e.Type)
	}
	b.WriteString("}\n")
	return b.String()
}

func renderBlocked(w *output.Writer, result *app.BlockedResult) {
	w.Print(fmt.Sprintf("%s · %d %s", w.Heading("Blocked"), result.Total, map[bool]string{true: "task", false: "tasks"}[result.Total == 1]))
	w.Blank()
	if result.Total == 0 {
		w.Print("No blocked work.")
		return
	}
	for _, g := range result.Groups {
		w.Print("Blocked by " + w.Style(output.StyleID, output.SanitizeLine(g.Blocker.ID)) + " — " + output.SanitizeLine(g.Blocker.Title))
		for _, i := range g.Issues {
			w.Print("  " + w.PriorityLabel(i.Priority) + " " + w.Style(output.StyleID, output.SanitizeLine(i.ID)) + "  " + output.SanitizeLine(i.Title))
		}
		w.Blank()
	}
}

func renderDepValidate(w *output.Writer, report *app.DepReport) {
	if report.CycleCount == 0 {
		w.Print("No dependency cycles detected.")
		return
	}
	w.Print(fmt.Sprintf("%d dependency cycle(s)", report.CycleCount))
	for _, c := range report.Cycles {
		w.Print("  " + strings.Join(c, " -> "))
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
