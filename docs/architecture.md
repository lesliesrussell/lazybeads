# Architecture

```
LazyBeads UI/commands → typed Go services → argv-based bd adapter → Beads
```

Beads remains the only authority for issue data. LazyBeads never writes `.beads/`, never issues SQL/Dolt, and never shells out with interpolated strings.

## Packages

| Package | Role |
|---|---|
| `cmd/lb` | Binary entrypoint |
| `internal/cli` | Cobra commands, confirmation, rendering |
| `internal/tui` | Bubble Tea client of `app.Service` |
| `internal/app` | Ranking, graph, mutations, doctor — shared by CLI and TUI |
| `internal/beads` | `exec.CommandContext` adapter, JSON decode, capabilities |
| `internal/config` | XDG/user/project TOML + env |
| `internal/workspace` | Resolution order, actor, write lock |
| `internal/output` | JSON envelope, tables, sanitizer |
| `internal/domain` | Issue, graph, health, exit codes |

The TUI holds no unique business logic. Pressing `c` on a selected issue calls `Service.Claim`, the same path as `lb claim`.

## Mutation protocol

1. Read current issue.
2. Show identity (workspace, issue, actor, intended change).
3. Confirm (`--yes` off-TTY, prompt on TTY, `--dry-run` never executes).
4. Take the per-workspace write lock.
5. Run `bd` via argv.
6. Re-fetch and display confirmed state.

## Recommendation

`lb next` ranks Beads' ready set. Every score factor is named in JSON and in `--verbose` human output. No hidden weights.
