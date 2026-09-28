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
- **`bd serve` is trusted only on this machine and for this workspace.** `serve.url` must be `http` on a loopback host; a server LazyBeads starts binds `127.0.0.1` on an ephemeral port and is stopped on exit. LazyBeads compares the server's Beads project id with the workspace's before using it, and sends `Bd-Project-Id` on every request so a server for another workspace refuses it.
- **Credentials stay out of argv and config.** A `bd serve` bearer token is read from `serve.token_file` and sent as a header; it never appears in a command line, a trace or an error.
- **A cloned repository cannot redirect LazyBeads.** `serve.url` and `serve.token_file` are read from user config and the environment only, never from a project `.lazybeads.toml`, and a project may turn `serve.http_writes` off but not force it on.
- **bd event hooks keep running.** `bd serve` does not run `on_create`, `on_update` or `on_close` hooks, so by default LazyBeads keeps writes on the `bd` CLI in a workspace that has any.
- **Writes are never repeated.** A write falls back from HTTP to the CLI only when the connection itself failed; one that may have reached the server is reported, not retried.

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
