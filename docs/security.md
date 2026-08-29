# Security

LazyBeads treats issue text as hostile. Titles, descriptions, labels, events, and memories are authored by collaborators and agents.

## Protections

- **No shell.** Every `bd` invocation is `exec.CommandContext` with an argv vector.
- **Terminal sanitization.** Control characters, OSC (clipboard, window title, hyperlinks), CSI, and bidi overrides are neutralized before display.
- **Workspace identity** is shown before mutations.
- **Freshness.** Read → confirm → write → re-read.
- **Write lock** per canonical workspace id, in-process only.
- **Bounds.** 30s timeout, 10 MiB stdout, 2 MiB stderr, graph depth 8 / 500 nodes.
- **Debug traces** redact environment values.

`lb doctor --fix` may create local LazyBeads configuration, a cache directory, and optional shell completion scripts. It does not upgrade Beads, push/pull Dolt, or change issues.

## Exit codes

| Code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Runtime failure |
| 2 | Usage, or mutation without `--yes` off-TTY |
| 3 | `bd` missing/unexecutable |
| 4 | Workspace discovery |
| 5 | Schema mismatch |
| 6 | Missing Beads capability |
| 7 | Mutation rejected |
| 8 | User declined confirmation |
| 9 | Timeout |
| 10 | Internal invariant |
