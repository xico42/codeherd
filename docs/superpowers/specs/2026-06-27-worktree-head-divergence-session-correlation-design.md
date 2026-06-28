# Keep sessions visible when a worktree's HEAD diverges

**Status:** Design approved
**Date:** 2026-06-27
**Scope:** session ↔ worktree correlation, across `session`, `worktree`, `semconv`, and the TUI

## Problem

When a git rebase is in progress in a codeherd worktree, or the user checks
out a different branch inside it, the worktree's session disappears from the
TUI. The session is still alive in tmux — the TUI just can't find it, so the
user can no longer attach or switch back to that worktree.

### Root cause

A session's identity is **frozen at creation time**: `session.Start` stamps
`@codeherd_canonical_name = SessionName(profile, project, branch)` into the
tmux session, and it never changes (`internal/session/session.go:83,146`).

The TUI, however, re-derives the correlation key from the worktree's **live**
git branch on every refresh:

```go
// internal/tui/items.go:77
sessionName := semconv.SessionName(data.profile, wt.project, wt.branch) // wt.branch is live
```

`wt.branch` comes from `git worktree list --porcelain`
(`internal/worktree/worktree.go:232`), which reports:

- **empty** when HEAD is detached — a rebase in progress detaches HEAD; and
- the **new branch** when a different branch is checked out.

Either way the recomputed key (`"proj-"` or `"proj-otherbranch"`) no longer
matches the frozen canonical name (`"proj-feature"`), and the join at
`items.go:89` fails. The same flaw exists in `worktree.Service.List`
(`worktree.go:588`), so `ch list worktree` mislabels these worktrees too.

## Core insight

A session's correlation key must come from something **stable** — not the live
HEAD. Every worktree has a stable identity branch that does not change on
rebase or checkout, and it can be derived without any new state:

- **Worktrees under `__worktrees/`:** the directory is named
  `FlattenBranch(branch)`, so `filepath.Base(path)` *is* the flattened branch.
  `FlattenBranch` is idempotent, so feeding the folder name back through
  `SessionName` reproduces the frozen canonical name exactly. No collisions are
  possible: two branches that flatten to the same name (`feat/x`, `feat-x`)
  would map to the same directory and so cannot coexist.

- **The main clone dir** (a worktree too, and one you *can* start a session on
  by pressing enter on its row — `actions.go:67`): its folder is named after
  the repo, not a branch, so the folder name can't be used. But the clone is
  checked out on `config.DefaultBranch` (`project/project.go:146`), and a
  session started there is created under that same branch. So the identity
  branch is recoverable from `config.DefaultBranch`. This works retroactively —
  it relies only on config plus the already-stamped canonical name.

