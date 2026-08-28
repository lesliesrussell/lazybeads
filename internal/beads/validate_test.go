// lb-1td
package beads

import (
	"strings"
	"testing"
)

func TestValidateTitle(t *testing.T) {
	if err := ValidateTitle("Add typed adapter"); err != nil {
		t.Fatalf("valid title rejected: %v", err)
	}
	if err := ValidateTitle("   "); err == nil {
		t.Error("whitespace-only title should be rejected")
	}
	if err := ValidateTitle("bad\x00title"); err == nil {
		t.Error("NUL bytes should be rejected")
	}
	// The limit is in code points, so a multi-byte title near the byte limit
	// must still be accepted.
	wide := strings.Repeat("字", MaxTitleRunes)
	if err := ValidateTitle(wide); err != nil {
		t.Errorf("%d-rune title should be accepted: %v", MaxTitleRunes, err)
	}
	if err := ValidateTitle(wide + "字"); err == nil {
		t.Error("over-length title should be rejected")
	}
}

func TestValidateTitlePreservesUnicode(t *testing.T) {
	// Validation must not normalize; the caller sends the exact bytes onward.
	in := "café ☕ ́combining"
	if err := ValidateTitle(in); err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
}

func TestValidateIDRejectsFlagLookalikes(t *testing.T) {
	if err := validateID("--force"); err == nil {
		t.Error("an ID that looks like a flag must be rejected before reaching argv")
	}
	if err := validateID(""); err == nil {
		t.Error("empty ID should be rejected")
	}
	if err := validateID("lb-a3f8.2"); err != nil {
		t.Errorf("valid ID rejected: %v", err)
	}
}

func TestDependencyArgsNeverReorder(t *testing.T) {
	// bd dep add <blocked> <blocker>. Silent reordering would invert the graph.
	got := DependencyArgs("add", DependencyInput{Blocked: "lb-4", Blocker: "lb-2", Type: "blocks"}, "")
	want := []string{"dep", "add", "lb-4", "lb-2", "--type", "blocks"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestClaimUsesAtomicUpdate(t *testing.T) {
	got := strings.Join(ClaimArgs("lb-2", "josh"), " ")
	if !strings.Contains(got, "update lb-2 --claim") {
		t.Errorf("claim must use the atomic bd operation, got %q", got)
	}
	if strings.Contains(got, "--status") || strings.Contains(got, "--assignee") {
		t.Errorf("claim must not separately set status and assignee: %q", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.5", "1.0.5", 0},
		{"1.0.5", "1.0.6", -1},
		{"1.1.0", "1.0.9", 1},
		{"v1.2.0", "1.2", 0},
		{"1.0.5-rc1", "1.0.5", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if !VersionWithinTestedRange("1.0.5") {
		t.Error("1.0.5 should be inside the tested range")
	}
	if VersionWithinTestedRange("2.5.0") {
		t.Error("2.5.0 should be outside the tested range")
	}
	if VersionWithinTestedRange("") {
		t.Error("an unknown version is not inside the tested range")
	}
}
