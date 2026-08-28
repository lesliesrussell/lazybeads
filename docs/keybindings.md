# Keybindings

The TUI is fully usable without a mouse. Arrows always work; `j`/`k` are additional list motion.

| Key | Action |
|---|---|
| `j` / `k` or arrows | Move |
| `ctrl-d` / `ctrl-u` | Half-page |
| `G` | Last item |
| `enter` | Issue detail |
| `esc` | Back / clear filter / cancel overlay |
| `q` | Back; quit at top-level |
| `r` `f` `b` `i` `a` `m` `h` | Ready, focus, blocked, issues, activity, memory, health |
| `g` | Graph for the selected issue |
| `c` / `u` / `x` / `n` | Claim / unclaim / close / create (always confirm) |
| `/` | Filter |
| `:` | Command palette |
| `R` | Refresh |
| `?` | Help |
| `y` / `Y` | Copy issue id / copy `lb show <id>` |

Mutations open a modal that always shows the issue id, title, action, and actor:

```
[y] Confirm  [n/esc] Cancel  [v] View raw command
```

Close asks for a reason before that modal. Configuration collisions are reported by `lb doctor` (`config.DetectKeyCollisions`).
