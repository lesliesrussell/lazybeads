// lb-b4p
// Package manpage serves the hand-written lb(1) manual page.
//
// The page is authored as roff rather than generated from the cobra command
// tree: cobra can describe flags, but not the configuration schema, the
// environment, the exit-code contract, or the interactive keybindings, and a
// manual that omits those does not cover the tool.
package manpage

import (
	_ "embed"
	"strings"
)

//go:embed lb.1
var source string

// Roff returns the manual page with the build version substituted into the
// .TH header.
func Roff(version string) string {
	if version == "" {
		version = "(unknown version)"
	}
	return strings.ReplaceAll(source, "@VERSION@", version)
}
