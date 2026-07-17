# Front-End Thinning (Plan 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give codeherd's CLI one error vocabulary — a single translator that returns (never `os.Exit`s) — and teach the TUI to match the same `herd` sentinels instead of printing raw internal error strings.

**Architecture:** `internal/herd` already owns the whole sentinel vocabulary (`internal/herd/errors.go`). This plan finishes the front-end half of §9: `cmd/errors.go`'s two translators (`worktreeErr`/`sessionErr`) collapse into one `herdErr` that **returns** a user-facing error and lets `Execute` print it and `main` set the exit code; and the TUI gains a `humanize(err)` that maps the same sentinels to concise status lines. `herd` still never formats user-facing text — presentation stays in each front end.

**Tech Stack:** Go, Cobra (CLI), Bubble Tea v2 (TUI). No new dependencies.

## Global Constraints

Copied from `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` (§9) and `CLAUDE.md`. Every task's requirements implicitly include this section.

- **One error vocabulary.** Front ends match `herd` sentinels and nothing else; all sentinels already live in `internal/herd/errors.go`. `herd` never formats user-facing text — presentation stays in each front end. (§9)
- **`herdErr` returns, never exits.** The translator returns an error; `Execute` prints it and `main` exits non-zero. Printing + `os.Exit(1)` inside `RunE` is the wart being removed — it made the trailing `return nil` unreachable and bypassed `Execute`'s error path. (§9)
- **`make check` gates every task** — 80% aggregate coverage floor, integration tests, lint (`golangci-lint`), build. Run it before marking a task complete.
- **`wrapcheck` is active on production code** (`cmd/`, `internal/tui/`), disabled for `_test.go`. Returning a freshly created `fmt.Errorf(...)` or a local `err` variable is allowed; returning an unwrapped error straight from another package's call in the `return` statement is not.
- **`staticcheck` runs all checks (`ST1005` included).** Error strings passed to `errors.New`/`fmt.Errorf` must not start with a capital letter and must not end with `.`, `:`, or `!`. (TUI status strings are plain `string`, not errors — `ST1005` does not apply to them.)
- **`goimports` with local-prefix `github.com/xico42/codeherd`** — run `gofmt`/`goimports` after edits so unused imports are dropped and grouping is correct. `make lint` enforces it.
- **TDD, frequent commits.** RED (failing/uncompilable test) → GREEN (minimal implementation) → refactor → commit per task.

---

## Scope reconciliation (read before starting)

The spec's §12 sketched Plan 2 as "~600 fewer lines across the front ends." **Most of that thinning already landed in Plan 1** (see §14.1). Verified current state:

- `cmd/services.go` and its `*ForProfile` shims — **already deleted** (with `activeProfile()`).
- `Model.sesSvc` / `Model.projSvc` dead fields — **already gone**.
- The TUI `profileCache` — **already deleted**; `switchProfile` calls `m.herd.WithProfile(next)`.
- Front-end line counts are **already at or near the §8.5 targets** (`cmd/session.go` 275, `cmd/worktree.go` 175, `internal/tui/actions.go` 279, `internal/tui/model.go` 578). There is no bulk of dead code left to remove.

What genuinely remains from §9 is the **error-vocabulary work**, and that is this plan:

1. Collapse `cmd/errors.go`'s two translators into one and remove `os.Exit` from `RunE` (Task 1).
2. Teach the TUI to match `herd` sentinels instead of printing raw errors (Task 2).

### Measured non-goals (deliberately out of scope)

Two items §14.1 flagged for Plan 2 were measured and **intentionally kept**:

