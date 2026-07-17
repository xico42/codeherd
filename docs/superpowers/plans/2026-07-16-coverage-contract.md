# Coverage Contract (Plan 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill §10's profile × operation *integration* matrix — the three gap cells (`Resolve`, `StopSessions`, `Teardown` under an active profile) — with real-tmux integration tests at the `internal/herd` layer, plus their profile-off counterparts, so the matrix becomes a legible standing contract.

**Architecture:** One new build-tagged integration test file, `internal/herd/matrix_integration_test.go` (`package herd`), driving the real `Herd` API against a **real** tmux server (isolated per-test via `CODEHERD_TMUX_SOCKET`) and **real** git. A two-row table (`profile off` / `profile on`) runs every operation in both columns. This is the gate that would have caught the shipped defect: under a profile the pre-refactor code rebuilt a profile-blind session name and missed the kill.

**Tech Stack:** Go, real `tmux` binary (isolated socket), real `git` binary. No new dependencies. Test-only — **no production code changes**.

## Global Constraints

Copied from `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` (§10, §3.3, §12) and `CLAUDE.md`. Every task's requirements implicitly include this section.

- **The coverage contract (§10).** Every operation runs with profiles on *and* off. The three cells that are gaps today are `StopSessions`, `Teardown`, `Resolve` **under an active profile**; they have *unit* (fake-tmux) coverage already — this plan adds *integration* (real-tmux) coverage. This matrix is the gate that would have caught the original defect (§3.3: the bug lived in profile × {stop, delete, show}, "the quadrant nobody wrote").
- **This is a test-only, characterization plan.** No production file changes. The cycle is: write the test → run it under `-tags integration` → **expect PASS** (the contract holds). A FAILURE is *not* a test to fix — it is the real defect the matrix exists to catch; **stop and report it** (record under §14.3, per the handoff).
- **Build tag format is exact.** Line 1 is `//go:build integration`, line 2 blank, then `package herd`. No legacy `// +build` line (matches all five existing integration files).
- **Package is `herd`** (white-box), matching `internal/herd/integration_test.go`. That file — compiled together with this one under `-tags integration` — already declares `func runGit(t *testing.T, dir string, args ...string) string`. **Reuse it; do not redeclare it** (a second declaration is a compile error). Do not redeclare any existing `package herd` test identifier (`mkMyappWorktree`, `contains`, `cloneDirPath`, the `fakeGit`/`fakeTmux` fakes, etc.).
- **Real-tmux isolation (CLAUDE.md).** Set `CODEHERD_TMUX_SOCKET` (`tmux.SocketEnvVar`) to a path under `t.TempDir()` so the `internal/tmux` `RealRunner` prepends `-S <socket>` to every call; clear `$TMUX`; probe with a throwaway `tmux -S <socket> new-session` and `t.Skip` when it fails (missing binary / sandboxed CI); cleanup must `tmux -S <socket> kill-server` (which also reaps the `sleep` processes the sessions started). Never call bare `exec.Command("tmux", …)` without `-S <socket>`.
- **`make check` gates every task** — 80% aggregate coverage floor, integration tests, lint, build. The coverage phase runs `go test ./...` **without** the integration tag, so this file is invisible to it and cannot lower the coverage number; the new tests run in the `test-integration` phase (`go test -tags integration ./...`). Run `make check` before marking a task complete.
- **`goimports` local-prefix `github.com/xico42/codeherd`** — run `gofmt`/`goimports` after edits.

---

## Scope reconciliation (read before starting)

§12 sizes Plan 3 as: "The three gap cells — `StopSessions`, `Teardown`, `Resolve` under an active profile — are covered." Verified against the current tree:

- **Unit (fake-tmux) coverage already exists** for the profile-scoped operations: `TestStopSessions_underProfile_matchesProfileScopedSession` (`session_test.go:637`), `TestTeardown_underProfile_killsSessionsThenDeletesWorktree` (`workspace_test.go:724`), `TestList_underProfile_findsRunningSession`, `TestSessions_filtersByActiveProfile`. This is the "unit coverage here (Tasks 4–5)" §14.1 names.
- **Integration (real-tmux) coverage does not exist** for these under a profile. The only existing `internal/herd` integration test (`integration_test.go`) is real-git-only (`Deps{Tmux: nil, …}`) and calls `EnsureWorkspace` alone. The CLI-level `cmd/profiles_integration_test.go::TestProfiles_sessionIsolationAcrossProfiles` drives real tmux but covers only profile × {create, list}. **No test anywhere constructs `herd.New(…, Deps{Tmux: tmux.NewRealRunner(), …})`.**

