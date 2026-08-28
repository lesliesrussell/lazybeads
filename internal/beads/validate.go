// lb-1td
package beads

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxTitleRunes bounds a title at the specification's limit, measured in
// Unicode code points rather than bytes so non-ASCII titles are not truncated
// early.
const MaxTitleRunes = 512

// ValidateTitle enforces the documented title rules while preserving Unicode
// exactly: the title is never normalized, case-folded, or transliterated.
func ValidateTitle(title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return &CommandError{Kind: ErrValidation, Operation: "validate title",
			Cause: fmt.Errorf("a title is required")}
	}
	if strings.ContainsRune(title, 0) {
		return &CommandError{Kind: ErrValidation, Operation: "validate title",
			Cause: fmt.Errorf("a title may not contain NUL bytes")}
	}
	if n := utf8.RuneCountInString(trimmed); n > MaxTitleRunes {
		return &CommandError{Kind: ErrValidation, Operation: "validate title",
			Cause: fmt.Errorf("title is %d characters; the maximum is %d", n, MaxTitleRunes)}
	}
	return nil
}

// validateID rejects empty or structurally impossible issue IDs before they
// reach argv, so a stray value cannot be interpreted as a flag.
func validateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return &CommandError{Kind: ErrValidation, Operation: "validate id",
			Cause: fmt.Errorf("an issue ID is required")}
	}
	if strings.HasPrefix(id, "-") {
		return &CommandError{Kind: ErrValidation, Operation: "validate id",
			Cause: fmt.Errorf("%q is not a valid issue ID", id)}
	}
	if strings.ContainsAny(id, "\x00\n\r") {
		return &CommandError{Kind: ErrValidation, Operation: "validate id",
			Cause: fmt.Errorf("issue IDs may not contain control characters")}
	}
	return nil
}
