// lb-wlu
package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/manpage"
	"github.com/lesliesrussell/lazybeads/internal/version"
)

// lb-b4p
func (rt *runtime) manCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "man",
		Short: "Print the lb(1) manual page",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := io.WriteString(rt.opts.Stdout, manpage.Roff(version.Version))
			return err
		},
	}
}
