# Compatibility

LazyBeads talks to Beads only through Beads' own interfaces: the `bd` CLI, and from Beads 1.3.0 the events journal and the `bd serve` HTTP API. It never writes `.beads/` or the Dolt database.

## Beads 1.3.0 features

| Feature | Needs | Without it |
|---|---|---|
| Live views (journal-fed mirror) | `bd events` and `bd config set events-journal true` | Views poll every `refresh_interval` |
| HTTP transport (`bd serve`) | A Dolt server or proxied-server workspace | The `bd` CLI; embedded Dolt is refused by `bd serve` 1.3.0 |
| HTTP writes | HTTP transport, an actor, and no bd event hooks (or `serve.http_writes = "always"`) | Writes use the `bd` CLI |
| Journal-backed activity feed | A journal that covers the requested window | `bd history` per issue |

Each Beads clone has its own journal sequence, and syncs (`bd dolt pull`) are not journaled, so the mirror is rebuilt on `R` and every five minutes.

## Version matrix

| LazyBeads | Beads | Status |
|---|---|---|
| 0.1.x | 1.0.x | Tested. Fixtures under `internal/beads/testdata/bd-1.0.5/`. |
| 0.1.x | 1.3.x | Tested. Fixtures under `internal/beads/testdata/bd-1.3.0/`. |
| 0.1.x | other 1.x | Used through capability probing. `lb doctor` warns and continues. |
| 0.1.x | 0.x / unknown | Error if `bd` cannot run or JSON cannot be parsed. Schema mismatch is exit **5**. |

`lb version` prints the LazyBeads identifier. Release binaries inject that identifier via GoReleaser ldflags.

## Capability probe

At startup the adapter records presence, not version strings alone:

| Capability | If missing |
|---|---|
| JSON output | Error; LazyBeads cannot operate |
| Atomic `--claim` | Exit **6** on `lb claim` |
| Dependency `--type` | Exit **6** on typed `lb dep add` |
| History | `lb show --events` degrades |
| Memory | `lb memory` degrades |
| Reopen / unclaim | Those commands exit **6** |
| Sync inspection (`bd dolt`) | `lb sync status` reports unknown; never invents “clean” |
| Events journal (`bd events tail --follow`) | Views poll; the activity feed uses `bd history` |

Missing capabilities return exit **6** with a hint. LazyBeads will not fall back to writing the Dolt database.

## Doctor

`lb doctor` warns when the installed `bd` is outside the tested 1.0.x–1.3.x range and continues. It also reports:

- Schema compatibility (error on mismatch; LazyBeads will not run `bd migrate`). When a newer `bd` refuses to apply pending migrations to a remote-backed database, lb says so and points at `bd migrate --force && bd dolt push` on the one designated clone, or `bd bootstrap` on the others, instead of advising an upgrade.
- JSON parse
- Workspace discovery
- Actor, config, stale claims, cycles
- Parent/status inconsistencies (open child of a closed parent, `in_progress` without an assignee)
- Sync visibility
- Transport: whether reads and writes go through `bd serve` or the `bd` CLI, and why
- Events journal: whether it is on, and how to turn it on

`lb doctor --fix` may write a user config file, create the cache directory, and install optional shell completion scripts. It never upgrades Beads, pushes or pulls Dolt, or changes issues.

## JSON drift

JSON decode is tolerant of field-name drift (`issue_type` vs `type`, `owner` vs `assignee`) and of Beads 1.0.5 encoding nested dependency `metadata` as the string `"{}"`.

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
