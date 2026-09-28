// lb-17y
package beads

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeVersionedFixtures(t *testing.T) {
	// lb-4gm.1
	for _, version := range []string{"bd-1.0.5", "bd-1.3.0"} {
		t.Run(version, func(t *testing.T) { decodeFixtureDir(t, filepath.Join("testdata", version)) })
	}
}

func decodeFixtureDir(t *testing.T, root string) {
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
				if name == "ready-nonempty.json" && (len(issues) != 1 || (issues[0].ID != "lb-1td" && issues[0].ID != "fx-2nl")) {
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

// lb-4gm.1
func TestDecodeCyclesAcrossVersions(t *testing.T) {
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cycles, err := decodeCycles(read("cycles.json"))
	if err != nil {
		t.Fatalf("decodeCycles(1.3.0): %v", err)
	}
	want := [][]string{{"fx-1w1", "fx-2ah"}, {"fx-3aa", "fx-4bb", "fx-5cc"}}
	if !reflect.DeepEqual(cycles, want) {
		t.Fatalf("cycles = %v, want %v", cycles, want)
	}
	if cycles, err := decodeCycles(read("cycles-empty.json")); err != nil || len(cycles) != 0 {
		t.Fatalf("empty cycles = %v, %v", cycles, err)
	}
	// Shapes emitted before 1.3.0 still decode.
	for _, legacy := range []string{`[["a","b"]]`, `[{"cycle":["a","b"]}]`, `[{"path":["a","b"]}]`, `[{"ids":["a","b"]}]`} {
		cycles, err := decodeCycles([]byte(legacy))
		if err != nil || !reflect.DeepEqual(cycles, [][]string{{"a", "b"}}) {
			t.Errorf("decodeCycles(%s) = %v, %v", legacy, cycles, err)
		}
	}
	// An object shape lb does not know must fail loudly, not report "no cycles".
	if _, err := decodeCycles([]byte(`[{"nodes":["a","b"]}]`)); err == nil {
		t.Error("an unknown cycle shape must be a decode error")
	}
}

// lb-4gm.1
func TestPendingMigrationIsNotReportedAsOldBinary(t *testing.T) {
	stderr, err := os.ReadFile(filepath.Join("testdata", "bd-1.3.0", "pending-migration.stderr"))
	if err != nil {
		t.Fatal(err)
	}
	if kind := classifyStderr(string(stderr), 1, nil); kind != ErrSchemaMismatch {
		t.Fatalf("kind = %q, want %q", kind, ErrSchemaMismatch)
	}
	ce := &CommandError{Kind: ErrSchemaMismatch, ExitCode: 1, Stderr: string(stderr)}
	msg, hint := ce.Message(), ce.UserHint()
	if !strings.Contains(msg, "migration") {
		t.Errorf("message should name the pending migration: %q", msg)
	}
	if strings.Contains(hint, "Upgrade `bd`") || !strings.Contains(hint, "bd migrate") || !strings.Contains(hint, "bd bootstrap") {
		t.Errorf("hint should point at migrate/bootstrap, not an upgrade: %q", hint)
	}
	if migrationPending("Checked schema: no pending schema migrations") {
		t.Error("a no-op migration report is not a refusal")
	}
	// The plain "binary too old" case keeps its upgrade advice.
	old := &CommandError{Kind: ErrSchemaMismatch, Stderr: "Error: schema version 3 is newer than this binary supports"}
	if !strings.Contains(old.UserHint(), "Upgrade `bd`") {
		t.Errorf("old-binary hint = %q", old.UserHint())
	}
}
