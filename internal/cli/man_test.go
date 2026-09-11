// lb-b4p
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/lesliesrussell/lazybeads/internal/manpage"
)

// manText renders the page and undoes the roff escaping that would otherwise
// hide flag names from a substring search.
func manText(t *testing.T) string {
	t.Helper()
	page := manpage.Roff("test")
	if !strings.HasPrefix(page, ".\\\"") {
		t.Fatalf("man page does not start with a roff comment: %.40q", page)
	}
	if strings.Contains(page, "@VERSION@") {
		t.Error("version placeholder was not substituted")
	}
	return strings.ReplaceAll(page, `\-`, "-")
}

// defines reports whether the page carries a real definition of term: a bold
// macro line naming it, not a passing mention in prose or an example. Without
// this, deleting a command's entry would still pass so long as EXAMPLES
// happened to use it.
func defines(page, term string) bool {
	for _, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, ".B") {
			continue
		}
		body := strings.ReplaceAll(line, `"`, "")
		i := strings.Index(body, term)
		if i < 0 {
			continue
		}
		rest := body[i+len(term):]
		if rest == "" {
			return true
		}
		switch c := rest[0]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			continue // a longer name, such as --label-any when seeking --label
		}
		return true
	}
	return false
}

// walk yields every command the operator can actually type, skipping cobra's
// generated help command and the per-shell children of `completion`, which the
// page documents as one alternation rather than four entries.
func walk(cmd *cobra.Command, path string, fn func(path string, c *cobra.Command)) {
	for _, sub := range cmd.Commands() {
		if sub.Name() == "help" || sub.Hidden {
			continue
		}
		p := path + " " + sub.Name()
		fn(p, sub)
		if sub.Name() == "completion" {
			continue
		}
		walk(sub, p, fn)
	}
}

func TestManPageDocumentsEveryCommand(t *testing.T) {
	page := manText(t)
	rt := &runtime{}
	walk(rt.root(), "lb", func(path string, _ *cobra.Command) {
		if !defines(page, path) {
			t.Errorf("man page does not document %q", path)
		}
	})
}

func TestManPageDocumentsEveryFlag(t *testing.T) {
	page := manText(t)
	rt := &runtime{}
	root := rt.root()

	check := func(where string, f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if !defines(page, "--"+f.Name) {
			t.Errorf("man page does not document --%s (%s)", f.Name, where)
		}
	}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { check("global", f) })
	root.LocalFlags().VisitAll(func(f *pflag.Flag) { check("lb", f) })
	walk(root, "lb", func(path string, c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) { check(path, f) })
	})
}

func TestManPageDocumentsExitCodes(t *testing.T) {
	page := manText(t)
	if !strings.Contains(page, ".SH EXIT STATUS") {
		t.Fatal("man page has no EXIT STATUS section")
	}
	section := page[strings.Index(page, ".SH EXIT STATUS"):]
	for _, code := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		if !strings.Contains(section, ".B "+code+"\n") {
			t.Errorf("exit code %s is not documented", code)
		}
	}
}

func TestManPageDocumentsEnvironmentAndConfig(t *testing.T) {
	page := manText(t)
	for _, want := range []string{
		"LB_ACTOR", "LB_BD_BIN", "LB_COLOR", "LB_PROJECT", "LB_BEADS_DIR",
		"LB_RIG", "LB_TIMEOUT", "LB_NO_CONFIRM", "LB_CONFIG", "LB_CACHE_DIR",
		"LB_DATA_DIR", "VISUAL", "EDITOR",
		"[general]", "[workspace]", "[ranking]", "[tui]", "[keys]", "[aliases]",
		".lazybeads.toml",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("man page does not mention %q", want)
		}
	}
}

func TestManCommandPrintsThePage(t *testing.T) {
	var out strings.Builder
	code := Execute(Options{Args: []string{"man"}, Stdout: &out, Stderr: &strings.Builder{}})
	if code != 0 {
		t.Fatalf("lb man exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), ".TH LB 1") {
		t.Error("lb man did not emit the manual page header")
	}
}
