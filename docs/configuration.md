# Configuration

Precedence, highest first:

1. Command flags
2. Environment variables
3. Project file `.lazybeads.toml`
4. User file
5. Built-in defaults

User file locations:

- macOS: `~/Library/Application Support/lazybeads/config.toml`
- Linux: `$XDG_CONFIG_HOME/lazybeads/config.toml` or `~/.config/lazybeads/config.toml`
- Windows: `%AppData%\lazybeads\config.toml`
- Override: `LB_CONFIG`

```toml
[general]
bd_binary = "bd"
actor = "you"
color = "auto"          # auto | always | never
confirm_mutations = true
stale_after = "8h"
timeout = "30s"

[workspace]
focus_parent = ""
exclude_labels = ["wontfix"]
ignore_issues = []

[ranking]
strategy = "balanced"   # priority | leverage | age | balanced | random
priority_weight = 100
leverage_weight = 15
age_weight = 1
focus_weight = 25
recently_unblocked_weight = 10
max_graph_depth = 8
max_graph_nodes = 500

[tui]
ascii = false

[serve]
# url = "http://127.0.0.1:7777"   # a running `bd serve`; loopback only
# token_file = "~/.config/beads/serve-token"
auto_start = "auto"               # TUI and --watch start bd serve on Dolt-server workspaces
```

Environment:

```
LB_BD_BIN  LB_CONFIG  LB_PROJECT  LB_BEADS_DIR  LB_RIG
LB_ACTOR   LB_COLOR   LB_TIMEOUT  LB_NO_CONFIRM  LB_LOG_LEVEL
LB_SERVE_URL  LB_SERVE_TOKEN_FILE  LB_SERVE_AUTO_START
```

With a `bd serve` available, reads go over its HTTP API and fall back to the `bd` CLI for anything the server cannot answer. `serve.url` and `serve.token_file` are honoured only from user config and the environment, never from a project `.lazybeads.toml`. `serve.url` must be on a loopback host and must serve this workspace's Beads project; `lb doctor` reports which transport is in use and why. `bd serve` needs a Dolt server workspace, so embedded-Dolt workspaces always use the CLI.

`LB_NO_CONFIRM=1` is environment-only. Project config cannot silently disable confirmation. `lb doctor --fix` may write a user config file if one is missing, create the cache directory (`LB_CACHE_DIR`), and write completion scripts under the data directory (`LB_DATA_DIR`); it never migrates Beads.
