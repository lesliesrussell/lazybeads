# Compatibility

LazyBeads talks to Beads only through the `bd` CLI. It never writes `.beads/` or the Dolt database.

## Version matrix

| LazyBeads | Beads | Status |
|---|---|---|
| 0.1.x | 1.0.x | Tested. Fixtures under `internal/beads/testdata/bd-1.0.5/`. |
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

Missing capabilities return exit **6** with a hint. LazyBeads will not fall back to writing the Dolt database.

## Doctor

`lb doctor` warns when the installed `bd` is outside the 1.0.x range and continues. It also reports:

- Schema compatibility (error on mismatch; LazyBeads will not run `bd migrate`)
- JSON parse
- Workspace discovery
- Actor, config, stale claims, cycles
- Parent/status inconsistencies (open child of a closed parent, `in_progress` without an assignee)
- Sync visibility

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
