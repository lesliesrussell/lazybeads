// lb-wlu
package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/lesliesrussell/lazybeads/internal/version"
)

func (rt *runtime) manCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "man",
		Short: "Print the lb(1) manual page",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			header := &doc.GenManHeader{
				Title:   "LB",
				Section: "1",
				Source:  "LazyBeads " + version.Version,
				Manual:  "LazyBeads Manual",
			}
			return doc.GenMan(cmd.Root(), header, rt.opts.Stdout)
		},
	}
}
