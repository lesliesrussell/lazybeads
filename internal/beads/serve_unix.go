//go:build !windows

// lb-4gm.3
package beads

import (
	"os"
	"syscall"
)

func terminate(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }
