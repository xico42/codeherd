# Checkout a remote branch into a new worktree

**Status:** Design approved
**Date:** 2026-06-27
**Scope:** `worktree` resource, across CLI and TUI

## Problem

To review someone else's PR, you want a worktree that has the PR's branch
checked out — fetched fresh from the remote, tracking it, under the same name.
Today `codeherd` cannot do this:

- There is **no `git fetch` anywhere** in the codebase. After the initial
  clone, remote-tracking refs go stale.
- `create worktree --from <ref>` always creates a **new, untracked** branch
  from a start point, and passes the ref straight to `git worktree add` with no
  fetch — so `--from origin/feat-x` only works if that ref already happens to be
  present locally.

This design adds the ability to fetch and check out a remote branch into a new
tracking worktree, and — as a coherent extension — makes **all** worktree
creation start from an up-to-date source.

## Core behavior

### Checking out a remote branch (`--track`)

Fetch a branch from a remote, then create a worktree whose local branch
**tracks** that remote branch, using the **same name** by default:

```
git fetch <remote> <branch>
git worktree add --track -b <branch> <worktree-path> <remote>/<branch>
```

- Default remote is `origin` and is omittable.
- A ref is parsed as `[<remote>/]<branch>`. The leading segment is treated as a
  remote **only if it matches an actual configured remote** (from `git remote`);
  otherwise the whole ref is the branch on `origin`. This keeps branch names
  containing slashes (`feature/login`) working.
- Distinct from `--from`, which creates a *new, untracked* branch from a start
  point. `--track` and `--from` are **mutually exclusive** — supplying both is an
  error.

### Fresh source on every creation

Worktree **creation** (default / `--from` / `--track`) starts from an
up-to-date source. List and delete never fetch.

A plain `git fetch origin` updates the remote-tracking ref (`origin/main`) but
does **not** move local `main`. So for a source branch that exists on a remote
(the common case):

1. **Fetch** the source's remote (default `origin`).
2. **Fast-forward the local source branch** as a courtesy:
   - source branch *not* checked out in the clone →
     `git fetch <remote> <src>:<src>` (updates the local ref only if it is a
     fast-forward; harmless no-op otherwise).
   - source branch *is* the clone's checked-out branch →
     `git merge --ff-only <remote>/<src>`.
3. **Base the new worktree branch off the remote-tracking ref** `<remote>/<src>`
   — so it is fresh even when step 2's fast-forward is skipped.
4. If the fast-forward cannot happen (diverged or dirty clone), **do not fail** —
   proceed with the fresh worktree off `<remote>/<src>` and surface a
   non-fatal notice. The local source branch just stays put.

For `--from` pointing at a **local-only ref** (tag, SHA, or a branch with no
upstream): use it directly as the start point, no fetch — there is nothing
remote to refresh.

For `--track <ref>`: the source *is* the remote branch, so step 2's local-source
fast-forward does not apply; fetch the remote branch and add the tracking
worktree.

## CLI surface

Add `--track <ref>` to `create worktree`:

```
ch create worktree myapp --track feat-x               # local feat-x  → origin/feat-x
ch create worktree myapp --track upstream/feat-x      # local feat-x  → upstream/feat-x
ch create worktree myapp myname --track origin/feat-x # local myname  → origin/feat-x
```

- Positional `<branch>` becomes **optional** when `--track` is given (the local
  name is derived from the ref); when provided, it **overrides** the derived
  local name.
- Composes with `--attach` and `--agent` exactly as today.
- Mutually exclusive with `--from`.
- Shell completion for `--track` suggests remote branches
  (`git for-each-ref refs/remotes`, skipping `*/HEAD`).

### Error handling

- **Fetch failure** (no such remote branch, network error) → surfaced verbatim
  with context, creation aborts.
- **Local branch already exists** (derived or overridden name) → clear error,
  stop. No silent reuse.
- **`--from` + `--track` together** → validation error.

## `internal/worktree` changes

Extend the `WorktreeRunner` interface:

