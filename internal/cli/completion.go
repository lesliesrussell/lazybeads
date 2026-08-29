// lb-uvj
package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lesliesrussell/lazybeads/internal/config"
)

func writeUserCompletions(root *cobra.Command) ([]string, error) {
	data, err := config.UserDataDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(data, "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	type script struct {
		name string
		gen  func(*os.File) error
	}
	scripts := []script{
		{"lb.bash", func(f *os.File) error { return root.GenBashCompletionV2(f, true) }},
		{"_lb", func(f *os.File) error { return root.GenZshCompletion(f) }},
		{"lb.fish", func(f *os.File) error { return root.GenFishCompletion(f, true) }},
		{"lb.ps1", func(f *os.File) error { return root.GenPowerShellCompletionWithDesc(f) }},
	}
	var fixes []string
	for _, s := range scripts {
		path := filepath.Join(dir, s.name)
		f, err := os.Create(path)
		if err != nil {
			return fixes, err
		}
		err = s.gen(f)
		_ = f.Close()
		if err != nil {
			return fixes, err
		}
		fixes = append(fixes, "wrote "+path)
	}
	fixes = append(fixes, "source the script for your shell; LazyBeads does not edit rc files")
	return fixes, nil
}
