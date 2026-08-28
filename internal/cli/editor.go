// lb-rc1
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/beads"
	"github.com/lesliesrussell/lazybeads/internal/domain"
	"github.com/lesliesrussell/lazybeads/internal/workspace"
)

func (rt *runtime) editInEditor(cmd *cobra.Command, id string) (beads.UpdateIssueInput, error) {
	detail, err := rt.svc.Show(cmd.Context(), id, app.ShowRequest{})
	if err != nil {
		return beads.UpdateIssueInput{}, err
	}
	editor := workspace.EditorCommand()
	if len(editor) == 0 {
		return beads.UpdateIssueInput{}, &app.UsageError{Message: "set $VISUAL or $EDITOR for --open-in-editor"}
	}
	f, err := os.CreateTemp("", "lb-edit-*.md")
	if err != nil {
		return beads.UpdateIssueInput{}, err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString(renderEditable(detail.Detail.Issue)); err != nil {
		_ = f.Close()
		return beads.UpdateIssueInput{}, err
	}
	if err := f.Close(); err != nil {
		return beads.UpdateIssueInput{}, err
	}
	c := exec.Command(editor[0], append(editor[1:], path)...)
	c.Stdin = rt.opts.Stdin
	c.Stdout = rt.opts.Stdout
	c.Stderr = rt.opts.Stderr
	if err := c.Run(); err != nil {
		return beads.UpdateIssueInput{}, fmt.Errorf("editor: %w", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return beads.UpdateIssueInput{}, err
	}
	return parseEditable(detail.Detail.Issue, string(body))
}

func renderEditable(issue domain.Issue) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", issue.ID)
	fmt.Fprintf(&b, "title: %s\n", issue.Title)
	fmt.Fprintf(&b, "type: %s\n", issue.Type)
	fmt.Fprintf(&b, "priority: %d\n", int(issue.Priority))
	fmt.Fprintf(&b, "status: %s\n", issue.Status)
	assignee := ""
	if issue.Assignee != nil {
		assignee = issue.Assignee.String()
	}
	fmt.Fprintf(&b, "assignee: %s\n", assignee)
	b.WriteString("labels:\n")
	if len(issue.Labels) == 0 {
		b.WriteString("  []\n")
	} else {
		for _, l := range issue.Labels {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(issue.Description)
	if issue.Description != "" && !strings.HasSuffix(issue.Description, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func parseEditable(original domain.Issue, text string) (beads.UpdateIssueInput, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(text, "---") {
		return beads.UpdateIssueInput{}, &app.UsageError{Message: "edited file must start with YAML frontmatter"}
	}
	rest := strings.TrimPrefix(text, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return beads.UpdateIssueInput{}, &app.UsageError{Message: "edited file is missing the closing ---"}
	}
	front := rest[:end]
	body := strings.TrimPrefix(rest[end+4:], "\n")

	in := beads.UpdateIssueInput{}
	var labels []string
	var sawLabels bool
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "- ") && sawLabels {
			labels = append(labels, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- ")))
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "title":
			if val != original.Title {
				in.Title = &val
			}
		case "type":
			if val != string(original.Type) {
				in.Type = &val
			}
		case "priority":
			n, err := strconv.Atoi(val)
			if err != nil {
				return beads.UpdateIssueInput{}, &app.UsageError{Message: "priority must be an integer"}
			}
			if domain.Priority(n) != original.Priority {
				in.Priority = &n
			}
		case "assignee":
			cur := ""
			if original.Assignee != nil {
				cur = original.Assignee.String()
			}
			if val != cur {
				in.Assignee = &val
			}
		case "labels":
			sawLabels = true
			if val != "" && val != "[]" {
				labels = append(labels, val)
			}
		}
	}
	if sawLabels {
		in.SetLabels = labels
	}
	if strings.TrimSpace(body) != strings.TrimSpace(original.Description) {
		in.Description = &body
	}
	return in, nil
}
