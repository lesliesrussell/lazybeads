// lb-0vu
package cli

import (
	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/tui"
)

func (rt *runtime) tuiCmd() *cobra.Command {
	var view, issue string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI (Bubble Tea)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rt.runTUI(view, issue)
		},
	}
	cmd.Flags().StringVar(&view, "view", "ready", "initial view: ready, focus, blocked, issues, activity, memory, health")
	cmd.Flags().StringVar(&issue, "issue", "", "open this issue in the detail view")
	return cmd
}

func (rt *runtime) runTUI(view, issue string) error {
	return tui.Run(rt.svc, tui.Options{
		ASCII: rt.ascii,
		Color: rt.out != nil && rt.out.ColorEnabled(),
		View:  view,
		Issue: issue,
	}, rt.opts.Stdin, rt.opts.Stdout)
}
