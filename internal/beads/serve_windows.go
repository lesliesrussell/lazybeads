//go:build windows

// lb-4gm.3
package beads

import (
	"os"
)

// Windows has no SIGTERM; Stop's grace period ends in Kill either way.
func terminate(p *os.Process) { _ = p.Kill() }
