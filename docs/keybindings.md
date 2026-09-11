# Keybindings

`lb(1)` is the authoritative reference; run `man lb` (or `lb man`) for the full
list alongside every flag, config key, and exit code. This file is the quick
card.

The TUI is a lazygit-style tiled console: stats header, context tabs, a list
panel beside a live preview, and a coloured keybinding bar. `tab` moves focus
between list and preview. Below 100 columns the preview pane hides. The UI is
fully usable without a mouse; arrows always work, and `j`/`k` are additional
list motion.

| Key | Action |
|---|---|
| `j` / `k` or arrows | Move |
| `tab` | Switch focus between list and preview |
| `ctrl-d` / `ctrl-u` | Half-page |
| `G` / `home` | Last / first item |
| `enter` | Issue detail |
| `esc` | Back / clear filter / cancel overlay |
| `q` | Back; quit at top-level |
| `r` `f` `b` `i` `a` `m` `h` | Ready, focus, blocked, issues, activity, memory, health |
| `g` | Graph for the selected issue |
| `c` / `u` / `x` / `n` | Claim / unclaim / close / create (always confirm) |
| `/` | Filter (bare `closed` / `open` / `status:in_progress`) |
| `[` / `]` | Cycle the status filter (issues view) |
| `:` | Command palette |
| `R` | Refresh |
| `?` | Help |
| `y` / `Y` | Copy issue id / copy `lb show <id>` |

With the preview focused via `tab`, the motion keys scroll it.

Mutations open a modal that always shows the issue id, title, action, and actor:

```
[y] Confirm  [n/esc] Cancel  [v] View raw command
```

Close asks for a reason before that modal. Configuration collisions are reported by `lb doctor` (`config.DetectKeyCollisions`).
