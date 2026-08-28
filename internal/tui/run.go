// lb-0vu
package tui

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lesliesrussell/lazybeads/internal/app"
)

// Run starts the Bubble Tea program against an already-resolved Service.
func Run(svc *app.Service, opts Options, in io.Reader, out io.Writer) error {
	m := New(svc, opts)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// RunStdio is the production entry used by `lb` / `lb tui`.
func RunStdio(svc *app.Service, opts Options) error {
	return Run(svc, opts, os.Stdin, os.Stdout)
}