- **`Model.cfg` stays.** It is a redundant cache of `m.herd.Config()`, but ~30 TUI test sites build `Model{cfg: cfg}` *without* a herd and read config through the field (plus `model_export_test.go`'s `CurrentConfigForTest()`). Removing it forces every one of those tests to construct a real `Herd` — the exact "ceremony that suppresses tests" §11 rejected when it kept `Ref`'s fields exported. The field is manually resynced at the only two sites that swap the herd (`NewModel`, `switchProfile`), so drift risk is negligible. §14.1's own guidance: "if the per-keypress read ever bites, the cache belongs on `Herd`, not the TUI" — it has not bitten. Keep it.
- **The inline `tmux.NewClient` sites stay.** `cmd/session.go`'s `execTmuxAttach` and the TUI's `switchClientCmd` build a tmux client only to call `SwitchClient` for **in-place interactive attach**. The design deliberately keeps interactive attach in the front ends (cf. `syscall.Exec`; `SetStatus` is the *only* name-addressed escape hatch into `herd`). These are exec mechanism for attach, not the dead service-injection smell §3.2 was about. Leave them.

`cmd/template.go` keeps its own `hooks.New` and direct `herdtemplate` call — §14.1 already ruled this the one front end that legitimately does not route through `herd`. Untouched here.

---

## File Structure

| File | Change | Responsibility after |
|---|---|---|
| `cmd/errors.go` | Rewrite | One translator, `herdErr(project, branch string, err error) error`, that returns friendly errors. No printing, no `os.Exit`. |
| `cmd/root.go` | Modify (`Execute`) | Print the returned error once, prefixed `Error: `, to `os.Stderr`; return non-nil so `main` exits 1. |
| `cmd/session.go` | Modify (6 call sites) | `return herdErr(project, branch, err)` at each translator call. |
| `cmd/worktree.go` | Modify (3 call sites) | `return herdErr(project, branch, err)` at each translator call. |
| `cmd/errors_internal_test.go` | Create | Unit tests for `herdErr` — one per sentinel + the `SessionExistsError` path + default passthrough. |
| `internal/tui/errors.go` | Create | `humanize(err error) string` — maps `herd` sentinels to concise status lines; default falls through to `err.Error()`. |
| `internal/tui/model.go` | Modify (2 render sites) | `m.statusMsg = humanize(msg.err)` and `m.remotePicker.errText = humanize(msg.err)`. |
| `internal/tui/errors_internal_test.go` | Create | Unit tests for `humanize` — one per sentinel + `SessionExistsError` + `nil` + default. |

Two independently reviewable tasks; a reviewer could reject the TUI change while approving the CLI change or vice versa.

---

### Task 1: Unify the CLI error translators into `herdErr` and remove `os.Exit` from `RunE`

**Files:**
- Modify: `cmd/errors.go` (full rewrite of both functions into one)
- Modify: `cmd/root.go:81-89` (`Execute`)
- Modify: `cmd/session.go:98,155,175,217,233,262` (call sites)
- Modify: `cmd/worktree.go:110,126,170` (call sites)
- Test: `cmd/errors_internal_test.go` (create)

**Interfaces:**
- Consumes (already exist in `internal/herd/errors.go`): sentinels `herd.ErrNotCloned`, `herd.ErrWorktreeExists`, `herd.ErrWorktreeNotFound`, `herd.ErrSessionRunning`, `herd.ErrSessionExists`, `herd.ErrSessionNotFound`, `herd.ErrPathNotFound`; typed error `*herd.SessionExistsError` with fields `Ref herd.Ref` (`.Project`, `.Branch`) and `Type herd.SessionType`.
- Produces: `func herdErr(project, branch string, err error) error` in package `cmd`. Replaces `worktreeErr(cmd, project, branch, err)` and `sessionErr(cmd, err)`, both of which are deleted.

**Context — why this shape:**
`herdErr` drops the `*cobra.Command` parameter because it no longer prints; it only builds and returns the error. `Execute` is the single print site. `runCmd` (the test harness) returns `Execute`'s error, so every friendly message becomes assertable via `err.Error()` — the coverage the old `os.Exit(1)` made impossible (see the comments in `cmd/session_test.go:88,109-112` that deliberately steered around the exiting branches).

- [ ] **Step 1: Write the failing unit tests**

Create `cmd/errors_internal_test.go`:

```go
package cmd

import (
	"errors"
	"testing"

	"github.com/xico42/codeherd/internal/herd"
)

func TestHerdErr_notCloned(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrNotCloned)
	want := "myapp is not cloned. Run 'ch clone project myapp' first"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_worktreeExists(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrWorktreeExists)
	want := "worktree myapp/feat already exists"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_worktreeNotFound(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrWorktreeNotFound)
	want := "worktree myapp/feat not found. Run 'ch create worktree myapp feat' first"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_sessionRunning(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrSessionRunning)
	want := "session myapp-feat is running. Stop it first or use --force"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_sessionExists_carriesRefFromTypedError(t *testing.T) {
	se := &herd.SessionExistsError{
		Ref:  herd.Ref{Project: "myapp", Branch: "feat"},
		Type: herd.SessionTypeAgent,
	}
	got := herdErr("ignored", "ignored", se)
	want := "session myapp/feat (agent) already exists. Attach with 'ch attach session myapp feat'"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_unknownSentinel_passesThrough(t *testing.T) {
	raw := errors.New("boom")
	got := herdErr("myapp", "feat", raw)
	if got != raw {
		t.Fatalf("herdErr() = %v, want the original error %v", got, raw)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/ -run TestHerdErr`
Expected: FAIL — `undefined: herdErr` (compile error; the function does not exist yet).

- [ ] **Step 3: Rewrite `cmd/errors.go` as the single translator**

Replace the entire contents of `cmd/errors.go` with:

```go
package cmd

import (
	"errors"
	"fmt"

	"github.com/xico42/codeherd/internal/herd"
)

// herdErr is the CLI's single error translator. Every command funnels its
// domain errors through here, matching herd sentinels and nothing else
// (the one error vocabulary — herd owns the sentinels, the front end owns
// the presentation).
//
// It RETURNS the error rather than printing and calling os.Exit. Execute
// prints it once (prefixed "Error: ") and main exits non-zero. The previous
// shape printed and called os.Exit(1) inside RunE, which made the trailing
// `return nil` unreachable and bypassed Execute's error path.
//
// project and branch supply context for the friendly messages; pass the
// identity values in scope at the call site (or ws.Ref.Project /
// ws.Ref.Branch). For ErrSessionExists the context is read from the typed
// *herd.SessionExistsError instead, so the two positional args are ignored
// on that branch.
func herdErr(project, branch string, err error) error {
	switch {
	case errors.Is(err, herd.ErrNotCloned):
		return fmt.Errorf("%s is not cloned. Run 'ch clone project %s' first", project, project)
	case errors.Is(err, herd.ErrWorktreeExists):
		return fmt.Errorf("worktree %s/%s already exists", project, branch)
	case errors.Is(err, herd.ErrWorktreeNotFound):
		return fmt.Errorf("worktree %s/%s not found. Run 'ch create worktree %s %s' first", project, branch, project, branch)
	case errors.Is(err, herd.ErrSessionRunning):
		return fmt.Errorf("session %s-%s is running. Stop it first or use --force", project, branch)
	case errors.Is(err, herd.ErrSessionExists):
		var sesErr *herd.SessionExistsError
		if errors.As(err, &sesErr) {
			return fmt.Errorf("session %s/%s (%s) already exists. Attach with 'ch attach session %s %s'",
				sesErr.Ref.Project, sesErr.Ref.Branch, sesErr.Type, sesErr.Ref.Project, sesErr.Ref.Branch)
		}
		return err
	default:
		return err
	}
}
```

Note: `ErrSessionNotFound` and `ErrPathNotFound` need no explicit branch — the raw domain error already carries a clear message (`"session not found: ..."`, `"worktree path not found: ..."`), so `default: return err` handles them exactly as the old `sessionErr` did (it merely reprinted the raw error for those cases). No leading-capital / trailing-punctuation in any new string → `ST1005` clean.

- [ ] **Step 4: Update `Execute` to print the returned error, prefixed**

In `cmd/root.go`, change the `Execute` body (currently lines 81-89):

```go
func Execute(version string) error {
	resetAllFlags(rootCmd)
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return fmt.Errorf("%w", err)
	}
	return nil
}
```

Only the print line changes: `fmt.Fprintln(os.Stderr, err)` → `fmt.Fprintln(os.Stderr, "Error:", err)`. This restores the `Error: ` affordance the old translators printed, now centralized for *every* user-facing error (config-load errors included). `main.go` already exits 1 on a non-nil return — unchanged.

- [ ] **Step 5: Update the 9 call sites**

In `cmd/session.go`, replace each translator call with `herdErr` (all sites have `project, branch` in scope):
- line 98 (`ShowSessionCmd.Run`): `return sessionErr(cmd, err)` → `return herdErr(project, branch, err)`
- line 155 (`CreateSessionCmd.Run`): `return worktreeErr(cmd, project, branch, err)` → `return herdErr(project, branch, err)`
- line 175 (`CreateSessionCmd.Run`): `return sessionErr(cmd, err)` → `return herdErr(project, branch, err)`
- line 217 (`DeleteSessionCmd.Run`): `return sessionErr(cmd, err)` → `return herdErr(project, branch, err)`
- line 233 (`DeleteSessionCmd.Run`): `return sessionErr(cmd, err)` → `return herdErr(project, branch, err)`
- line 262 (`AttachSessionCmd.Run`): `return sessionErr(cmd, err)` → `return herdErr(project, branch, err)`

In `cmd/worktree.go`:
- line 110 (`CreateWorktreeCmd.Run`): `return worktreeErr(cmd, project, posBranch, err)` → `return herdErr(project, posBranch, err)`
- line 126 (`CreateWorktreeCmd.Run`, the `--attach` block): `return sessionErr(cmd, err)` → `return herdErr(ws.Ref.Project, ws.Ref.Branch, err)` (use `ws.Ref` — `Track` may have derived a different local branch; it is the authoritative identity here)
- line 170 (`DeleteWorktreeCmd.Run`): `return worktreeErr(cmd, project, branch, err)` → `return herdErr(project, branch, err)`

- [ ] **Step 6: Run `gofmt`/`goimports` and the package tests**

Run: `gofmt -w cmd/errors.go cmd/root.go cmd/session.go cmd/worktree.go && go test ./cmd/`
Expected: PASS. The new `TestHerdErr_*` pass; the pre-existing session/worktree tests still pass (the ones that previously steered around `os.Exit` now flow through `default: return err` exactly as before). `cmd/errors.go` no longer imports `os` or `github.com/spf13/cobra`; `goimports` drops them.

- [ ] **Step 7: Run the full gate**

Run: `make check`
Expected: green — coverage ≥80% (Task 1 adds directly-testable error paths, nudging `cmd` coverage up), integration passes, lint clean, build OK.

- [ ] **Step 8: Commit**

```bash
git add cmd/errors.go cmd/root.go cmd/session.go cmd/worktree.go cmd/errors_internal_test.go
git commit -m "refactor(cmd): one error translator, no os.Exit in RunE

Collapse worktreeErr/sessionErr into a single herdErr that RETURNS a
user-facing error instead of printing and calling os.Exit(1) inside RunE.
Execute prints it once (prefixed \"Error: \") and main sets the exit code.
This makes the friendly sentinel messages returnable and therefore
testable — the paths the old os.Exit made impossible to cover.

Behaviour: all user-facing errors (config-load included) now print with a
consistent \"Error: \" prefix via Execute; exit codes unchanged.

Refs spec §9.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Teach the TUI to match `herd` sentinels instead of rendering raw errors

**Files:**
- Create: `internal/tui/errors.go`
- Modify: `internal/tui/model.go:232` and `internal/tui/model.go:262`
- Test: `internal/tui/errors_internal_test.go` (create)

**Interfaces:**
- Consumes: the same `herd` sentinels and `*herd.SessionExistsError` as Task 1.
- Produces: `func humanize(err error) string` in package `tui`.

**Context — why this shape:**
Today the dashboard sets `m.statusMsg = msg.err.Error()` (model.go:232) and `m.remotePicker.errText = msg.err.Error()` (model.go:262), leaking raw internal strings like `"project not cloned"`. §9: "The TUI stops rendering raw errors … One vocabulary lets the TUI match the same sentinels." `humanize` is TUI-local presentation (the return is a plain status `string`, so `ST1005` does not apply — sentences with capitals and periods are fine). Unlike the CLI translator, the TUI `errMsg` carries no project/branch, so context-free messages are used except for `*herd.SessionExistsError`, which carries its own `Ref`.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/tui/errors_internal_test.go`:

```go
package tui

import (
	"errors"
	"testing"

	"github.com/xico42/codeherd/internal/herd"
)

func TestHumanize_nil(t *testing.T) {
	if got := humanize(nil); got != "" {
		t.Fatalf("humanize(nil) = %q, want empty", got)
	}
}

func TestHumanize_sentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"notCloned", herd.ErrNotCloned, "Project is not cloned — clone it first."},
		{"worktreeExists", herd.ErrWorktreeExists, "Worktree already exists."},
		{"worktreeNotFound", herd.ErrWorktreeNotFound, "Worktree not found."},
		{"sessionNotFound", herd.ErrSessionNotFound, "No such session."},
		{"sessionRunning", herd.ErrSessionRunning, "Session is running — stop it first."},
		{"pathNotFound", herd.ErrPathNotFound, "Worktree path not found."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanize(tc.err); got != tc.want {
				t.Fatalf("humanize(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestHumanize_sessionExists_usesRef(t *testing.T) {
	se := &herd.SessionExistsError{
		Ref:  herd.Ref{Project: "myapp", Branch: "feat"},
		Type: herd.SessionTypeAgent,
	}
	want := "Session myapp/feat (agent) already exists."
	if got := humanize(se); got != want {
		t.Fatalf("humanize() = %q, want %q", got, want)
	}
}

func TestHumanize_unknown_passesThrough(t *testing.T) {
	raw := errors.New("some raw failure")
	if got := humanize(raw); got != "some raw failure" {
		t.Fatalf("humanize() = %q, want the raw message", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestHumanize`
Expected: FAIL — `undefined: humanize` (compile error).

- [ ] **Step 3: Create `internal/tui/errors.go`**

```go
package tui

import (
	"errors"
	"fmt"

	"github.com/xico42/codeherd/internal/herd"
)

// humanize maps a herd domain error to a concise, user-facing status line for
// the TUI. It matches the same herd sentinels the CLI translator does (one
// error vocabulary) so the dashboard stops surfacing raw internal error
// strings. Unknown errors fall through to their own message.
//
// The TUI errMsg carries no project/branch, so most messages are
// context-free; ErrSessionExists is the exception — its typed error carries
// the Ref, so the message names the session.
func humanize(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, herd.ErrNotCloned):
		return "Project is not cloned — clone it first."
	case errors.Is(err, herd.ErrWorktreeExists):
		return "Worktree already exists."
	case errors.Is(err, herd.ErrWorktreeNotFound):
		return "Worktree not found."
	case errors.Is(err, herd.ErrSessionExists):
		var se *herd.SessionExistsError
		if errors.As(err, &se) {
			return fmt.Sprintf("Session %s/%s (%s) already exists.", se.Ref.Project, se.Ref.Branch, se.Type)
		}
		return "Session already exists."
	case errors.Is(err, herd.ErrSessionNotFound):
		return "No such session."
	case errors.Is(err, herd.ErrSessionRunning):
		return "Session is running — stop it first."
	case errors.Is(err, herd.ErrPathNotFound):
		return "Worktree path not found."
	default:
		return err.Error()
	}
}
```

- [ ] **Step 4: Wire the two render sites in `internal/tui/model.go`**

At line 232 (the `errMsg` case):

```go
	case errMsg:
		m.busy = ""
		if msg.err != nil {
			m.statusMsg = humanize(msg.err)
		}
		return m, nil
```

At line 262 (the `remoteBranchesMsg` error branch):

```go
			if msg.err != nil {
				m.remotePicker.errText = humanize(msg.err)
```

Both change only `msg.err.Error()` → `humanize(msg.err)`.

- [ ] **Step 5: Run the package tests**

Run: `gofmt -w internal/tui/errors.go internal/tui/model.go && go test ./internal/tui/`
Expected: PASS — `TestHumanize_*` pass and the existing TUI suite is unaffected (raw-string assertions, if any, only covered non-sentinel errors, which still pass through `default`).

- [ ] **Step 6: Run the full gate**

Run: `make check`
Expected: green — coverage ≥80%, integration, lint, build all OK.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/errors.go internal/tui/model.go internal/tui/errors_internal_test.go
git commit -m "feat(tui): humanize herd sentinels instead of raw errors

Add humanize(err) mapping the herd sentinel vocabulary to concise status
lines, and route the dashboard's two error-render sites (statusMsg and the
remote-picker errText) through it. The TUI no longer surfaces raw internal
error strings; it matches the same sentinels the CLI does.

