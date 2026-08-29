// lb-58x
package version

// Version is the LazyBeads release identifier shown by `lb version` and `lb status`.
// Goreleaser overrides this via -X at release build time.
var Version = "0.1.0"

// TestedBeads is the Beads series covered by fixtures and the compatibility matrix.
const TestedBeads = "1.0.x"