So Plan 3 fills the *integration* column for the gap-cell operations at the `herd` layer, where §10's matrix names them. The two profile-off columns for these operations are included in the same tables (cheap, one `registry` parameter) so the file reads as the literal §10 matrix and guards the profile-off path from regression.

### Non-goals (deliberately out of scope)

- **`EnsureWorkspace` and `List` rows.** §10 marks both cells "covered" in both columns and neither is a gap; `List` profile-on is exercised by unit tests and behaviour change §14.1 #2. Not re-covered here (YAGNI). `EnsureWorkspace` is used only as *setup* (it creates the real worktree the other operations act on).
- **CLI-layer duplication.** `TestProfiles_sessionIsolationAcrossProfiles` already covers create+list through Cobra under a profile. This plan covers the mutation/query operations directly through the `Herd` API — the two together close §3.3's quadrant. No new `cmd_test` file.
- **Promoting the matrix into a `CLAUDE.md` standing rule** (a §14.3 prompt) — a post-execution decision, not a plan task.

---

## File Structure

| File | Change | Responsibility after |
|---|---|---|
| `internal/herd/matrix_integration_test.go` | Create | The §10 coverage contract: a `profile off`/`profile on` table, an isolated-real-tmux harness (`useIsolatedTmux`, `tmuxHasSession`, `setupMatrixHerd`), and one test per gap-cell operation (`Resolve`+`Launch`, `StopSessions`, `Teardown` force + non-force refuse), each run in both columns against real tmux + real git. |

One file, three tasks. Task 1 also carries the shared harness (the deliverable of every later task depends on it); a reviewer could reject any one operation's test while approving its neighbours.

---

### Task 1: Harness + the `Launch`/`Resolve` matrix rows

**Files:**
- Create: `internal/herd/matrix_integration_test.go`

**Interfaces:**
- Consumes (already exist): `func runGit(t *testing.T, dir string, args ...string) string` (declared in `internal/herd/integration_test.go`, same package + build tag); `herd.New`, `(*Herd).Ref`, `(*Herd).EnsureWorkspace`, `(*Herd).Launch`, `(*Herd).Resolve`, `Ref.CanonicalName()`, `SessionTypeAgent`, `EnsureOpts`, `LaunchOpts`, `Deps`; `tmux.NewRealRunner`, `tmux.SocketEnvVar`; `git.NewRealRunner`; `config.{Config,DefaultsConfig,ProjectConfig,AgentConfig,ProfileRegistry}`.
- Produces (later tasks rely on these, all in this file): the package-level var `matrixProfiles []struct{ name string; registry *config.ProfileRegistry }`; `func useIsolatedTmux(t *testing.T) string`; `func tmuxHasSession(t *testing.T, socket, name string) bool`; `func setupMatrixHerd(t *testing.T, registry *config.ProfileRegistry) (*Herd, Ref, string)` returning `(herd, identity-ref, worktree-path)`.

**Context — why this shape:**
`setupMatrixHerd` builds a Herd on *real* tmux + git, clones a tiny upstream repo into the codeherd layout, and creates a real `feat` worktree via `EnsureWorkspace` (default add → `git worktree add -b feat <path> main`). Profile mode is chosen by the `registry` argument: `nil` = off (what `config.Load` returns in the common case); `&config.ProfileRegistry{Active: "work"}` = on, so `h.Ref(...)` stamps `Profile: "work"` and every session name is prefixed (`work-myapp-feat`). The agent command is `sleep 300` so the tmux session stays alive for the assertions. `useIsolatedTmux` is an inline copy of the `cmd_test` helper (CLAUDE.md sanctions inlining per package; the name is free in `package herd`).

- [ ] **Step 1: Write the failing test (compile-RED — helpers not yet defined)**

Create `internal/herd/matrix_integration_test.go` with the build tag, imports, and **only** the `TestMatrix_LaunchAndResolve` function (which references the not-yet-written harness):

