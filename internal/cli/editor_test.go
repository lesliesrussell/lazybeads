// lb-rc1
package cli

import (
	"strings"
	"testing"

	"github.com/lesliesrussell/lazybeads/internal/domain"
)

func TestParseEditableDetectsTitleAndLabelChanges(t *testing.T) {
	original := domain.Issue{
		ID: "lb-1", Title: "Old", Type: domain.TypeTask, Priority: 2,
		Labels: []string{"cli"}, Description: "body",
	}
	text := renderEditable(original)
	text = strings.Replace(text, "title: Old", "title: New title", 1)
	text = strings.Replace(text, "  - cli", "  - cli\n  - mvp", 1)
	in, err := parseEditable(original, text)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title == nil || *in.Title != "New title" {
		t.Errorf("title = %v", in.Title)
	}
	if len(in.SetLabels) != 2 {
		t.Errorf("labels = %v", in.SetLabels)
	}
}
