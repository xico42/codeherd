# Tmux-Native TUI

## Problem

Running `ch` prints help. The TUI requires `ch tui`. Attaching to agent sessions replaces the TUI process via `syscall.Exec`, so users must relaunch the TUI each time they switch back. There is no handling for nested tmux sessions.

## Design

### Root command change

- `ch` (no args) launches the TUI inside a `codeherd` tmux session.
- `ch help` shows the current help output (Cobra built-in).
- `ch tui` subcommand is removed.
- `--no-tmux` flag on root runs the TUI directly in the terminal, bypassing tmux wrapping.

### Tmux session lifecycle

When `ch` runs with no subcommand and `--no-tmux` is not set:

```
1. --no-tmux?
   → Run TUI directly in terminal.

2. Inside the `codeherd` tmux session?
   → select-window to TUI window, exit 0.

3. Inside tmux, different session?
   → Create `codeherd` session if it does not exist (command: `ch --no-tmux`).
   → switch-client -t codeherd.

4. Not inside tmux?
   → Create `codeherd` session if it does not exist (command: `ch --no-tmux`).
   → attach-session -t codeherd.
```

The `codeherd` session runs `ch --no-tmux` as its window command. This avoids infinite recursion — the TUI runs directly inside the tmux window.

### Attach flow

The current `execTmuxAttach` uses `syscall.Exec("tmux", "attach-session", ...)`, replacing the TUI process. This changes to a context-aware flow:

**Inside tmux (default path):**
- `PendingAttach` triggers `tmux switch-client -t <agent-session>` via the tmux `Runner`.
- The TUI process stays alive and keeps refreshing.
- No `tea.Quit` — the TUI remains running in the `codeherd` session.

**Outside tmux (`--no-tmux`):**
- Current behavior preserved: `syscall.Exec` replaces the process with `tmux attach-session`.

### Returning to the TUI

Standard tmux session switching. No custom keybindings needed:
- `prefix + s` — session list picker
- `prefix + (` / `)` — previous/next session

### Files affected

| File | Change |
|---|---|
| `cmd/root.go` | Add default `Run`, `--no-tmux` flag, tmux lifecycle logic |
| `cmd/tui.go` | Delete |
| `cmd/session.go` | Make `execTmuxAttach` context-aware (switch-client vs syscall.Exec) |
| `internal/tui/model.go` | `PendingAttach` triggers switch-client instead of tea.Quit when inside tmux |
| `internal/tui/actions.go` | Attach action uses switch-client when inside tmux |
| `internal/tmux/client.go` | Add `SwitchClient`, `SelectWindow`, `AttachSession` methods |