```go
//go:build integration

package herd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/tmux"
)

// TestMatrix_LaunchAndResolve fills the Launch and Resolve rows of the §10
// matrix against real tmux: a launched agent session must exist on the server
// under its (possibly profile-prefixed) canonical name, and Resolve must find
// it by the same identity Ref that created it.
func TestMatrix_LaunchAndResolve(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, _ := setupMatrixHerd(t, col.registry)

			launched, err := h.Launch(ref, LaunchOpts{})
			if err != nil {
				t.Fatalf("Launch: %v", err)
			}

			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Fatalf("tmux server has no session %q after Launch", ref.CanonicalName())
			}

			got, err := h.Resolve(ref, SessionTypeAgent)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.ID != launched.ID {
				t.Errorf("Resolve ID = %q, want %q", got.ID, launched.ID)
			}
			if got.Canonical != ref.CanonicalName() {
				t.Errorf("Resolve Canonical = %q, want %q", got.Canonical, ref.CanonicalName())
			}
		})
	}
}
```

Note: `errors` is imported now because Task 3 (same file) uses it; Go tolerates it only once other code references it. To keep Step 1 compiling *up to the point of the missing helpers*, this import is exercised by Task 3's code added later — if `go vet`/compile complains about an unused `errors` import at Step 1, that is subsumed by the undefined-helper failure you are expecting in Step 2. (The import is left in place from the start so the file's import block is stable across tasks.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/herd/ -run TestMatrix_LaunchAndResolve`
Expected: FAIL — compile errors `undefined: matrixProfiles`, `undefined: useIsolatedTmux`, `undefined: setupMatrixHerd`, `undefined: tmuxHasSession` (the harness does not exist yet).

- [ ] **Step 3: Add the harness (table + three helpers)**

Append to `internal/herd/matrix_integration_test.go` (after the imports, before or after the test — Go order-independent):

```go
// matrixProfiles is the two columns of the §10 coverage matrix: every
// operation is exercised with profiles off and on. "off" passes a nil
// registry (profile mode disabled — what config.Load returns in the common
// case); "on" passes a registry with an active profile, so h.Ref() stamps the
// profile and every session name is prefixed (e.g. work-myapp-feat).
var matrixProfiles = []struct {
	name     string
	registry *config.ProfileRegistry
}{
	{"profile off", nil},
	{"profile on", &config.ProfileRegistry{Active: "work"}},
}

// useIsolatedTmux gives the calling test a private tmux server reached via a
// socket under t.TempDir(). It sets CODEHERD_TMUX_SOCKET so the Herd's real
// tmux runner targets the same server, clears $TMUX so new-session does not
// think it is nested, probes once, and t.Skips when tmux cannot daemonize
// (missing binary or sandboxed CI). The server is killed on cleanup so the
// socket — and any sleep processes the sessions started — disappear with the
// TempDir. Returns the socket path for direct tmux assertions.
func useIsolatedTmux(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	t.Setenv("TMUX", "")
	probe := exec.Command("tmux", "-S", socket, "new-session", "-d", "-s", "__probe__", "sleep", "30")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux daemonize unavailable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
	})
	return socket
}

// tmuxHasSession reports whether the isolated server has an exactly-named
// session. The "=" target prefix forces an exact match so an agent session
// (work-myapp-feat) is never confused with its shell (work-myapp-feat~sh).
func tmuxHasSession(t *testing.T, socket, name string) bool {
	t.Helper()
	return exec.Command("tmux", "-S", socket, "has-session", "-t", "="+name).Run() == nil
}