This is **folder-name correlation**: exact, retroactive (fixes already-running
sessions on rebuild, no restart), and profile-scoped (the key includes the
active profile, matching today's behaviour). No new tmux correlation state.

## Display behavior

When HEAD has diverged, the worktree is still listed under its **creation-time
identity branch**, annotated with a **live-state hint**:

- **Identity branch (displayed name):**
  - normally, the live branch git reports (exact, with slashes);
  - when diverged, the matched session's raw branch from a new
    `@codeherd_branch` tmux option (exact, with slashes) if present;
  - otherwise the folder name (for `__worktrees/`) or `config.DefaultBranch`
    (for the clone dir). The folder-name form is flattened — slashes shown as
    dashes — which is the agreed fallback for pre-upgrade sessions and
    session-less worktrees.

- **Live-state hint** (new `Item.HeadHint`, rendered after the branch on
  `delegate.go:46`):
  - `detached` when HEAD is detached (covers rebase, bisect, manual detach);
  - `on <live-branch>` when a different branch is checked out;
  - empty when HEAD is on its identity branch.

  The hint uses only data already parsed from porcelain — no extra git calls.

## Components and changes

### `internal/semconv`
- New const `TmuxOptionBranch = "@codeherd_branch"`.
- New helper `WorktreeIdentityBranch(path, cloneDir, defaultBranch, liveBranch string) string`
  returning the identity branch to feed into `SessionName`:
  - `path != cloneDir` → `filepath.Base(path)` (the flattened branch);
  - `path == cloneDir` (clone dir) → `defaultBranch` when set, else `liveBranch`
    as best effort.

  `SessionName` flattens the result, so returning either the already-flattened
  folder name or the raw config branch yields the correct key (flatten is
  idempotent). Shared by the TUI and `worktree.Service.List`.

### `internal/worktree`
- `WorktreeInfo` gains `Detached bool`.
- `parseWorktreePorcelain` sets `Detached = true` on the `detached` porcelain
  line. `Branch` keeps its current meaning (live branch, empty when detached) —
  existing tests are unaffected.
- `Service.List` uses `WorktreeIdentityBranch` (it has the clone dir and the
  project's `DefaultBranch` in scope) for its `Session` lookup, so
  `ch list worktree` stays consistent.

### `internal/session`
- `Start` stamps one new option alongside the existing ones:
  `@codeherd_branch = req.Branch` (raw, with slashes), for pretty display.

### `internal/tmux`
- `SessionRecord` gains `Branch string`.
- `ListSessions` extends its `-F` format string and field parsing to read
  `#{@codeherd_branch}`.

### `internal/tui`
- `refreshResult` carries the worktree `Detached` flag, a
  `defaultBranches map[string]string` (project → `config.DefaultBranch`), and a
  `sessionBranch map[string]string` (canonical name → raw branch).
- `refreshCmd` populates `defaultBranches` from `cfg`, and `sessionBranch` from
  every session record (agent and shell).
- `buildItems` builds the lookup key with
  `SessionName(profile, project, WorktreeIdentityBranch(path, cloneDir, defaultBranch, liveBranch))`
  instead of the live branch, then computes the display branch and `HeadHint`
  as described above. `Item` gains `HeadHint string`.
- `delegate.go` appends ` (<HeadHint>)` to line 1 when `HeadHint != ""`.

## Data flow (after)

```
git worktree list --porcelain ─→ WorktreeInfo{Path, Branch(live), Detached}
                                        │
folder name (or config.DefaultBranch for clone dir)
        └─→ WorktreeIdentityBranch ─→ SessionName ─→ key ─┐
                                                          ├─→ match canonical → Item
tmux ListSessions ─→ {ID, CanonicalName, Branch(raw), Type, Status, …} ──────┘
                                        │
            display branch = raw branch (or folder / config / live fallback) + HeadHint
```

## Edge cases and limitations

- **Already-running sessions** (no `@codeherd_branch`): correlated by the
  folder-name / config key (covers `__worktrees/` and clone-dir sessions);
  diverged display falls back to the folder name or `config.DefaultBranch`.
- **Clone-dir session, `config.DefaultBranch` unset, while diverged:** the one
  residual gap. Without the configured default branch the clone-dir identity
  can only fall back to the live branch, which is empty when detached. Setting
  `default_branch` in project config closes it; new sessions also carry
  `@codeherd_branch`. Narrow and non-blocking.
- **Session-less diverged worktree:** shows the identity branch + hint; there
  is no session to lose.

## Item action safety

Existing item actions (attach, delete worktree, create session) are unaffected
by the display-branch change. Attach uses the stable tmux `session_id`
(`AgentSessionID` / `ShellSessionID`), not the branch. Delete/create resolve the
worktree via `WorktreePath`, which flattens whatever branch form is displayed —
both `feature/foo` and the folder fallback `feature-foo` resolve to the same
`__worktrees/feature-foo` directory.

## Testing

TDD; must keep aggregate coverage ≥ 80% (`make check`).

- `semconv`: `WorktreeIdentityBranch` for `__worktrees/` (folder name), clone
  dir with `DefaultBranch` set, and clone dir falling back to live branch;
  `FlattenBranch` idempotency.
- `worktree`: `parseWorktreePorcelain` sets `Detached` on detached blocks;
  existing detached-branch-empty assertion still holds.
- `session`: `Start` stamps `@codeherd_branch`.
- `tmux`: `ListSessions` parses the new `Branch` field.
- `tui buildItems` (regression for the bug):
  - detached `__worktrees/` worktree with a running agent/shell session still
    correlates by folder name;
  - checked-out-other-branch worktree still correlates;
  - clone-dir worktree correlates via `config.DefaultBranch` (and falls back to
    live branch when unset);
  - `HeadHint` values: `detached`, `on <branch>`, empty;
  - display branch uses `@codeherd_branch` when present, folder / config name
    otherwise.
- `delegate`: renders the hint suffix when `HeadHint` is set.
