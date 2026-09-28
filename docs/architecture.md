# Architecture

```
LazyBeads UI/commands → typed Go services → mirror (TUI, --watch) → transport → Beads
                                                                     ├─ bd CLI (argv)
                                                                     └─ bd serve (/v0 HTTP)
```

Beads remains the only authority for issue data. LazyBeads reaches it through the `bd` CLI or through `bd serve`, Beads' own HTTP API. It never writes `.beads/`, never issues SQL/Dolt, never migrates a schema, and never shells out with interpolated strings.

Until Beads 1.3.0 the rule was "`bd` CLI only". Beads 1.3.0 added an events journal (`bd events`) and an HTTP API (`bd serve`), and the Beads maintainers recommended that tools like LazyBeads use them ([issue #1](https://github.com/lesliesrussell/lazybeads/issues/1)). LazyBeads follows that advice: both are Beads' own supported interfaces, so the rule is now "Beads' interfaces only".

## Packages

| Package | Role |
|---|---|
| `cmd/lb` | Binary entrypoint |
| `internal/cli` | Cobra commands, confirmation, rendering |
| `internal/tui` | Bubble Tea client of `app.Service`; redraws as the mirror changes |
| `internal/app` | Ranking, graph, mutations, doctor, transport selection — shared by CLI and TUI |
| `internal/mirror` | Local copy of the workspace fed by the events journal |
| `internal/beads` | Transports: the `bd` CLI adapter (`exec.CommandContext`, streaming) and the `bd serve` client; JSON decode; journal reader; capabilities |
| `internal/config` | XDG/user/project TOML + env |
| `internal/workspace` | Resolution order, actor, write lock |
| `internal/output` | JSON envelope, tables, sanitizer |
| `internal/domain` | Issue, graph, health, exit codes |

The TUI holds no unique business logic. Pressing `c` on a selected issue calls `Service.Claim`, the same path as `lb claim`.

## Transports

Every read and write goes through one `beads.Client` interface.

- **CLI** runs `bd` with an argv vector. It is always available and is the fallback for everything else.
- **HTTP** talks to `bd serve` (`/v0`). It is used for a server named in `serve.url`, or one the TUI and `--watch` start themselves (`bd serve --addr 127.0.0.1:0`) when the workspace runs a Dolt server; embedded-Dolt workspaces cannot serve HTTP. LazyBeads checks the server serves this workspace's Beads project and sends `Bd-Project-Id` on every request. Operations the server does not advertise, filters it lacks, arguments it refuses, and any read while it is unreachable go to the CLI. Writes go over HTTP only when `serve.http_writes` allows it (by default, not in a workspace with bd event hooks, which HTTP writes skip), and fall back to the CLI only when the request never left LazyBeads.

`lb doctor` reports which transport is in use and why.

## Events journal and the mirror

With `bd config set events-journal true`, every committed mutation writes an ordered record carrying the issue's full state after the change. LazyBeads never turns the journal on: that changes what every `bd` command in the workspace records.

For long-lived sessions (the TUI, `lb status --watch`), `internal/mirror` keeps a local copy:

1. Read the journal head, the whole workspace (`list --all`, `blocked`), and the head again; retry if they differ.
2. Follow the journal from the head — `bd events tail --follow`, or `events:watch` (Server-Sent Events, resumed with `Last-Event-ID`) over HTTP — and apply each record instead of querying again.
3. Answer list, ready, blocked, stats and dependency reads from the copy, with Beads' own rules; pass everything else, and every mutation, to the transport.

The copy answers only when it is exactly right. While it is being built or replaying, while a mutation LazyBeads made is waiting for its own record, for filters it does not model, and whenever the journal is off, reads go to Beads. It is rebuilt when the journal reports truncation, when the user presses `R`, and every five minutes, because `bd dolt pull` and `bd sql` are not journaled. Without a journal, views poll every `general.refresh_interval`.

The activity feed reads the newest journal records in one call when they cover the requested window, and otherwise `bd history` per issue.

## Mutation protocol

1. Read current issue.
2. Show identity (workspace, issue, actor, intended change).
3. Confirm (`--yes` off-TTY, prompt on TTY, `--dry-run` never executes).
4. Take the per-workspace write lock.
5. Send the change through the transport (`bd` via argv, or `bd serve`).
6. Re-fetch and display confirmed state.

## Recommendation

`lb next` ranks Beads' ready set. Every score factor is named in JSON and in `--verbose` human output. No hidden weights.
