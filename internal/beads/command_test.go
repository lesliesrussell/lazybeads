// lb-1td
package beads

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRedactArgs(t *testing.T) {
	got := redactArgs([]string{"list", "--token", "hunter2", "--password=s3cret", "--limit", "10"})
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "s3cret") {
		t.Errorf("secrets survived redaction: %q", joined)
	}
	if !strings.Contains(joined, "--limit 10") {
		t.Errorf("non-secret args should be preserved: %q", joined)
	}
}

func TestRedactEnvKeepsNames(t *testing.T) {
	got := redactEnv([]string{"PATH=/usr/bin", "BEADS_DOLT_PASSWORD=hunter2", "GH_TOKEN=abc"})
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "abc") {
		t.Errorf("secret values survived: %q", joined)
	}
	if !strings.Contains(joined, "BEADS_DOLT_PASSWORD=<redacted>") {
		t.Errorf("variable names should remain visible: %q", joined)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Errorf("ordinary variables should be intact: %q", joined)
	}
}

func TestBoundedBufferStopsAtLimit(t *testing.T) {
	b := &boundedBuffer{limit: 10}
	n, err := b.Write([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	// The writer must report a full write so the child process is not blocked.
	if n != 16 {
		t.Errorf("Write reported %d, want 16", n)
	}
	if len(b.Bytes()) != 10 {
		t.Errorf("captured %d bytes, want the 10-byte limit", len(b.Bytes()))
	}
	if !b.overflow {
		t.Error("overflow should be recorded")
	}
}

func TestMissingBinaryIsTypedError(t *testing.T) {
	r := NewRunner("definitely-not-a-real-binary-xyzzy")
	_, err := r.Run(context.Background(), "probe", Scope{})
	ce, ok := AsCommandError(err)
	if !ok {
		t.Fatalf("expected CommandError, got %T", err)
	}
	if ce.Kind != ErrBDBinaryMissing {
		t.Errorf("kind = %q, want %q", ce.Kind, ErrBDBinaryMissing)
	}
	if ce.ExitCode2() != 3 {
		t.Errorf("exit code = %d, want 3", ce.ExitCode2())
	}
	if !strings.Contains(ce.UserHint(), "LB_BD_BIN") {
		t.Errorf("hint should mention the override: %q", ce.UserHint())
	}
}

// TestNoShellInterpolation proves argv execution: a shell metacharacter in an
// argument is passed through literally rather than being interpreted.
func TestNoShellInterpolation(t *testing.T) {
	r := NewRunner("echo")
	res, err := r.Run(context.Background(), "probe", Scope{Timeout: 5 * time.Second},
		"hello; touch /tmp/lazybeads-should-not-exist", "$(whoami)", "`id`")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := string(res.Stdout)
	if !strings.Contains(out, "$(whoami)") || !strings.Contains(out, "`id`") {
		t.Errorf("arguments were interpreted rather than passed literally: %q", out)
	}
}

func TestScopeArgsOnlySetWhenSelected(t *testing.T) {
	if got := scopeArgs(Scope{}); len(got) != 0 {
		t.Errorf("an empty scope must let bd resolve on its own, got %v", got)
	}
	got := scopeArgs(Scope{Project: "/tmp/x"})
	if len(got) != 2 || got[0] != "--directory" || got[1] != "/tmp/x" {
		t.Errorf("scopeArgs = %v", got)
	}
}

func TestCommandEnvSetsBeadsDirOnlyWhenSelected(t *testing.T) {
	env := strings.Join(commandEnv(Scope{}), " ")
	if strings.Contains(env, "BEADS_DIR=") {
		t.Error("BEADS_DIR must not be invented when the user did not select one")
	}
	env = strings.Join(commandEnv(Scope{BeadsDir: "/tmp/p/.beads"}), " ")
	if !strings.Contains(env, "BEADS_DIR=/tmp/p/.beads") {
		t.Error("an explicit BeadsDir should be exported")
	}
}

func TestClassifyStderr(t *testing.T) {
	cases := []struct {
		stderr string
		want   ErrorKind
	}{
		{"Error: schema version 3 is newer than this binary supports", ErrSchemaMismatch},
		{"no beads database found in this directory", ErrWorkspaceNotFound},
		{"Error: issue lb-zzz not found", ErrNotFound},
		{"unknown command \"teleport\" for \"bd\"", ErrUnsupported},
		{"adding this would create a cycle", ErrConflict},
		{"Error: title cannot be empty", ErrValidation},
		{"something unexpected exploded", ErrBDExecution},
	}
	for _, c := range cases {
		if got := classifyStderr(c.stderr, 1, nil); got != c.want {
			t.Errorf("classifyStderr(%q) = %q, want %q", c.stderr, got, c.want)
		}
	}
}

func TestTimeoutClassification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r := NewRunner("sleep")
	_, err := r.Run(ctx, "probe", Scope{Timeout: 30 * time.Millisecond}, "5")
	ce, ok := AsCommandError(err)
	if !ok {
		t.Fatalf("expected CommandError, got %T: %v", err, err)
	}
	if ce.Kind != ErrTimeout && ce.Kind != ErrCancelled {
		t.Errorf("kind = %q, want a timeout or cancellation", ce.Kind)
	}
	if ce.ExitCode2() != 9 {
		t.Errorf("exit code = %d, want 9", ce.ExitCode2())
	}
}
