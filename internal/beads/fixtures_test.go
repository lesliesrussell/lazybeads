// lb-17y
package beads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeVersionedFixtures(t *testing.T) {
	root := filepath.Join("testdata", "bd-1.0.5")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			switch {
			case name == "version.json":
				if len(trimJSON(data)) == 0 {
					t.Fatal("version fixture is empty")
				}
			case name == "ready.json" || name == "ready-nonempty.json" || name == "list.json":
				issues, err := decodeIssues(data)
				if err != nil {
					t.Fatalf("decodeIssues: %v", err)
				}
				if name == "ready-nonempty.json" && (len(issues) != 1 || issues[0].ID != "lb-1td") {
					t.Fatalf("ready-nonempty = %+v", issues)
				}
			case strings.HasPrefix(name, "show-"):
				detail, err := decodeDetail(data)
				if err != nil {
					t.Fatalf("decodeDetail: %v", err)
				}
				if detail.ID == "" {
					t.Fatal("show fixture lost the issue id")
				}
				if name == "show-closed.json" && !detail.IsClosed() {
					t.Errorf("closed fixture status = %s", detail.Status)
				}
			}
		})
	}
}

func TestMalformedFixtureIsDecodeError(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "bd-1.0.5", "malformed-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeIssues(data); err == nil {
		t.Fatal("malformed output must not decode as issues")
	}
}

func FuzzDecodeIssues(f *testing.F) {
	root := filepath.Join("testdata", "bd-1.0.5")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if data, err := os.ReadFile(filepath.Join(root, e.Name())); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"schema_version":1}`))
	f.Add([]byte("Showing 2 issues\n[{invalid"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeIssues(data)
		_, _ = decodeDetail(data)
	})
}

func FuzzValidateTitle(f *testing.F) {
	f.Add("Add typed bd adapter")
	f.Add("")
	f.Add("a\x00b")
	f.Fuzz(func(t *testing.T, title string) {
		_ = ValidateTitle(title)
	})
}

func FuzzClaimArgs(f *testing.F) {
	f.Add("lb-1", "operator")
	f.Fuzz(func(t *testing.T, id, actor string) {
		if err := validateID(id); err != nil {
			return
		}
		args := ClaimArgs(id, actor)
		if len(args) < 3 || args[0] != "update" || args[1] != id {
			t.Fatalf("ClaimArgs(%q, %q) = %v", id, actor, args)
		}
		joined := strings.Join(args, "\x00")
		if strings.Contains(joined, "\n") {
			t.Fatalf("argv leaked a newline: %v", args)
		}
	})
}
