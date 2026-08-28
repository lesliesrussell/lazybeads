// lb-rc1
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/lesliesrussell/lazybeads/internal/app"
	"github.com/lesliesrussell/lazybeads/internal/config"
	"github.com/lesliesrussell/lazybeads/internal/output"
)

func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func isInteractiveWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func (rt *runtime) confirm(plan *app.MutationResult) error {
	if rt.dryRun {
		return nil
	}
	if rt.yes {
		return nil
	}
	if config.NoConfirmFromEnv() || (rt.cfg.General.ConfirmMutations != nil && !*rt.cfg.General.ConfirmMutations) {
		if isInteractive(rt.opts.Stdin) && rt.out != nil {
			rt.out.Warn("confirmation disabled by LB_NO_CONFIRM; mutation will proceed")
		}
		return nil
	}
	if !isInteractive(rt.opts.Stdin) {
		return &app.UsageError{Message: "refusing to mutate without --yes in non-interactive mode"}
	}

	w := rt.opts.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, plan.Message+"?")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Workspace: %s\n", rt.svc.Workspace.RootPath)
	if plan.Issue.ID != "" || (plan.Before != nil && plan.Before.ID != "") {
		issue := plan.Issue
		if plan.Before != nil {
			issue = *plan.Before
		}
		fmt.Fprintf(w, "  Issue:     %s — %s\n", issue.ID, output.SanitizeLine(issue.Title))
	}
	if len(plan.Argv) > 0 {
		fmt.Fprintf(w, "  Command:   %s\n", strings.Join(plan.Argv, " "))
	}
	for _, warn := range plan.Warnings {
		fmt.Fprintf(w, "  Warning:   %s\n", warn)
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, "[y] Confirm  [n] Cancel  [v] View raw command: ")

	reader := bufio.NewReader(rt.opts.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && len(strings.TrimSpace(line)) == 0 {
			return &app.DeclinedError{}
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return nil
		case "n", "no", "q", "":
			return &app.DeclinedError{}
		case "v":
			fmt.Fprintln(w, strings.Join(plan.Argv, " "))
			fmt.Fprint(w, "[y] Confirm  [n] Cancel  [v] View raw command: ")
		default:
			fmt.Fprint(w, "[y] Confirm  [n] Cancel  [v] View raw command: ")
		}
	}
}

func (rt *runtime) runMutation(plan func(dry bool) (*app.MutationResult, error)) error {
	preview, err := plan(true)
	if err != nil {
		return err
	}
	if rt.dryRun {
		return rt.emitMutation(preview)
	}
	if err := rt.confirm(preview); err != nil {
		return err
	}
	result, err := plan(false)
	if err != nil {
		return err
	}
	return rt.emitMutation(result)
}

func (rt *runtime) optsMutate() app.MutateOptions {
	return app.MutateOptions{Actor: rt.actor}
}

func (rt *runtime) emitMutation(result *app.MutationResult) error {
	if rt.format != output.FormatHuman {
		return rt.emit(result.Action, result, result.Warnings)
	}
	renderMutation(rt.out, result)
	return nil
}
