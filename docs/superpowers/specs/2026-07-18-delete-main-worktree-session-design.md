# Session-only delete on the main worktree

## Problem

The TUI forbids deleting the main worktree — correctly, since the main
worktree is the clone dir itself and removing it makes no sense. But the guard
is too broad. Pressing `d` on the main worktree short-circuits at the very top
of `startDelete()` with the status line *"cannot delete the main worktree"*,
before the confirm menu ever opens. That also blocks the perfectly legitimate
actions of killing the main worktree's **agent** or **shell** session (tmux
session, agent processes) while leaving the worktree in place.

The user wants `d` on the main worktree to still reach the session-only delete
options — just never the option that removes the worktree.

## Current behavior

### TUI (`d` key)

`internal/tui/actions.go` — `startDelete()`:

```go
if sel.IsMain {
    m.statusMsg = "cannot delete the main worktree"
    return m, nil   // hard block — menu never opens
}
```

The confirm menu (`internal/tui/confirm.go`) is never constructed for a main
worktree, so its `Delete agent session only` / `Delete shell session only`
choices are unreachable there.

### CLI

Two separate commands, different behavior for `main`:

- `ch delete session <project> <branch> [--shell]` → `Herd.StopSessions`.
  **No main-worktree guard anywhere.** Already works on the main worktree
  today — this is the "kill the session only" capability, and the CLI already
  has it. Unchanged by this design.
- `ch delete worktree <project> <branch>` → `Herd.Teardown` (workspace.go:321).
  **No explicit main guard.** With `--force` it calls
  `git.Remove(cloneDir, cloneDir)` on the main worktree, which git refuses with
  a cryptic low-level error instead of a friendly one.

## Design

### 1. TUI — narrow the block in `startDelete()`

Replace the broad `sel.IsMain` block with one scoped to the genuinely-empty
case:

```go
if sel.IsMain && !sel.HasAgent && !sel.HasShell {
    m.statusMsg = "main worktree has no sessions to delete"
    return m, nil
}
```

When the main worktree has at least one session, execution falls through and
opens the confirm menu in a main-worktree mode that never offers worktree
removal. The `groupProject` guard above it is unchanged.

### 2. Confirm menu — main-worktree options

`newConfirmModel` already receives the `Item`, which carries `IsMain`,
`HasAgent`, and `HasShell`. Branch on `IsMain` so the main worktree only ever
offers session kills:

| State | Choices |
|---|---|
| main + agent + shell | `Delete all sessions`, `Delete agent session only`, `Delete shell session only`, `Cancel` |
| main + agent | `Delete agent session only`, `Cancel` |
| main + shell | `Delete shell session only`, `Cancel` |
| main + no sessions | *never reaches here — gated in step 1* |

Non-main behavior is unchanged (still `Delete everything` + per-session +
`Cancel`). The `deleteAll` action (worktree + sessions) is **never** added to a
main-worktree menu, so the destructive path cannot fire on `main` — the
invariant is enforced at menu-construction time, not by trusting the handler.

### 3. New action — `deleteAllSessions`

The main + agent + shell case needs a "clear both sessions, keep the worktree"
option. That is distinct from `deleteAll`, which also removes the worktree, so
it gets its own action.

`internal/tui/confirm.go`:

```go
const (
    deleteAll         deleteAction = iota // worktree + all sessions
    deleteAllSessions                     // all sessions, keep the worktree
    deleteAgent                           // agent session only
    deleteShell                           // shell session only
    deleteCancel                          // cancel
)
```

Handler in `internal/tui/actions.go`, mirroring `confirmDeleteAgent` /
`confirmDeleteShell` but using `StopOpts{All: true}` (the same option
`Teardown` uses) instead of a per-type stop. No `Teardown` call, so the
worktree survives:

```go
func (m Model) confirmDeleteAllSessions() (tea.Model, tea.Cmd) {
    ref := m.confirm.target.Ref
    hrd := m.herd
    m.confirm, m.screen = nil, screenList
    return m, func() tea.Msg {
        if _, err := hrd.StopSessions(ref, herd.StopOpts{All: true}); err != nil {
            return errMsg{err: err}
        }
        return m.refreshCmd()()
    }
}
```

Wired into the `updateConfirmDelete` switch alongside the existing cases. The
`deleteAllSessions` choice is offered only on the main + agent + shell menu;
non-main worktrees already have `Delete everything`, which covers the
clear-it-all intent.

### 4. herd — a clean guard on `Teardown`

Add a sentinel to `internal/herd/errors.go`:

```go
ErrMainWorktree = errors.New("cannot delete the main worktree")
```

In `Teardown` (workspace.go), immediately after `cloneDir` is computed and
before any session-stopping or git work, guard on the same `wtPath == cloneDir`
test that defines `IsMain` elsewhere:

```go
if wtPath == cloneDir {
    return fmt.Errorf("%w: %s/%s", ErrMainWorktree, ref.Project, ref.Branch)
}
```

`ch delete session main` is untouched — it never calls `Teardown`, so it keeps
working exactly as today.

### 5. Error translators

Wire the new sentinel into both front-end translators (one error vocabulary):

- `cmd/errors.go` `herdErr`:
  ```go
  case errors.Is(err, herd.ErrMainWorktree):
      return fmt.Errorf("cannot delete the main worktree %s/%s — delete its sessions with 'ch delete session %s <branch>'", project, branch, project)
  ```
- `internal/tui/errors.go` `humanize`:
  ```go
  case errors.Is(err, herd.ErrMainWorktree):
      return "Cannot delete the main worktree."
  ```
  Defensive: the TUI now blocks this before `Teardown` is reached, but the
  vocabulary stays complete.

## Testing

- `internal/tui/actions_test.go`: the existing "delete main worktree blocked"
  test is replaced/extended —
  - main + session → `d` opens the confirm menu (screen becomes
    `screenConfirmDelete`), not a status message;
  - main + no session → status message with the new wording, stays on the list.
- `internal/tui` confirm test: a main-worktree `Item` yields a menu that omits
  the `deleteAll` choice; the agent+shell case includes `deleteAllSessions`.
- `internal/herd` `Teardown` test: a main-worktree ref returns
  `ErrMainWorktree` for both `Force: true` and `Force: false`, and no git
  removal is attempted.
- `cmd` translator test: `ErrMainWorktree` maps to the friendly message.

Run `make check` (coverage ≥ 80%, integration, lint, build) before completion.

## Scope / non-goals

- No new packages, no signature changes; every edit lands in existing files
  (`actions.go`, `confirm.go`, `errors.go` in `tui`, `cmd/errors.go`,
  `internal/herd/errors.go`, `internal/herd/workspace.go`).
- `ch delete session` behavior is unchanged — it already handles the main
  worktree.
- `deleteAllSessions` is not added to non-main menus.