// setupMatrixHerd builds a Herd wired to REAL tmux and REAL git for the given
// profile column, with the myapp project cloned and a "feat" worktree created
// on disk. It returns the Herd, the identity Ref (carrying the profile when
// the registry is non-nil), and the worktree path.
func setupMatrixHerd(t *testing.T, registry *config.ProfileRegistry) (*Herd, Ref, string) {
	t.Helper()
	root := t.TempDir()

	// A tiny upstream repo with a single commit on main.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "init")

	// Clone into the codeherd layout: <projectsDir>/github.com/user/myapp.
	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "clone", remote, cloneDir)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir, Agent: "agent"},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
		Agents: map[string]config.AgentConfig{
			// A long sleep keeps the tmux session alive for the assertions.
			"agent": {Cmd: "sleep", Args: []string{"300"}},
		},
	}
	h := New(cfg, registry, Deps{Tmux: tmux.NewRealRunner(), Git: git.NewRealRunner()})

	ref := h.Ref("myapp", "feat")
	ws, err := h.EnsureWorkspace(ref, EnsureOpts{})
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	return h, ref, ws.Path
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w internal/herd/matrix_integration_test.go && go test -tags integration ./internal/herd/ -run TestMatrix_LaunchAndResolve -v`
Expected: PASS for both subtests (`profile off`, `profile on`) — or the whole test SKIPs if tmux cannot daemonize in this environment. A genuine assertion FAILURE means real tmux disagrees with the domain (e.g. Resolve cannot find a profile-prefixed session) — that is a real defect; stop and report it under §14.3, do not "fix" the test.

- [ ] **Step 5: Run the full gate**

Run: `make check`
Expected: green — coverage ≥80% (unchanged; the coverage phase omits `-tags integration` so this file is invisible to it), integration tests pass (or skip), lint clean, build OK.

- [ ] **Step 6: Commit**

```bash
git add internal/herd/matrix_integration_test.go
git commit -m "test(herd): real-tmux matrix — Launch/Resolve, profile off+on

First cell of the §10 coverage contract. Adds an isolated-real-tmux harness
(useIsolatedTmux, tmuxHasSession, setupMatrixHerd) and a two-column table
(profile off / profile on) exercising Launch and Resolve against a real tmux
server. Resolve under a profile is one of the three §10 gap cells; this is its
integration coverage.

Refs spec §10, §3.3.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: The `StopSessions` matrix row

**Files:**
- Modify: `internal/herd/matrix_integration_test.go` (append one test)

**Interfaces:**
- Consumes: the Task 1 harness (`matrixProfiles`, `useIsolatedTmux`, `setupMatrixHerd`, `tmuxHasSession`); `(*Herd).Launch`, `(*Herd).StopSessions`; `LaunchOpts`, `StopOpts`, `SessionTypeAgent`, `SessionTypeShell`, `Ref.CanonicalName()`.
- Produces: `func TestMatrix_StopSessions(t *testing.T)`.

**Context — why this shape:**
`StopSessions(ref, StopOpts{All: true})` must stop *both* the agent and the shell session and return their handles. The shell's tmux name is `Ref.CanonicalName() + "~sh"` (that is exactly what `semconv.ShellSessionName` returns). Under a profile this is the cell that was a gap: the pre-refactor code rebuilt a profile-blind name (`myapp-feat`) and missed the real `work-myapp-feat`. This is a characterization test on existing code — expect PASS; a FAIL is a real defect.

- [ ] **Step 1: Write the test**

Append to `internal/herd/matrix_integration_test.go`:

```go
// TestMatrix_StopSessions fills the StopSessions row: after launching both an
// agent and a shell session, StopSessions(All) must stop both, return two
// handles, and leave neither on the real tmux server. Under a profile this is
// the cell that was a gap — the pre-refactor code rebuilt a profile-blind name
// and missed the profile-prefixed session.
func TestMatrix_StopSessions(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, _ := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{Type: SessionTypeAgent}); err != nil {
				t.Fatalf("Launch agent: %v", err)
			}
			if _, err := h.Launch(ref, LaunchOpts{Type: SessionTypeShell}); err != nil {
				t.Fatalf("Launch shell: %v", err)
			}

			agentName := ref.CanonicalName()
			shellName := ref.CanonicalName() + "~sh" // == semconv.ShellSessionName(...)
			if !tmuxHasSession(t, socket, agentName) || !tmuxHasSession(t, socket, shellName) {
				t.Fatalf("precondition: expected both sessions running (agent=%v shell=%v)",
					tmuxHasSession(t, socket, agentName), tmuxHasSession(t, socket, shellName))
			}

			stopped, err := h.StopSessions(ref, StopOpts{All: true})
			if err != nil {
				t.Fatalf("StopSessions: %v", err)
			}
			if len(stopped) != 2 {
				t.Errorf("StopSessions stopped %d sessions, want 2", len(stopped))
			}
			if tmuxHasSession(t, socket, agentName) {
				t.Errorf("agent session %q survived StopSessions", agentName)
			}
			if tmuxHasSession(t, socket, shellName) {
				t.Errorf("shell session %q survived StopSessions", shellName)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `gofmt -w internal/herd/matrix_integration_test.go && go test -tags integration ./internal/herd/ -run TestMatrix_StopSessions -v`
Expected: PASS for both subtests (or SKIP if tmux unavailable). A FAIL under `profile on` is the real gap the matrix exists to catch — report it under §14.3, do not alter the test to pass.

- [ ] **Step 3: Run the full gate**

Run: `make check`
Expected: green.

- [ ] **Step 4: Commit**

```bash
git add internal/herd/matrix_integration_test.go
git commit -m "test(herd): real-tmux matrix — StopSessions, profile off+on

