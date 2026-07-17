# Backward compatibility for pre-`@codeherd_project` sessions

**Status:** design
**Date:** 2026-07-16
**Related:** `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` §14.1 (behaviour change #7 and the "What Plan 2 inherits" hardening note this design resolves).

## 1. Problem

The herd collapse (Plan 1) added a new tmux session option, `@codeherd_project`, stamped at launch and used to rebuild a session's `Ref`. Sessions created by any earlier version carry every other option — including the frozen `@codeherd_canonical_name` — but not this one.

The new binary lists and kills sessions by rebuilding a canonical name from `Ref` parts (`hd.Ref.CanonicalName()`), which is derived from `@codeherd_profile` + `@codeherd_project` + `@codeherd_branch`. For a pre-upgrade session the project part is empty, so the rebuilt name never equals the session's real, stored name. Consequences:

- The session is **dropped from the TUI/`list`** (its rebuilt key matches no workspace in the `List` join).
- The session **survives `delete worktree`** — `StopSessions` compares the front end's real-project `Ref` against the session's empty-project rebuilt name, they never match, so the worktree is force-removed while the agent process keeps running. This is the original orphan defect, resurfacing only for sessions live across the upgrade boundary.

The `SessionRecord.Project` doc comment claims such a session "fails loudly (`project "" is not configured`)" in `Teardown`. It does not: no path feeds a Handle's own empty-project `Ref` to a path resolver, so that guard never fires. The behaviour is a silent orphan, not a loud failure.

## 2. Root cause

Every version back to `29f11be` stamped `@codeherd_canonical_name` with the fully-qualified name `SessionName(profile, project, branch)` and **matched sessions on that stored value**. The collapse stopped trusting the stored name and started rebuilding it from parts. Rebuilding is fragile by construction: any option that feeds the rebuild (today the project; tomorrow anything else) becomes load-bearing for matching, so adding one silently breaks every session that predates it.

## 3. Principle

**The stored `@codeherd_canonical_name` is the session's identity of record.** It is frozen at creation and is the only correct key for matching a live session. Matching keys on it; it is never rebuilt from `Ref` parts. `Ref` remains the input front ends supply and the value used to *derive* paths and names — but not the key used to *match* an already-running session.

This restores parity with every prior version and is inherently forward-safe: a future added option cannot break matching, because matching never depends on the parts.

## 4. Design

Three layers, separated by the strength of guarantee each provides. Layer 1 is the correctness guarantee; layers 2–3 are best-effort recovery that improves display and heals the state permanently.

### Layer 1 — match on the stored canonical (the guarantee)

`Handle` gains a field:

```go
type Handle struct {
    ID        string
    Canonical string // @codeherd_canonical_name — the frozen identity, the match key
    Ref       Ref
    // …existing fields…
}
```

`Canonical` is set from `r.CanonicalName`. The three match sites switch from the rebuilt name to the stored one:

| Site | Was | Becomes |
|---|---|---|
| `Resolve` (session.go) | `hd.Ref.CanonicalName() == canonical` | `hd.Canonical == canonical` |
| `StopSessions` (session.go) | `hd.Ref.CanonicalName() != canonical` | `hd.Canonical != canonical` |
| `List` join (workspace.go) | `key := hd.Ref.CanonicalName()` | `key := hd.Canonical` |

On the front-end side, the comparison value stays `ref.CanonicalName()` — front ends always hold a complete, real-project `Ref`, so their rebuilt name is correct. For a **new** session, `hd.Canonical == hd.Ref.CanonicalName()`, so this change is a no-op. For a **pre-upgrade** session, the stored canonical is the real name and matches. `Sessions()`'s profile filter (`hd.Ref.Profile == h.profile`) is unchanged — `@codeherd_profile` is stored on pre-upgrade sessions, so the filter already works.

**After layer 1 alone, listing and killing are correct regardless of whether the project is ever recovered.**

### Layer 2 — recover the project (best-effort, for display)

A pure, independently testable helper reconstructs the missing project by iterate-and-validate against configured projects:

```go
// resolveProject finds the configured project whose canonical name matches the
// stored one, given the (stored) profile and branch. It disambiguates by
// validating against real config rather than string-splitting the name, so a
// project that no longer exists in config yields "", false.
func resolveProject(cfg *config.Config, profile, branch, canonical string) (string, bool) {
    for name := range cfg.Projects {
        if semconv.SessionName(profile, name, branch) == canonical {
            return name, true
        }
    }
    return "", false
}
```

Profile and branch are both stored on the record, so the only unknown is the project, and the match is unambiguous. Iterating and validating (rather than stripping the prefix/suffix) guarantees we never recover a name that isn't a real project — which matters because layer 3 writes it back.

### Layer 3 — self-heal (write once, so the cost is paid once)

When layer 2 recovers a project, the live session is re-stamped so it becomes first-class permanently and the recovery runs only once. This write lives in the same single place as layers 1–2 (see Reusability) and carries a comment stating it is a backward-compatibility shim:

```go
// Backward compatibility: sessions created before @codeherd_project existed
// carry no project stamp. Recover it from the frozen canonical name and stamp
// it, so the session heals to first-class on first observation. Idempotent —
// once stamped, future reads take the value directly and skip this path.
if r.Project == "" && r.CanonicalName != "" {
    if project, ok := resolveProject(h.cfg, r.Profile, r.Branch, r.CanonicalName); ok {
        hd.Ref.Project = project
        _ = h.tmux.SetOption(r.Name, semconv.TmuxOptionProject, project)
    }
}
```

The heal happens **anywhere a session is observed**, including read-only paths like `ch list session`. This is a deliberate choice: it heals as early and as broadly as possible, the write is idempotent and benign, and it keeps the logic in one place rather than special-casing which callers may mutate. If layer 2 fails to recover a project (project removed from config), no write happens and the session still lists and kills via layer 1, with a blank project column.

### Reusability — one place, no duplication

All recovery and healing lives in the single `handles()` chokepoint, which is already documented as "the single place a tmux record becomes a Handle." Every consumer funnels through it:

- `Resolve`, `Sessions`, `StopSessions` → `h.handles()` directly
- `List`'s join → `h.Sessions()` → `h.handles()`
- `Teardown`'s pre-check → `h.handles()`

`handleFrom` becomes a method (`h.handleFrom(r) Handle`) so it can read `h.cfg` and call `h.tmux.SetOption`; `handles()` calls it per record. The recovery arithmetic is the pure `resolveProject` helper (layer 2), unit-testable in isolation; the heal is the one `SetOption` call inside `h.handleFrom`. There is exactly one implementation of each, reached by every path.

## 5. Testing

- **`resolveProject` unit tests** — recovers the right project under a profile, under no profile, returns `false` when no configured project matches, and is unambiguous when two projects share a branch name but differ in project name.
- **`handles()` recovery + heal test** — a `fakeTmux` row with empty `Project` but a real stored `CanonicalName` yields a `Handle` with `Ref.Project` recovered, `Canonical` set, and a `SetOption(@codeherd_project, …)` call recorded. A second `handles()` call over an already-stamped row issues no further `SetOption` (idempotence).
- **Compat regression test (the reason this exists)** — the mirror of Plan 1's `TestConfirmDeleteAll_divergedHeadSessionIsKilled`: a pre-upgrade session (row with real `CanonicalName`, empty `Project`) under an active profile is killed by `StopSessions`/`Teardown`, matched by ID via the stored canonical. Names the compat guarantee explicitly.
- **Forward-safety assertion** — a test documenting that a `Handle` created from a record with a project matches identically to one without, once healed, so a future added option cannot regress matching.

Tmux-touching tests isolate per the repo convention (`CODEHERD_TMUX_SOCKET` under `t.TempDir()`, clear `$TMUX`, probe-and-skip, `kill-server` cleanup).

## 6. Documentation changes

- **`SessionRecord.Project` comment** (`internal/tmux/client.go`) — remove the inaccurate "fails loudly (`project "" is not configured`)" claim; state that pre-upgrade records carry no project and that `internal/herd` recovers and heals it from the canonical name on observation.
- **Spec §14.1 behaviour change #7** — rewrite: pre-upgrade sessions are now recognized, listed, killed, and healed on first observation (drop the "survives teardown" caveat). This ships as the changelog line for the compat release.
- **Spec §14.1 "What Plan 2 inherits"** — remove the stored-canonical hardening note; it is done here.

## 7. Scope and non-goals

- **In scope:** `internal/herd` (session.go `Handle`/`handleFrom`/`handles`/`Resolve`/`StopSessions`, workspace.go `List` join), a `resolveProject` helper, tests, and the three doc edits. One focused change.
- **Out of scope / non-goals:** No new CLI command (healing is automatic, not a `migrate` verb). No change to how new sessions are stamped. Independent of Plan 2's front-end thinning and Plan 3's integration matrix — this can land before or after either.
- **Boundary condition:** a session whose project was removed from config after the session started lists with a blank project and is not healed, but is still killed correctly. Acceptable — there is no project to heal to.