Refs spec §9.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage (§9 — the only section Plan 2 owns):**
- "One vocabulary. All sentinels move to `herd`" — done in Plan 1; Task 1 consumes them. ✅
- "`cmd/errors.go` has two translators … One package yields one translator" — Task 1 collapses `worktreeErr`+`sessionErr` → `herdErr`. ✅
- "Fix the `os.Exit` wart … becomes a translator that returns an error and lets Cobra exit" — Task 1 removes both `os.Exit(1)`; `Execute` prints, `main` exits. ✅
- "The TUI stops rendering raw errors … match the same sentinels" — Task 2 `humanize` at both render sites. ✅
- "`herd` never formats user-facing text" — preserved; all presentation is in `cmd`/`tui`. ✅
- §12 "no front end constructs a service or builds a session name" — already true after Plan 1 (verified in Scope reconciliation); the two inline `tmux.NewClient` attach sites are exec mechanism, not services (measured non-goal). ✅

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Every code step shows complete code. ✅

**3. Type consistency:**
- `herdErr(project, branch string, err error) error` — signature identical in the interface block, the implementation (Step 3), and all 9 call sites (Step 5). ✅
- `humanize(err error) string` — identical in interface block, implementation, tests, and both wiring sites. ✅
- `*herd.SessionExistsError` field access (`.Ref.Project`, `.Ref.Branch`, `.Type`) matches `internal/herd/errors.go`. ✅
- `herd.SessionTypeAgent` renders as `"agent"` (`= semconv.SessionTypeAgent`, herd.go:28), matching the expected strings in both tasks' tests. ✅

**Behaviour changes to record in the branch handoff (§14.2):**
1. All user-facing CLI errors now print with a consistent `Error: ` prefix via `Execute` (previously only the two translators did; config-load and other `RunE` errors gained it). Exit codes unchanged.
2. The sentinel-branch friendly messages are now returned (hence testable) rather than printed-then-`os.Exit`ed. `err.Error()` carries the friendly text without the `Error: ` prefix (the prefix is added only at print time in `Execute`).
3. The TUI renders humanized status text for `herd` sentinels instead of raw error strings.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-16-front-end-thinning.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute both tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**

After execution, record findings in the spec's §14.2 ("After Plan 2 — front-end thinning") before Plan 3, then offer `superpowers:finishing-a-development-branch`.