Second §10 gap cell. Launches an agent and a shell session, then asserts
StopSessions(All) stops both by ID and neither survives on the real tmux
server — under a profile, the exact case the pre-refactor profile-blind name
rebuild missed.

Refs spec §10, §3.3.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: The `Teardown` matrix row (force + non-force refuse) — the shipped defect

**Files:**
- Modify: `internal/herd/matrix_integration_test.go` (append two tests)

**Interfaces:**
- Consumes: the Task 1 harness (`matrixProfiles`, `useIsolatedTmux`, `setupMatrixHerd`, `tmuxHasSession`); `(*Herd).Launch`, `(*Herd).Teardown`; `LaunchOpts`, `TeardownOpts`, `ErrSessionRunning`, `Ref.CanonicalName()`; the `errors` and `os` imports already in the file.
- Produces: `func TestMatrix_Teardown(t *testing.T)`, `func TestMatrix_TeardownRefusesRunning(t *testing.T)`.

**Context — why this shape:**
`Teardown` is the row the shipped defect lived in (§2, §8.3): the TUI killed by ID then ran a second profile-blind kill loop that missed, and force-deleted the worktree anyway — orphaning the agent against a gone directory. With `Force: true`, `Teardown` must kill the (profile-prefixed) session **and** remove the worktree from disk. Without `Force`, it must refuse with `ErrSessionRunning` while a session is live and leave both the session and worktree intact (`workspace.go:334-345` returns `ErrSessionRunning` before stopping anything). These are characterization tests on existing code — expect PASS; a surviving session under `profile on` in the force case is precisely the orphaned-agent bug.

- [ ] **Step 1: Write both tests**

Append to `internal/herd/matrix_integration_test.go`:

```go
// TestMatrix_Teardown fills the Teardown row — the row the shipped defect
// lived in. With Force, Teardown must kill the (profile-prefixed) session AND
// remove the worktree from disk. A surviving session under "profile on" is
// exactly the orphaned-agent bug the matrix exists to catch.
func TestMatrix_Teardown(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, wtPath := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{}); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Fatalf("precondition: session %q not running", ref.CanonicalName())
			}

			if err := h.Teardown(ref, TeardownOpts{Force: true}); err != nil {
				t.Fatalf("Teardown: %v", err)
			}

			if tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Errorf("session %q survived Teardown (orphaned agent)", ref.CanonicalName())
			}
			if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
				t.Errorf("worktree %q still on disk after Teardown (stat err=%v)", wtPath, err)
			}
		})
	}
}

// TestMatrix_TeardownRefusesRunning is the non-force half: Teardown without
// Force must refuse with ErrSessionRunning while a session is live, and must
// leave both the session and the worktree intact.
func TestMatrix_TeardownRefusesRunning(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, wtPath := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{}); err != nil {
				t.Fatalf("Launch: %v", err)
			}

			err := h.Teardown(ref, TeardownOpts{Force: false})
			if !errors.Is(err, ErrSessionRunning) {
				t.Fatalf("Teardown(Force:false) err = %v, want ErrSessionRunning", err)
			}
			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Errorf("session %q was killed despite refusal", ref.CanonicalName())
			}
			if _, err := os.Stat(wtPath); err != nil {
				t.Errorf("worktree %q removed despite refusal: %v", wtPath, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `gofmt -w internal/herd/matrix_integration_test.go && go test -tags integration ./internal/herd/ -run 'TestMatrix_Teardown' -v`
Expected: PASS for all four subtests (both tests × both columns), or SKIP if tmux unavailable. The `-run 'TestMatrix_Teardown'` pattern matches both `TestMatrix_Teardown` and `TestMatrix_TeardownRefusesRunning`. A surviving session under `profile on` in the force case is the shipped defect — report it under §14.3.

- [ ] **Step 3: Run the whole matrix once, then the full gate**

Run: `go test -tags integration ./internal/herd/ -run TestMatrix -v && make check`
Expected: all `TestMatrix_*` subtests PASS (or SKIP together); `make check` green. Running the whole `TestMatrix` family confirms the harness serves every operation and there is no cross-test tmux leakage (each subtest gets its own isolated socket + `kill-server` cleanup).

- [ ] **Step 4: Commit**

```bash
git add internal/herd/matrix_integration_test.go
git commit -m "test(herd): real-tmux matrix — Teardown force + refuse, profile off+on

