// lb-1td
package beads

import (
	"testing"
	"time"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

// readyFixture is a verbatim `bd ready --json` payload from bd 1.0.5.
const readyFixture = `[
  {
    "id": "lb-1td",
    "title": "Foundation",
    "status": "open",
    "priority": 0,
    "issue_type": "task",
    "owner": "operator@example.com",
    "created_at": "2026-08-28T14:16:58Z",
    "created_by": "Operator",
    "updated_at": "2026-08-28T14:16:58Z",
    "dependency_count": 0,
    "dependent_count": 1,
    "comment_count": 0
  }
]`

func TestDecodeIssuesArray(t *testing.T) {
	issues, err := decodeIssues([]byte(readyFixture))
	if err != nil {
		t.Fatalf("decodeIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	got := issues[0]
	if got.ID != "lb-1td" || got.Title != "Foundation" {
		t.Errorf("unexpected identity: %+v", got)
	}
	if got.Priority != 0 {
		t.Errorf("priority = %v, want 0", got.Priority)
	}
	if got.Type != domain.TypeTask {
		t.Errorf("type = %q, want task (from issue_type)", got.Type)
	}
	if got.Assignee == nil || got.Assignee.Email != "operator@example.com" {
		t.Errorf("owner should map onto assignee: %+v", got.Assignee)
	}
	if got.DependentCount != 1 {
		t.Errorf("dependent_count = %d, want 1", got.DependentCount)
	}
	if got.CreatedAt == nil {
		t.Fatal("created_at should decode")
	}
	if len(got.Raw) == 0 {
		t.Error("raw payload should be preserved")
	}
}

// TestDecodeShowIsArrayWrapped covers bd's habit of returning a single issue
// inside an array from `bd show --json`.
func TestDecodeShowIsArrayWrapped(t *testing.T) {
	const fixture = `[
  {
    "id": "lb-nvw",
    "title": "Adapter",
    "status": "open",
    "priority": 1,
    "issue_type": "task",
    "labels": ["cli", "mvp"],
    "dependencies": [
      {"id": "lb-1td", "title": "Foundation", "status": "open", "priority": 0,
       "issue_type": "task", "dependency_type": "blocks"}
    ],
    "dependent_count": 2,
    "dependency_count": 1
  }
]`
	detail, err := decodeDetail([]byte(fixture))
	if err != nil {
		t.Fatalf("decodeDetail: %v", err)
	}
	if detail.ID != "lb-nvw" {
		t.Errorf("id = %q", detail.ID)
	}
	if len(detail.Labels) != 2 {
		t.Errorf("labels = %v", detail.Labels)
	}
	if len(detail.Dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(detail.Dependencies))
	}
	dep := detail.Dependencies[0]
	if dep.Type != domain.RelBlocks {
		t.Errorf("dependency type = %q, want blocks", dep.Type)
	}
	if blockers := detail.Blockers(); len(blockers) != 1 {
		t.Errorf("open blocking dependency should count as a blocker, got %d", len(blockers))
	}
}

// TestClosedDependencyIsNotABlocker verifies readiness reasoning ignores
// resolved blockers.
func TestClosedDependencyIsNotABlocker(t *testing.T) {
	detail := domain.IssueDetail{
		Issue: domain.Issue{ID: "a"},
		Dependencies: []domain.Dependency{
			{Type: domain.RelBlocks, Issue: domain.Issue{ID: "b", Status: domain.StatusClosed}},
			{Type: domain.RelRelatesTo, Issue: domain.Issue{ID: "c", Status: domain.StatusOpen}},
		},
	}
	if got := detail.Blockers(); len(got) != 0 {
		t.Errorf("closed and non-blocking relations must not block: %v", got)
	}
}

func TestDecodeEmptyEnvelopeIsNotAnError(t *testing.T) {
	// `bd memories --json` returns a bare envelope when there is nothing to say.
	issues, err := decodeIssues([]byte(`{"schema_version": 1}`))
	if err != nil {
		t.Fatalf("empty envelope should decode as no results: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("got %d issues, want 0", len(issues))
	}
}

func TestDecodeMalformedIsTypedError(t *testing.T) {
	_, err := decodeIssues([]byte(`{"id": "lb-1", "priority": `))
	ce, ok := AsCommandError(err)
	if !ok {
		t.Fatalf("expected a CommandError, got %T: %v", err, err)
	}
	if ce.Kind != ErrDecode {
		t.Errorf("kind = %q, want %q", ce.Kind, ErrDecode)
	}
}

func TestDecodeUnknownStatusIsPreserved(t *testing.T) {
	issues, err := decodeIssues([]byte(`[{"id":"x","status":"marinating","issue_type":"widget","priority":9}]`))
	if err != nil {
		t.Fatalf("unknown values must decode, not fail: %v", err)
	}
	if issues[0].Status != "marinating" {
		t.Errorf("status = %q, want it preserved verbatim", issues[0].Status)
	}
	if issues[0].Type != "widget" {
		t.Errorf("type = %q, want it preserved verbatim", issues[0].Type)
	}
	if issues[0].Priority != 9 {
		t.Errorf("priority = %v, want 9 preserved", issues[0].Priority)
	}
	if issues[0].Priority.Label() != "P9" {
		t.Errorf("label = %q", issues[0].Priority.Label())
	}
}

func TestMissingPriorityIsUnknownNotZero(t *testing.T) {
	// Zero is the most urgent priority, so a missing field must never decode to
	// it or unprioritized work would be recommended first.
	issues, err := decodeIssues([]byte(`[{"id":"x","title":"t"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if issues[0].Priority != domain.PriorityUnknown {
		t.Errorf("priority = %v, want unknown", issues[0].Priority)
	}
	if issues[0].Priority.Score() != 0 {
		t.Errorf("unknown priority should score 0, got %v", issues[0].Priority.Score())
	}
}

// TestSkewedTimestampsClampToZero covers bd emitting mixed-zone timestamps,
// which would otherwise render as negative ages.
func TestSkewedTimestampsClampToZero(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	issue := domain.Issue{CreatedAt: &future, UpdatedAt: &future}
	if got := issue.Age(time.Now()); got != 0 {
		t.Errorf("Age = %v, want 0 for a future timestamp", got)
	}
	if got := issue.Idle(time.Now()); got != 0 {
		t.Errorf("Idle = %v, want 0 for a future timestamp", got)
	}
}

func TestActorFrom(t *testing.T) {
	cases := []struct{ in, name, email string }{
		{"Jo Smith <jo@example.com>", "Jo Smith", "jo@example.com"},
		{"jo@example.com", "jo@example.com", "jo@example.com"},
		{"claude-agent-1", "claude-agent-1", ""},
	}
	for _, c := range cases {
		got := actorFrom(c.in)
		if got == nil {
			t.Fatalf("actorFrom(%q) = nil", c.in)
		}
		if got.Name != c.name || got.Email != c.email {
			t.Errorf("actorFrom(%q) = %+v, want %s/%s", c.in, got, c.name, c.email)
		}
	}
	if actorFrom("  ") != nil {
		t.Error("blank actor should decode as nil")
	}
}
