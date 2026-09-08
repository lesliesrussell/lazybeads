# LazyBeads

The terminal operator console for [Beads](https://github.com/gastownhall/beads).

`bd` is the database, graph, and atomic coordination layer. `lb` is attention management: rank, explain, navigate, and mutate through `bd` only — never by writing `.beads/` itself.

```sh
make                 # bin/lb
make install         # ~/.local/bin/lb plus man page and shell completions
make install PREFIX=/usr/local   # system-wide; needs sudo
# or:
go install github.com/lesliesrussell/lazybeads/cmd/lb@latest
```

```sh
lb completion zsh    # print a completion script
lb man | man -l -    # read the lb(1) page
```

Requires a Beads CLI (`bd`) on `PATH`, or `LB_BD_BIN`. Tested against Beads 1.0.x; see [docs/compatibility.md](docs/compatibility.md).

## Quick start

```sh
cd ~/code/your-beads-project
lb doctor
lb status
lb next
lb claim <id> --yes
lb show <id>
lb close <id> --reason "done" --yes
```

On a real terminal, `lb` with no arguments launches the TUI. Otherwise it prints help and exits 2.

`lb doctor --fix` may write a user config file, create a cache directory, and install shell completion scripts. It never mutates Beads.

## Commands

| Area | Commands |
|---|---|
| Read | `status` `ready` `next` `show` `list` `search` `why` `graph` `blocked` |
| Mutate | `create` `edit` `claim` `unclaim` `close` `reopen` `assign` `dep` |
| Supervise | `focus` `stale` `activity` `memory` `sync status` `doctor` |
| UI | `tui` |

Every essential TUI action has a CLI equivalent. Mutations prompt on a TTY; non-interactive use requires `--yes`. `--dry-run` prints the `bd` argv and executes nothing.

JSON output is a stable envelope (`schema_version`, `command`, `workspace`, `data`, `warnings`, `generated_at`).

See [docs/](docs/) for architecture, configuration, keybindings, compatibility, and security.
