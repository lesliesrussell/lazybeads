# Compatibility

LazyBeads is tested against **Beads 1.0.x** (captured fixtures under `internal/beads/testdata/bd-1.0.5/`). Other versions are used through capability probing, not version-string gating alone.

At startup the adapter records:

- JSON output
- Atomic `--claim`
- Dependency `--type`
- History, memory, reopen, unclaim (assignee+status clear)
- Sync inspection (`bd dolt`)

Missing capabilities return exit **6** with a hint. LazyBeads will not fall back to writing the Dolt database.

`lb doctor` warns when the installed `bd` is outside the 1.0.x range and continues.

Schema mismatch is exit **5**. Upgrade `bd` and follow Beads' own migration; LazyBeads will not run `bd migrate`.

JSON decode is tolerant of field-name drift (`issue_type` vs `type`, `owner` vs `assignee`) and of Beads 1.0.5 encoding nested dependency `metadata` as the string `"{}"`.