Third §10 gap cell, the row the shipped defect lived in. Force teardown must
kill the profile-prefixed session AND remove the worktree from disk; non-force
must refuse with ErrSessionRunning and touch nothing. Both run profile off and
on against real tmux. Completes the §10 coverage contract.

Refs spec §10, §2, §8.3.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage (§10 — the section Plan 3 owns):**
- "Integration — the coverage contract. Every operation runs with profiles on *and* off." — every `TestMatrix_*` iterates `matrixProfiles` = {off, on}. ✅
- Gap cell `Resolve` under profile — Task 1 `TestMatrix_LaunchAndResolve` (`profile on` subtest). ✅
- Gap cell `StopSessions` under profile — Task 2 `TestMatrix_StopSessions` (`profile on`). ✅
- Gap cell `Teardown` under profile — Task 3 `TestMatrix_Teardown` + `TestMatrix_TeardownRefusesRunning` (`profile on`). ✅
- "This matrix is the gate that would have caught the original defect" — Task 3 asserts the profile-prefixed session is gone AND the worktree removed after force teardown (the exact orphan condition of §2/§8.3). ✅
- §3.3 "the quadrant nobody wrote" (profile × {stop, delete, show}) — stop=`StopSessions` (Task 2), delete=`Teardown` (Task 3), show=`Resolve` (Task 1), all under a profile. ✅
- "`herd` tests fake `tmux.Runner`" (unit, §10) vs integration — this plan is the integration layer; unit fakes already exist and are untouched. ✅

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Every code step shows complete code. The one prose note (Step 1 `errors` import) is a real compile-order caveat, not a placeholder. ✅

**3. Type/name consistency:**
- Harness signatures identical across the interface blocks, Step 3 implementation, and every call site: `useIsolatedTmux(t) string`, `tmuxHasSession(t, socket, name string) bool`, `setupMatrixHerd(t, registry) (*Herd, Ref, string)`, `matrixProfiles` with fields `name`/`registry`. ✅
- Reuses `runGit` from `integration_test.go` — not redeclared. Checked against the full `package herd` test-identifier list; `useIsolatedTmux`, `tmuxHasSession`, `setupMatrixHerd`, `matrixProfiles`, and all `TestMatrix_*` names are free. ✅
- API calls match as-built signatures: `New(cfg, registry, Deps{Tmux, Git})`, `h.Ref(project, branch)`, `EnsureWorkspace(ref, EnsureOpts{})`, `Launch(ref, LaunchOpts{Type})`, `Resolve(ref, SessionTypeAgent)`, `StopSessions(ref, StopOpts{All:true}) ([]Handle, error)`, `Teardown(ref, TeardownOpts{Force}) error`, `ErrSessionRunning`, `Ref.CanonicalName()`. Verified against `internal/herd/{herd,session,workspace,errors}.go`. ✅
- Shell tmux name `ref.CanonicalName() + "~sh"` matches `semconv.ShellSessionName`. Agent config `{Cmd:"sleep", Args:["300"]}` → `Command()` = `"sleep 300"`. ✅

**Behaviour / process notes for the §14.3 handoff:**
1. This plan changes **no production code** — it is pure integration coverage. If any `TestMatrix_*` fails on first green-run, that is a real defect (record it), not a plan error.
2. The new tests run only in `make check`'s `test-integration` phase; they do not affect the coverage percentage (coverage runs without the tag). `make lint`/`make build` also omit the tag, so the file is exercised only under `-tags integration`.
3. §14.3 prompts to answer after execution: did the matrix find real bugs or confirm the design? is it cheap/fast enough to keep green (each subtest starts and kills a real tmux server + `sleep` processes)? should it become a `CLAUDE.md` standing rule?

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-16-coverage-contract.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute the three tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**

After execution, record findings in the spec's §14.3 ("After Plan 3 — the coverage contract"), then offer `superpowers:finishing-a-development-branch` — this is the final plan of the herd-domain refactor.