```go
type WorktreeRunner interface {
    // existing
    Add(cloneDir, worktreePath, branch string) error
    AddNewBranch(cloneDir, worktreePath, branch string) error
    AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
    Remove(cloneDir, worktreePath string) error
    List(cloneDir string) ([]WorktreeInfo, error)

    // new
    Fetch(cloneDir, remote, branch string) error            // git fetch <remote> <branch>
    FetchAll(cloneDir string) error                         // git fetch --all --prune
    FastForward(cloneDir, remote, branch string) error      // FF local <branch> to <remote>/<branch>
    Remotes(cloneDir string) ([]string, error)              // git remote
    ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)
    AddTracking(cloneDir, worktreePath, branch, remoteRef string) error // git worktree add --track -b ...
}
```

New types (live in `internal/worktree`):

```go
type RemoteBranch struct {
    Remote string // e.g. "origin"
    Branch string // e.g. "feature/login"
    Ref    string // e.g. "origin/feature/login"
}
```

New `Service` methods:

- `NewTracking(project, branch, ref string) (NewResult, error)` — resolve
  remote+branch (using `Remotes` to decide whether the leading segment is a
  remote), derive/override the local name, fetch the targeted branch, then
  `AddTracking`. Reuses the existing path/semconv resolution and the
  post-creation file-copy/template processing already applied by `New`/`NewFrom`.
- A freshness helper used by `New`/`NewFrom`/`NewTracking` that performs the
  fetch + best-effort fast-forward + remote-tracking base-point selection
  described in **Fresh source on every creation**. `FastForward` returning a
  "not fast-forwardable / dirty" condition is treated as non-fatal (notice, not
  error).

Ref resolution detail: `FastForward` for a non-checked-out branch is the
`git fetch <remote> <src>:<src>` form; for the checked-out branch it is
`git merge --ff-only`. The Service decides which based on the clone's current
branch. Distinguishing "FF skipped because diverged/dirty" (non-fatal) from a
real error is part of the helper's contract and must be unit-tested.

## TUI

Keybinding **`r`** — "checkout remote branch" — available from a project
row, or from a worktree/agent row (using that row's project). `r` was
previously bound to a manual dashboard refresh; that binding is **removed**
because the dashboard already auto-refreshes every 3 seconds, freeing `r` for
this feature.

The picker:

1. Run `FetchAll` for the project's clone, showing a transient `fetching…`
   state (blocking; simplest and always-fresh).
2. Show a **filterable list** of remote branches (`<remote>/<branch>`) from
   `ListRemoteBranches`. Empty list → friendly "no remote branches found (try
   again after pushing/fetching)" message.
3. On select → open the **existing create-worktree form, pre-filled**:
   - `branch` = derived local name (editable),
   - a read-only note `Tracks: <remote>/<branch>`,
   - the usual `attach` / `agent` fields.
4. Submit routes to `Service.NewTracking`.

Implementation notes:

- Add a small "remote branch list" model/view alongside the existing form.
- Thread a `tracksRef string` field through `formModel`; submission selects
  `NewTracking` when set, otherwise the existing `New` / `NewFrom` routing.
- The `r` binding is registered in `internal/tui/keys.go` and dispatched in
  `internal/tui/model.go` next to the existing `n` (new worktree) handling.

## Testing

- **`internal/worktree` unit tests** (mock `WorktreeRunner`):
  - ref parsing — `origin` default, explicit remote, branch names with slashes,
    leading segment that is *not* a configured remote.
  - fetch-then-add ordering for `NewTracking`.
  - local-name derivation and positional override.
  - "local branch already exists" error.
  - freshness helper: base-point is `<remote>/<src>`; FF attempted; FF
    skipped/diverged is non-fatal; local-only `--from` ref skips fetch.
- **CLI tests** (`cmd`): `--track` flag wiring, optional positional, `--track`
  + `--from` mutual-exclusion error, completion suggests remote branches.
- **TUI tests**: picker select → pre-filled form (`tracksRef` set, derived
  name) → routes to `NewTracking`; empty remote-branch list message.
- **Integration test** (real git + tmux, isolated `CODEHERD_TMUX_SOCKET` under
  `t.TempDir()`, `//go:build integration`): set up a clone with a remote branch,
  check it out end-to-end, assert the worktree exists and its branch tracks the
  remote.
- Keep aggregate coverage ≥ 80% (`make check`).

## Out of scope (YAGNI)

- GitHub PR-number resolution (`--pr 123` / `pull/123/head`).
- Configurable default remote name (hardcoded `origin`).
- Async/background picker refresh (blocking fetch on open is sufficient for now).
- Fetching before read-only or destructive worktree operations.
