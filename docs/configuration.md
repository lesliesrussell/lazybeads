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
```

Environment:

```
LB_BD_BIN  LB_CONFIG  LB_PROJECT  LB_BEADS_DIR  LB_RIG
LB_ACTOR   LB_COLOR   LB_TIMEOUT  LB_NO_CONFIRM  LB_LOG_LEVEL
```

`LB_NO_CONFIRM=1` is environment-only. Project config cannot silently disable confirmation. `lb doctor --fix` may write a user config file if one is missing; it never migrates Beads.
