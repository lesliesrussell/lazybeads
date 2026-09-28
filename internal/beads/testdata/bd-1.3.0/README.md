Captured from `bd` 1.3.0 (Homebrew) in a throwaway workspace; personal
identifiers are replaced with fixture values.

Hand-authored, because `bd` refuses to create a cycle and a failing migration
cannot be reproduced on demand:

- `cycles.json` follows the `Cycle` schema in bd's `openapi.v0.yaml` (v1.3.0).
- `pending-migration.stderr` is the stderr of `bd list --json` against a
  remote-backed database with unapplied schema migrations.
