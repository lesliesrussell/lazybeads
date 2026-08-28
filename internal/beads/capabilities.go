// lb-1td
package beads

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Capabilities records which optional Beads features the installed binary
// actually provides. Presence is established by probing where practical rather
// than by parsing version strings alone.
type Capabilities struct {
	JSONOutput          bool `json:"json_output"`
	AtomicClaim         bool `json:"atomic_claim"`
	DependencyRelations bool `json:"dependency_relations"`
	EventHistory        bool `json:"event_history"`
	MemoryCommands      bool `json:"memory_commands"`
	CrossRig            bool `json:"cross_rig"`
	SyncInspection      bool `json:"sync_inspection"`
	Reopen              bool `json:"reopen"`
	Unclaim             bool `json:"unclaim"`
	CustomMetadata      bool `json:"custom_metadata"`

	// Version is the detected bd version, when readable.
	Version string `json:"version,omitempty"`
	// ProbeErrors records probes that could not be completed, so `lb doctor`
	// can distinguish "absent" from "undetermined".
	ProbeErrors []string `json:"probe_errors,omitempty"`
}

// Has reports whether a named capability is available.
func (c Capabilities) Has(name string) bool {
	switch strings.ToLower(name) {
	case "json", "json_output":
		return c.JSONOutput
	case "claim", "atomic_claim":
		return c.AtomicClaim
	case "relations", "dependency_relations":
		return c.DependencyRelations
	case "history", "event_history":
		return c.EventHistory
	case "memory", "memory_commands":
		return c.MemoryCommands
	case "rig", "cross_rig":
		return c.CrossRig
	case "sync", "sync_inspection":
		return c.SyncInspection
	case "reopen":
		return c.Reopen
	case "unclaim":
		return c.Unclaim
	case "metadata", "custom_metadata":
		return c.CustomMetadata
	}
	return false
}

// capsProbe caches capability detection per binary for a TTL window so every
// command does not pay for the probe.
var (
	capsMu    sync.Mutex
	capsCache = map[string]capsEntry{}
)

type capsEntry struct {
	caps Capabilities
	at   time.Time
}

const capsTTL = 5 * time.Minute

// Capabilities detects what the installed bd supports.
func (c *CLI) Capabilities(ctx context.Context, scope Scope) Capabilities {
	key := c.runner.Binary + "|" + scope.Project + "|" + scope.BeadsDir

	capsMu.Lock()
	if entry, ok := capsCache[key]; ok && time.Since(entry.at) < capsTTL {
		capsMu.Unlock()
		return entry.caps
	}
	capsMu.Unlock()

	caps := c.probeCapabilities(ctx, scope)

	capsMu.Lock()
	capsCache[key] = capsEntry{caps: caps, at: time.Now()}
	capsMu.Unlock()
	return caps
}

// probeCapabilities runs read-only probes. It never mutates state: a feature
// whose only proof would be a write is inferred from the help text instead.
func (c *CLI) probeCapabilities(ctx context.Context, scope Scope) Capabilities {
	caps := Capabilities{}

	if v, err := c.Version(ctx, scope); err == nil {
		caps.Version = v.Version
		caps.JSONOutput = true
	} else {
		caps.ProbeErrors = append(caps.ProbeErrors, "version: "+err.Error())
	}

	// A minimal JSON read confirms the output contract really works, not just
	// that the flag is accepted.
	if _, err := c.jsonCall(ctx, "capability probe", scope, "list", "--limit", "1", "--flat"); err == nil {
		caps.JSONOutput = true
	} else if caps.JSONOutput {
		caps.JSONOutput = false
		caps.ProbeErrors = append(caps.ProbeErrors, "json read: "+err.Error())
	}

	help := func(args ...string) string {
		res, err := c.runner.Run(ctx, "help probe", scope, append(args, "--help")...)
		if err != nil && res == nil {
			return ""
		}
		return strings.ToLower(string(res.Stdout) + string(res.Stderr))
	}

	updateHelp := help("update")
	caps.AtomicClaim = strings.Contains(updateHelp, "--claim")
	caps.CustomMetadata = strings.Contains(updateHelp, "--set-metadata") || strings.Contains(updateHelp, "--metadata")
	// bd has no dedicated unclaim; clearing the assignee is the supported path
	// and is only offered when the flag exists.
	caps.Unclaim = strings.Contains(updateHelp, "--assignee") && strings.Contains(updateHelp, "--status")

	depHelp := help("dep", "add")
	caps.DependencyRelations = strings.Contains(depHelp, "--type")

	rootHelp := help()
	caps.EventHistory = strings.Contains(rootHelp, "history")
	caps.MemoryCommands = strings.Contains(rootHelp, "remember") && strings.Contains(rootHelp, "memories")
	caps.Reopen = strings.Contains(rootHelp, "reopen")
	caps.SyncInspection = strings.Contains(rootHelp, "dolt")
	caps.CrossRig = strings.Contains(rootHelp, "rig") || strings.Contains(rootHelp, "federation")

	return caps
}

// CompareVersions returns -1, 0 or 1 comparing dotted version strings. Any
// non-numeric suffix is ignored so "1.0.5 (Homebrew)" compares cleanly.
func CompareVersions(a, b string) int {
	av, bv := splitVersion(a), splitVersion(b)
	for i := 0; i < len(av) || i < len(bv); i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, part := range strings.Split(v, ".") {
		digits := part
		for i, r := range part {
			if r < '0' || r > '9' {
				digits = part[:i]
				break
			}
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// TestedVersionRange documents the upstream versions LazyBeads has fixtures for.
const (
	MinTestedVersion = "1.0.0"
	MaxTestedVersion = "1.99.99"
)

// VersionWithinTestedRange reports whether the installed bd is inside the range
// LazyBeads has been verified against.
func VersionWithinTestedRange(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	return CompareVersions(v, MinTestedVersion) >= 0 && CompareVersions(v, MaxTestedVersion) <= 0
}
