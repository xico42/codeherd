# Plan 1 — the collapse

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md`. Read §2, §6, §12.1, and §14 before starting. This plan is Plan 1 of three; Plans 2 (front-end thinning) and 3 (coverage contract) are written later, in their own sessions, informed by what this one records in §14.1 of the spec.

**Goal:** Collapse `internal/session`, `internal/worktree`, and `internal/project` into a single `internal/herd` domain package that owns identity (the active profile) and therefore cannot address a session it did not create.

**Architecture:** One `Herd` value holds `cfg` + the active profile + the exec-boundary runners, and hands out `Ref` values that always carry the profile. Every session lookup is keyed on a `Ref`, so the profile-blind `semconv.SessionName("", …)` literal — the root cause of the shipped defect and three of its siblings (spec §3.1) — has nowhere left to live. Git exec moves to a new `internal/git` mechanism package. Build order is **project → session → worktree** (spec §12.1); each stage moves one domain's logic *and* migrates its callers, so `cmd/` and `internal/tui/` are touched once per domain rather than twice.

**Tech Stack:** Go (module `github.com/xico42/codeherd`), Cobra, Bubble Tea v2, tmux, git. Tests are stdlib `testing` with hand-written fakes at the `Runner` seam.

## Global Constraints

- **`make check` must pass before every commit.** It runs coverage (80% floor), integration tests, lint, and build. A task is not done until it is green.
- **Coverage floor: 80% aggregate.** Deleting three packages that sit at 91% / 89.3% / high coverage while adding an uncovered `herd` will sink the total. Tests move *with* the code in the same commit, never in a follow-up.
- **`herd` must never import `cmd` or `internal/tui`.** Keeps a future promotion out of `internal/` a rename (spec §11).
- **Typed enums:** every closed-set string gets a defined Go type with named constants (`SessionType`, `Status`). No bare string parameters for these.
- **`wrapcheck` is enabled.** Every error crossing a package boundary must be wrapped with `%w` and a context prefix. `_test.go` files are exempt (`.golangci.yml`).
- **`goimports` with `local-prefixes: github.com/xico42/codeherd`.** Import blocks are stdlib / third-party / codeherd.
- **Test repos must pin the default branch:** `git init -b main <dir>`, never bare `git init` (CLAUDE.md).
- **Tests touching real tmux must isolate it:** set `CODEHERD_TMUX_SOCKET` under `t.TempDir()`, clear `$TMUX`, probe-and-skip, `kill-server` on cleanup. Never `exec.Command("tmux", …)` directly from a test.
- **A stage that rewrites tests instead of moving them is a signal the stage is doing too much** (spec §12.1).
- **Record the handoff as you go, not at the end.** Every task's commit step appends what it learned to spec §14.1, in that same commit. See below.

## Recording the handoff (read this before Task 1)

Spec §14 is the handoff between sessions, and it is load-bearing: Plans 2 and 3 are written in fresh sessions whose only context is the spec. If this plan is executed subagent-per-task, **the agent running Task 6 never saw Tasks 1–5** — it cannot reconstruct what surprised you, what cost an hour, or which assumption turned out wrong. Nobody can write that down after the fact.

So each task writes its own notes, in its own commit:

- Every task's commit step ends with an append to spec §14.1 and `git add`s the spec alongside the code.
- **Append; do not rewrite.** §14 says so explicitly. Later tasks add below earlier ones.
- Write it under a `**Task N — <name>**` sub-heading so Task 6 can curate it into prose.
- **If a task learned nothing worth recording, write nothing.** An empty §14.1 after Task 1 is a fine outcome; padding it with "went as planned" is worse than silence, because it dilutes the signal Plan 2 is reading for.

What is worth recording (spec §14's own list):

| | |
|---|---|
| **Assumptions this document got wrong** | Name the section, so the next session distrusts the right paragraph |
| **API changes** | The real signature, if it moved off this plan's Deviations table |
| **Decisions reversed** | Which §11 row, and what forced it |
| **Traps** | Anything costing more than ~30 minutes — especially cross-layer surprises: tmux behaviour, git worktree edge cases, Cobra lifecycle, test isolation |
| **Deferred work** | What you skipped, and which plan should pick it up |
| **Behaviour changes** | Anything a user could notice. This refactor ships four that are known going in; there may be more |

## Deviations from the spec's §6 sketch

The spec says the §6 surface "is a sketch, not a contract" and asks for the real one to be recorded. These are decided here, up front, and must be copied into spec §14.1 by Task 6:

| Spec §6 says | This plan does | Why |
|---|---|---|
| `New(cfg *config.Config, profile string, deps Deps)` | `New(cfg *config.Config, registry *config.ProfileRegistry, deps Deps)` | `WithProfile(name)` needs `ProfilesDir` to call `config.LoadProfile`, and only the registry has it. The spec's own §8.1 sample (`herd.New(cfg, registry.Active, …)`) nil-panics when profiles are off — `config.Load` returns a nil registry, which is exactly why `cmd/services.go:37` guards it. Passing the registry makes `New` total and deletes `activeProfile()`. |
| `hooks` does not appear anywhere | Unexported field `newHook func(config.HooksConfig) hooks.Hook`, defaulted in `New` | Spec §3.2's constraint is that hooks must not be **bound at construction** — that is what created the dead `Model.sesSvc` fields. A defaulted, test-overridable field satisfies that and keeps the 8 existing hook tests moving intact (spec §10) instead of being rewritten against real shell commands. It stays out of the exported API. |
| `Handle` has a `Ref` | Same, plus `@codeherd_project` is stamped as a new tmux option | Without it, `Ref.Project` cannot be recovered from a tmux record (the canonical name is ambiguous — spec §11), so `Sessions()` would return `Handle`s with a half-populated `Ref`. A `Ref` missing only `Project` is a footgun that compiles into `Teardown`. One extra `SetOption` + one format field removes it. |
| — | `Project(name string) (Project, error)` added | `ch show project <name>` needs one project with `Cloned` status. §6 listed only `Projects()`. |
| — | `CloneAll` dropped, not moved | `project.Service.CloneAll` has **zero non-test callers** — `cmd/project.go:99-128` runs its own loop. YAGNI. |
| `RemoteBranch` declared in `herd` | Declared in `internal/git`, re-exported from `herd` as a type alias | `git.Runner.ListRemoteBranches` returns it, so declaring it in `herd` would force a conversion loop at the exec boundary. `type RemoteBranch = git.RemoteBranch` gives §6's surface for free. |
| Files: `session.go worktree.go project.go launch.go teardown.go list.go` | `herd.go errors.go paths.go project.go session.go workspace.go` | Launch/Teardown/List are each ~40 lines and belong beside the domain they operate on. Six files either way. |

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/git/git.go` | `WorktreeRunner` + `CloneRunner` + `Runner` union; `RealRunner`; `WorktreeInfo` / `RemoteBranch`; porcelain parsers. Mechanism — no `cfg`, no profile. |
| `internal/git/git_test.go` | Parser unit tests (moved from `worktree_test.go`). |
| `internal/git/realrunner_test.go` | Real-git integration-ish tests (moved from `internal/worktree/realrunner_test.go`). |
| `internal/herd/herd.go` | `Herd`, `Ref`, `Deps`, `New`, `WithProfile`, `SessionType`, `Status`, `hookFor`. |
| `internal/herd/errors.go` | Every sentinel + `AlreadyClonedError` + `SessionExistsError`. One vocabulary (spec §9). |
| `internal/herd/paths.go` | `cloneDir` / `worktreesRoot` / `worktreePath` / `projectNames`. Pure identity → config derivation. |
| `internal/herd/project.go` | `Project`, `Projects`, `Project(name)`, `Clone`. |
| `internal/herd/session.go` | `Handle`, `LaunchOpts`, `StopOpts`, `Launch`, `Resolve`, `Sessions`, `StopSessions`, `SetStatus`. |
| `internal/herd/workspace.go` | `Workspace`, `EnsureOpts`, `TeardownOpts`, `EnsureWorkspace`, `Provision`, `List`, `Teardown`, `RemoteBranches`. |
| `internal/herd/fakes_test.go` | The **one** shared fake set: `fakeGit` (satisfies the 14-method union), `fakeTmux`, `mockHook`. Per-test overrides via func fields. |

**Deleted (by the end of Task 5):** `internal/project/`, `internal/session/`, `internal/worktree/` — all files.

**Modified:** `cmd/root.go` (composition root), `cmd/services.go`, `cmd/project.go`, `cmd/session.go`, `cmd/worktree.go`, `cmd/template.go`, `cmd/completion.go`, `cmd/plugin.go`, `cmd/errors.go`, `cmd/tui.go`, `internal/tmux/client.go` (one new option), `internal/tui/model.go`, `actions.go`, `form.go`, `agent_picker.go`, `remote_picker.go`.

**Test packages:** all `herd` tests live in `package herd` (internal), not `package herd_test`. Reason: `worktree_test.go` already tests unexported helpers (`parseRef`, `freshenStartPoint`, `resolvePaths`), and `project_test.go` + `session_test.go` **both** declare `mockHook` / `hookCall` — merging them into one external test package collides. One internal test package + one shared `fakes_test.go` resolves both.

---

### Task 1: `internal/git` — rehouse the exec boundary

Pure move. `internal/worktree` and `internal/project` keep compiling via type aliases, which Tasks 3 and 5 delete.

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/git_test.go`
- Create: `internal/git/realrunner_test.go` (moved from `internal/worktree/realrunner_test.go`)
- Modify: `internal/worktree/worktree.go:19-289` (delete the runner + parsers, add aliases)
- Modify: `internal/project/project.go:25-49` (delete the runner, add aliases)
- Delete: `internal/worktree/realrunner_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  package git

  type WorktreeInfo struct{ Path, Branch string; Detached bool }
  type RemoteBranch struct{ Remote, Branch, Ref string }

  type WorktreeRunner interface {
      Add(cloneDir, worktreePath, branch string) error
      AddNewBranch(cloneDir, worktreePath, branch string) error
      AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
      Remove(cloneDir, worktreePath string) error
      List(cloneDir string) ([]WorktreeInfo, error)
      Fetch(cloneDir, remote, branch string) error
      FetchAll(cloneDir string) error
      FastForward(cloneDir, remote, branch string) error
      Remotes(cloneDir string) ([]string, error)
      ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)
      AddTracking(cloneDir, worktreePath, branch, remoteRef string) error
      HasLocalBranch(cloneDir, branch string) (bool, error)
  }
  type CloneRunner interface{ Clone(repo, path, branch string) error }
  type Runner interface{ WorktreeRunner; CloneRunner }

  type RealRunner struct{}
  func NewRealRunner() *RealRunner   // implements Runner
  ```
  Unexported, used by Task 5: `parseRef(remotes []string, ref string) (remote, branch string, explicit bool)` — **exported here as `ParseRef`**, because Task 5's `freshenStartPoint` lives in `herd` and needs it.

- [ ] **Step 1: Create `internal/git/git.go`**

Move verbatim from `internal/worktree/worktree.go`, renaming the receiver type only:
- `WorktreeInfo` (lines 29-33), `RemoteBranch` (51-55)
- `WorktreeRunner` interface (58-72) — note the interface has **12** methods; the spec's "13 methods" in §5 miscounted. `Runner` is therefore a 13-method union, not 14.
- `RealWorktreeRunner` methods (80-221) → methods on `RealRunner`
- `parseWorktreePorcelain` (225-250), `parseRemoteBranches` (255-273)
- `parseRef` (279-289) → exported as `ParseRef`, same body, same doc comment

Move verbatim from `internal/project/project.go`:
- `RealGitRunner.Clone` (37-49) → `func (r *RealRunner) Clone(repo, path, branch string) error`, same body

Add at the top:
```go
// Package git wraps git command execution. It is a mechanism package: it
// never sees the config, the active profile, or a Ref — it only takes paths
// and refs it is handed. Exactly one real implementation exists; the
// interfaces exist so internal/herd can fake the exec boundary in tests.
package git

// Runner is the union both herd and its tests depend on. Splitting it
// further is out of scope: it sits at the exec boundary where one real
// implementation exists.
type Runner interface {
	WorktreeRunner
	CloneRunner
}

// NewRealRunner returns a Runner backed by the system git binary.
func NewRealRunner() *RealRunner { return &RealRunner{} }
```

- [ ] **Step 2: Move the parser tests**

`git grep -n 'parseWorktreePorcelain\|parseRemoteBranches\|parseRef' internal/worktree/worktree_test.go` to find them. Move each matching `func Test…` into `internal/git/git_test.go` (`package git`), renaming `parseRef` → `ParseRef` at call sites. Delete them from `worktree_test.go`.

Move `internal/worktree/realrunner_test.go` → `internal/git/realrunner_test.go`: change line 1 to `package git`, rename `NewRealWorktreeRunner()` → `NewRealRunner()` and `RealWorktreeRunner` → `RealRunner` throughout. Its `runGit` / `realRunnerRepos` helpers come along unchanged.

Add one test for the newly-unioned `Clone` (there is no runner-level clone test today — `project_test.go` fakes it):

```go
func TestRealRunner_Clone(t *testing.T) {
	src := t.TempDir()
	runGit(t, src, "init", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "init")

	dst := filepath.Join(t.TempDir(), "clone")
	if err := NewRealRunner().Clone(src, dst, "main"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Errorf("cloned tree missing f.txt: %v", err)
	}
}
```

- [ ] **Step 3: Alias the old packages so the tree stays green**

In `internal/worktree/worktree.go`, delete lines 28-33 (`WorktreeInfo`), 51-55 (`RemoteBranch`), 57-221 (`WorktreeRunner` + `RealWorktreeRunner`), 223-289 (parsers), and the now-unused `bufio` / `os/exec` imports. Add:

```go
// Deprecated: these aliases keep this package compiling while its logic
// moves to internal/herd. Deleted in the worktree stage of the collapse.
type WorktreeInfo = git.WorktreeInfo
type RemoteBranch = git.RemoteBranch
type WorktreeRunner = git.WorktreeRunner

func NewRealWorktreeRunner() *git.RealRunner { return git.NewRealRunner() }
```

Every internal use of `parseRef` in `worktree.go` (`freshenStartPoint:327`, `NewTracking:478`) becomes `git.ParseRef`.

In `internal/project/project.go`, delete lines 25-49 (`GitRunner`, `RealGitRunner`, `NewRealGitRunner`, `Clone`) and the `os/exec` import. Add:

```go
// Deprecated: alias kept while this package's logic moves to internal/herd.
type GitRunner = git.CloneRunner

func NewRealGitRunner() *git.RealRunner { return git.NewRealRunner() }
```

`worktree_test.go`'s `mockGit` and `project_test.go`'s `mockGitRunner` still satisfy the aliased interfaces unchanged — do not touch them.

- [ ] **Step 4: Verify**

Run: `make check`
Expected: `OK: <n>% >= 80%`, integration green, lint clean, build clean. Coverage should be roughly flat — nothing was deleted, only relocated.

If `wrapcheck` fires on the moved `RealRunner` methods, it is a false positive from the package move; the bodies already wrap with `%w`. Do not add nolint — re-check the import block ordering first (`goimports` with the local prefix).

- [ ] **Step 5: Record and commit**

Append to spec §14.1 under a `**Task 1 — internal/git**` heading. Known going in — record it even if nothing else surprised you:

> `WorktreeRunner` has **12** methods, not the 13 §5 claims, so `git.Runner` is a 13-method union rather than 14. §14.1's prompt "did `git.Runner` as a 14-method union cause pain in test fakes?" is asking about the wrong number.

Add anything else the move surfaced — `wrapcheck` or `goimports` fighting the new package boundary, parser tests that did not survive the split.

```bash
git add internal/git internal/worktree internal/project docs/superpowers/specs
git commit -m "refactor: rehouse git exec into internal/git

WorktreeRunner and GitRunner lived in two domain packages that both
shelled out to git. Move both to internal/git behind a Runner union;
the old packages keep aliases until their logic follows."
```

---

### Task 2: `internal/herd` skeleton — identity, config, paths

No behaviour moves yet. This task builds the thing that makes the defect impossible: a `Ref` you cannot obtain without a profile.

**Files:**
- Create: `internal/herd/herd.go`
- Create: `internal/herd/errors.go`
- Create: `internal/herd/paths.go`
- Create: `internal/herd/herd_test.go`
- Create: `internal/herd/paths_test.go`
- Create: `internal/herd/fakes_test.go`

**Interfaces:**
- Consumes: `git.Runner`, `git.RealRunner` (Task 1); `tmux.Runner`, `tmux.NewClient`; `config.Config`, `config.ProfileRegistry`, `config.LoadProfile`, `config.RepoPath`, `config.HooksConfig`; `hooks.Hook`, `hooks.New`; `semconv.*`.
- Produces: everything in the code blocks below. Tasks 3-5 hang methods off `*Herd` and use `h.cfg`, `h.git`, `h.tmux`, `h.profile`, `h.hookFor`, `h.cloneDir`, `h.worktreePath`, `h.projectNames`.

- [ ] **Step 1: Write the failing tests**

`internal/herd/herd_test.go`:

```go
package herd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

// h.Ref takes no profile argument, so the shortest path is the correct one.
// This is the whole point of the collapse: the profile-blind Ref cannot be
// spelled without visibly hand-building the struct.
func TestRef_carriesActiveProfile(t *testing.T) {
	h := New(&config.Config{}, &config.ProfileRegistry{Active: "work"}, Deps{})
	ref := h.Ref("myapp", "feat")

	if ref.Profile != "work" {
		t.Errorf("Profile = %q, want %q", ref.Profile, "work")
	}
	if got := ref.CanonicalName(); got != "work-myapp-feat" {
		t.Errorf("CanonicalName() = %q, want %q", got, "work-myapp-feat")
	}
}

// A nil registry is what config.Load returns when profiles are off. New must
// not panic on it — the spec's own §8.1 sample did.
func TestNew_nilRegistryMeansNoProfile(t *testing.T) {
	h := New(&config.Config{}, nil, Deps{})
	ref := h.Ref("myapp", "feat")

	if ref.Profile != "" {
		t.Errorf("Profile = %q, want empty", ref.Profile)
	}
	if got := ref.CanonicalName(); got != "myapp-feat" {
		t.Errorf("CanonicalName() = %q, want %q", got, "myapp-feat")
	}
}

func TestRef_tmuxNameDiffersByType(t *testing.T) {
	h := New(&config.Config{}, &config.ProfileRegistry{Active: "work"}, Deps{})
	ref := h.Ref("myapp", "feat/login")

	if got := ref.tmuxName(SessionTypeAgent); got != "work-myapp-feat-login" {
		t.Errorf("agent tmuxName = %q", got)
	}
	if got := ref.tmuxName(SessionTypeShell); got != "work-myapp-feat-login~sh" {
		t.Errorf("shell tmuxName = %q", got)
	}
}

func TestWithProfile_swapsConfigAndProfile(t *testing.T) {
	dir := t.TempDir()
	toml := "[projects.myapp]\nrepo = \"git@github.com:user/other.git\"\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &config.ProfileRegistry{Active: "work", Names: []string{"work", "home"}, ProfilesDir: dir}
	h := New(&config.Config{}, reg, Deps{})

	next, err := h.WithProfile("home")
	if err != nil {
		t.Fatalf("WithProfile: %v", err)
	}
	if next.Ref("myapp", "feat").Profile != "home" {
		t.Error("new Herd did not adopt the home profile")
	}
	if next.Config().Projects["myapp"].Repo != "git@github.com:user/other.git" {
		t.Error("new Herd did not adopt the home config")
	}
	if h.Ref("myapp", "feat").Profile != "work" {
		t.Error("WithProfile mutated the receiver; it must return a new Herd")
	}
}

func TestWithProfile_errorsWhenProfilesDisabled(t *testing.T) {
	h := New(&config.Config{}, nil, Deps{})
	if _, err := h.WithProfile("work"); err == nil {
		t.Fatal("want error when profiles are disabled, got nil")
	}
}

func TestHookFor_defaultsToConfiguredHooks(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	h := New(cfg, nil, Deps{})
	if h.hookFor("myapp") == nil {
		t.Error("hookFor returned nil for a configured project")
	}
	if h.hookFor("nonexistent") == nil {
		t.Error("hookFor returned nil for an unconfigured project; it must be total")
	}
}
```

`internal/herd/paths_test.go`:

```go
package herd

import (
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func pathsHerd(t *testing.T) (*Herd, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	return New(cfg, nil, Deps{}), dir
}

func TestCloneDir_derivedFromRepoURL(t *testing.T) {
	h, dir := pathsHerd(t)
	got, err := h.cloneDir("myapp")
	if err != nil {
		t.Fatalf("cloneDir: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp")
	if got != want {
		t.Errorf("cloneDir = %q, want %q", got, want)
	}
}

func TestWorktreePath_flattensBranch(t *testing.T) {
	h, dir := pathsHerd(t)
	got, err := h.worktreePath(h.Ref("myapp", "feat/login"))
	if err != nil {
		t.Fatalf("worktreePath: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat-login")
	if got != want {
		t.Errorf("worktreePath = %q, want %q", got, want)
	}
}

func TestPaths_unconfiguredProject(t *testing.T) {
	h, _ := pathsHerd(t)
	if _, err := h.cloneDir("nope"); err == nil {
		t.Error("want error for unconfigured project, got nil")
	}
}

func TestProjectNames_sortedOrAll(t *testing.T) {
	h, _ := pathsHerd(t)
	h.cfg.Projects["alpha"] = config.ProjectConfig{Repo: "git@github.com:user/alpha.git"}

	all, err := h.projectNames("")
	if err != nil {
		t.Fatalf("projectNames(\"\"): %v", err)
	}
	if len(all) != 2 || all[0] != "alpha" || all[1] != "myapp" {
		t.Errorf("projectNames(\"\") = %v, want [alpha myapp]", all)
	}

	one, err := h.projectNames("myapp")
	if err != nil {
		t.Fatalf("projectNames(\"myapp\"): %v", err)
	}
	if len(one) != 1 || one[0] != "myapp" {
		t.Errorf("projectNames(\"myapp\") = %v", one)
	}
	if _, err := h.projectNames("nope"); err == nil {
		t.Error("want error for unconfigured project, got nil")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/herd/...`
Expected: FAIL — `no Go files in .../internal/herd` (the package does not exist yet).

- [ ] **Step 3: Write `internal/herd/herd.go`**

```go
// Package herd is codeherd's domain. It owns projects, worktrees, and the
// tmux sessions running in them — three things that used to be three
// packages that could not see each other.
//
// The split cost us a class of defects. internal/session had no config, so
// it could not know the active profile, so every profile decision moved up
// into its callers, and one of them rebuilt a session name without the
// profile and killed nothing. Here, identity lives in one place: a Ref
// obtained from Herd.Ref always carries the profile, and every session
// lookup is keyed on a Ref.
package herd

import (
	"fmt"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// SessionType distinguishes the two kinds of session codeherd runs. Both are
// first-class: they coexist for the same Ref and are addressed the same way.
type SessionType string

const (
	SessionTypeAgent SessionType = semconv.SessionTypeAgent
	SessionTypeShell SessionType = semconv.SessionTypeShell
)

// Status is an agent session's lifecycle state, stored on the tmux session.
type Status string

const (
	StatusRunning Status = semconv.StatusRunning
	StatusWaiting Status = semconv.StatusWaiting
)

// RemoteBranch is one remote-tracking branch. Aliased rather than redeclared:
// git.Runner returns these, and a conversion loop at the exec boundary would
// buy nothing.
type RemoteBranch = git.RemoteBranch

// Ref identifies a workspace — a project and branch, scoped to a profile.
//
// Branch is ALWAYS the identity branch: the branch the worktree was created
// for, which is what its sessions were named after. It is never the branch
// HEAD currently points at. Use Workspace.DisplayBranch for rendering.
//
// Obtain a Ref from Herd.Ref or from Workspace.Ref. Never build one by hand.
// Herd.Ref takes no profile argument, so the shortest path is the correct
// one; a hand-built herd.Ref{Project: p, Branch: b} is visibly missing a
// field under review, and that missing field is the bug this package exists
// to prevent.
type Ref struct {
	Profile string
	Project string
	Branch  string
}

// CanonicalName is the session name frozen at creation: the identity both
// session types share, and the key every tmux lookup matches on.
func (r Ref) CanonicalName() string {
	return semconv.SessionName(r.Profile, r.Project, r.Branch)
}

// tmuxName is the actual tmux session name for a type. It differs from
// CanonicalName only for shell sessions, which carry a ~sh suffix so the two
// types can coexist.
func (r Ref) tmuxName(t SessionType) string {
	if t == SessionTypeShell {
		return semconv.ShellSessionName(r.Profile, r.Project, r.Branch)
	}
	return semconv.SessionName(r.Profile, r.Project, r.Branch)
}

// Deps holds the exec-boundary runners. Two fields; revisit options at three.
type Deps struct {
	Tmux tmux.Runner
	Git  git.Runner
}

// Herd is the domain: config, the active profile, and the runners.
type Herd struct {
	cfg         *config.Config
	profile     string
	profilesDir string
	profiles    []string
	tmux        *tmux.Client
	git         git.Runner

	// newHook builds the hook dispatcher for one project's hook config.
	//
	// It is a defaulted field, not a constructor parameter, and that is
	// deliberate. Binding hooks at construction is what killed dependency
	// injection in the TUI: the actions needed a project-bound hook, so
	// every one of them rebuilt its own service and Model.sesSvc became a
	// field that was assigned and never read. Herd holds cfg, so it can
	// resolve hooks per operation instead. Tests override this field.
	newHook func(config.HooksConfig) hooks.Hook
}

// New builds a Herd for the given config and profile registry. A nil
// registry means profile mode is off — that is what config.Load returns in
// the common case, so New must accept it.
func New(cfg *config.Config, registry *config.ProfileRegistry, deps Deps) *Herd {
	h := &Herd{
		cfg:     cfg,
		tmux:    tmux.NewClient(deps.Tmux),
		git:     deps.Git,
		newHook: func(hc config.HooksConfig) hooks.Hook { return hooks.New(hc) },
	}
	if registry != nil {
		h.profile = registry.Active
		h.profilesDir = registry.ProfilesDir
		h.profiles = registry.Names
	}
	return h
}

// Ref supplies the active profile. This is the only sanctioned way to mint a
// Ref from a (project, branch) pair.
func (h *Herd) Ref(project, branch string) Ref {
	return Ref{Profile: h.profile, Project: project, Branch: branch}
}

// Config exposes the config this Herd was built for. Front ends need it for
// agent lookup and project enumeration.
func (h *Herd) Config() *config.Config { return h.cfg }

// Profile returns the active profile name, or "" when profile mode is off.
func (h *Herd) Profile() string { return h.profile }

// Profiles returns every discovered profile name, nil when profile mode is off.
func (h *Herd) Profiles() []string { return h.profiles }

// WithProfile returns a new Herd scoped to a different profile, sharing this
// one's runners. The receiver is unchanged.
func (h *Herd) WithProfile(name string) (*Herd, error) {
	if h.profilesDir == "" {
		return nil, fmt.Errorf("cannot switch to profile %q: profiles are not enabled", name)
	}
	cfg, err := config.LoadProfile(h.profilesDir, name)
	if err != nil {
		return nil, fmt.Errorf("loading profile %s: %w", name, err)
	}
	next := *h
	next.cfg = cfg
	next.profile = name
	return &next, nil
}

// hookFor returns the hook dispatcher for a project. It is total: an
// unconfigured project yields a dispatcher with no hooks, which fires nothing.
func (h *Herd) hookFor(project string) hooks.Hook {
	return h.newHook(h.cfg.Projects[project].Hooks)
}
```

- [ ] **Step 4: Write `internal/herd/errors.go`**

One vocabulary. Every sentinel that lived in `session`, `worktree`, or `project` lands here.

```go
package herd

import (
	"errors"
	"fmt"
)

// Sentinels. These are the whole error vocabulary of the domain; front ends
// match these and nothing else. They span three packages today, which is why
// cmd/errors.go grew two translators that both handle ErrNotCloned and print
// different text for it.
var (
	ErrNotCloned         = errors.New("project not cloned")
	ErrAlreadyCloned     = errors.New("already cloned")
	ErrWorktreeExists    = errors.New("worktree already exists")
	ErrWorktreeNotFound  = errors.New("worktree not found")
	ErrLocalBranchExists = errors.New("local branch already exists")
	ErrSessionExists     = errors.New("session already exists")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionRunning    = errors.New("session is running")
	ErrPathNotFound      = errors.New("worktree path not found")
)

// AlreadyClonedError carries the path that already exists.
type AlreadyClonedError struct{ Path string }

func (e *AlreadyClonedError) Error() string { return e.Path + " already exists, skipping" }
func (e *AlreadyClonedError) Unwrap() error { return ErrAlreadyCloned }

// SessionExistsError is returned by Launch when a session for the same Ref
// and type is already running. It carries the Ref so a front end can print an
// attach hint without re-deriving identity.
type SessionExistsError struct {
	Ref  Ref
	Type SessionType
}

func (e *SessionExistsError) Error() string {
	return fmt.Sprintf("%s: %s/%s (%s)", ErrSessionExists.Error(), e.Ref.Project, e.Ref.Branch, e.Type)
}

func (e *SessionExistsError) Unwrap() error { return ErrSessionExists }
```

- [ ] **Step 5: Write `internal/herd/paths.go`**

Bodies move from `worktree.go:305-318` (`resolvePaths`), `447-457` (`cloneDirFor`), and `674-687` (`projectNames`), split into single-purpose helpers. `internal/worktree` keeps its copies until Task 5 deletes the package — one task of duplication, deliberately.

```go
package herd

import (
	"fmt"
	"sort"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// repoPath returns the filesystem-relative path derived from a project's repo
// URL, e.g. github.com/user/myapp.
func (h *Herd) repoPath(project string) (string, error) {
	p, ok := h.cfg.Projects[project]
	if !ok {
		return "", fmt.Errorf("project %q is not configured", project)
	}
	rp, err := config.RepoPath(p.Repo)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL %q: %w", p.Repo, err)
	}
	return rp, nil
}

// cloneDir returns the main git clone directory for a project.
func (h *Herd) cloneDir(project string) (string, error) {
	rp, err := h.repoPath(project)
	if err != nil {
		return "", err
	}
	return semconv.CloneDir(h.cfg.Defaults.ProjectsDir, rp), nil
}

// worktreesRoot returns the directory holding a project's worktrees.
func (h *Herd) worktreesRoot(project string) (string, error) {
	rp, err := h.repoPath(project)
	if err != nil {
		return "", err
	}
	return semconv.WorktreesRoot(h.cfg.Defaults.ProjectsDir, rp), nil
}

// worktreePath returns the filesystem path for a ref's worktree. It derives
// from Ref.Branch — the identity branch — so it agrees with the session name
// by construction.
func (h *Herd) worktreePath(ref Ref) (string, error) {
	rp, err := h.repoPath(ref.Project)
	if err != nil {
		return "", err
	}
	return semconv.WorktreePath(h.cfg.Defaults.ProjectsDir, rp, ref.Branch), nil
}

// projectNames returns sorted project names, or just the named one after
// validating it exists.
func (h *Herd) projectNames(project string) ([]string, error) {
	if project != "" {
		if _, ok := h.cfg.Projects[project]; !ok {
			return nil, fmt.Errorf("project %q is not configured", project)
		}
		return []string{project}, nil
	}
	names := make([]string, 0, len(h.cfg.Projects))
	for name := range h.cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
```

- [ ] **Step 6: Write `internal/herd/fakes_test.go`**

The one shared fake set. Later tasks add per-test overrides by assigning func fields; they do not hand-roll new runners.

```go
package herd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/hooks"
)

// fakeGit satisfies the whole git.Runner union. Every method is a func field
// defaulting to success, so a test overrides only what it cares about:
//
//	g := &fakeGit{}
//	g.AddFn = func(_, _, _ string) error { return errors.New("boom") }
type fakeGit struct {
	mu    sync.Mutex
	Calls []string // "<method> <arg> <arg>…", in order

	AddFn                func(cloneDir, worktreePath, branch string) error
	AddNewBranchFn       func(cloneDir, worktreePath, branch string) error
	AddNewBranchFromFn   func(cloneDir, worktreePath, branch, startPoint string) error
	RemoveFn             func(cloneDir, worktreePath string) error
	ListFn               func(cloneDir string) ([]git.WorktreeInfo, error)
	FetchFn              func(cloneDir, remote, branch string) error
	FetchAllFn           func(cloneDir string) error
	FastForwardFn        func(cloneDir, remote, branch string) error
	RemotesFn            func(cloneDir string) ([]string, error)
	ListRemoteBranchesFn func(cloneDir string) ([]git.RemoteBranch, error)
	AddTrackingFn        func(cloneDir, worktreePath, branch, remoteRef string) error
	HasLocalBranchFn     func(cloneDir, branch string) (bool, error)
	CloneFn              func(repo, path, branch string) error
}

func (g *fakeGit) record(parts ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Calls = append(g.Calls, strings.Join(parts, " "))
}

// called reports whether any recorded call contains all the given substrings.
func (g *fakeGit) called(want ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.Calls {
		ok := true
		for _, w := range want {
			if !strings.Contains(c, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (g *fakeGit) Add(cloneDir, worktreePath, branch string) error {
	g.record("Add", cloneDir, worktreePath, branch)
	if g.AddFn != nil {
		return g.AddFn(cloneDir, worktreePath, branch)
	}
	return nil
}

func (g *fakeGit) AddNewBranch(cloneDir, worktreePath, branch string) error {
	g.record("AddNewBranch", cloneDir, worktreePath, branch)
	if g.AddNewBranchFn != nil {
		return g.AddNewBranchFn(cloneDir, worktreePath, branch)
	}
	return nil
}

func (g *fakeGit) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	g.record("AddNewBranchFrom", cloneDir, worktreePath, branch, startPoint)
	if g.AddNewBranchFromFn != nil {
		return g.AddNewBranchFromFn(cloneDir, worktreePath, branch, startPoint)
	}
	return nil
}

func (g *fakeGit) Remove(cloneDir, worktreePath string) error {
	g.record("Remove", cloneDir, worktreePath)
	if g.RemoveFn != nil {
		return g.RemoveFn(cloneDir, worktreePath)
	}
	return nil
}

func (g *fakeGit) List(cloneDir string) ([]git.WorktreeInfo, error) {
	g.record("List", cloneDir)
	if g.ListFn != nil {
		return g.ListFn(cloneDir)
	}
	return nil, nil
}

func (g *fakeGit) Fetch(cloneDir, remote, branch string) error {
	g.record("Fetch", cloneDir, remote, branch)
	if g.FetchFn != nil {
		return g.FetchFn(cloneDir, remote, branch)
	}
	return nil
}

func (g *fakeGit) FetchAll(cloneDir string) error {
	g.record("FetchAll", cloneDir)
	if g.FetchAllFn != nil {
		return g.FetchAllFn(cloneDir)
	}
	return nil
}

func (g *fakeGit) FastForward(cloneDir, remote, branch string) error {
	g.record("FastForward", cloneDir, remote, branch)
	if g.FastForwardFn != nil {
		return g.FastForwardFn(cloneDir, remote, branch)
	}
	return nil
}

func (g *fakeGit) Remotes(cloneDir string) ([]string, error) {
	g.record("Remotes", cloneDir)
	if g.RemotesFn != nil {
		return g.RemotesFn(cloneDir)
	}
	return []string{"origin"}, nil
}

func (g *fakeGit) ListRemoteBranches(cloneDir string) ([]git.RemoteBranch, error) {
	g.record("ListRemoteBranches", cloneDir)
	if g.ListRemoteBranchesFn != nil {
		return g.ListRemoteBranchesFn(cloneDir)
	}
	return nil, nil
}

func (g *fakeGit) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	g.record("AddTracking", cloneDir, worktreePath, branch, remoteRef)
	if g.AddTrackingFn != nil {
		return g.AddTrackingFn(cloneDir, worktreePath, branch, remoteRef)
	}
	return nil
}

func (g *fakeGit) HasLocalBranch(cloneDir, branch string) (bool, error) {
	g.record("HasLocalBranch", cloneDir, branch)
	if g.HasLocalBranchFn != nil {
		return g.HasLocalBranchFn(cloneDir, branch)
	}
	return false, nil
}

func (g *fakeGit) Clone(repo, path, branch string) error {
	g.record("Clone", repo, path, branch)
	if g.CloneFn != nil {
		return g.CloneFn(repo, path, branch)
	}
	return nil
}

// fakeTmux satisfies tmux.Runner. Sessions is the raw list-sessions table it
// serves; Calls records every invocation.
type fakeTmux struct {
	mu       sync.Mutex
	Sessions []sessionRow
	Calls    [][]string
	RunFn    func(args ...string) (string, string, int, error) // overrides everything
}

// sessionRow is one record in the fake's list-sessions table, in the field
// order tmux.Client.ListSessions parses.
type sessionRow struct {
	ID, Name, Canonical, Type, Status, Annotation, StartedAt, Profile, Branch, Project string
}

func (r sessionRow) format() string {
	return strings.Join([]string{
		r.ID, r.Name, r.Canonical, r.Type, r.Status,
		r.Annotation, r.StartedAt, r.Profile, r.Branch, r.Project,
	}, "\t")
}

func (f *fakeTmux) Run(args ...string) (string, string, int, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, args)
	f.mu.Unlock()

	if f.RunFn != nil {
		return f.RunFn(args...)
	}
	switch args[0] {
	case "list-sessions":
		if len(f.Sessions) == 0 {
			return "", "", 1, nil // tmux exits 1 when there are no sessions
		}
		rows := make([]string, len(f.Sessions))
		for i, s := range f.Sessions {
			rows[i] = s.format()
		}
		return strings.Join(rows, "\n"), "", 0, nil
	case "new-session":
		return "$1", "", 0, nil
	case "has-session":
		return "", "", 1, nil
	}
	return "", "", 0, nil
}

// called reports whether any recorded tmux invocation contains all the given
// substrings, in any position.
func (f *fakeTmux) called(want ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		joined := strings.Join(c, " ")
		ok := true
		for _, w := range want {
			if !strings.Contains(joined, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// killed returns every kill-session target, in order.
func (f *fakeTmux) killed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		if len(c) >= 3 && c[0] == "kill-session" {
			out = append(out, c[2])
		}
	}
	return out
}

// mockHook records hook triggers and can fail a named one.
type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

// withHook forces every operation on h to use the given hook, bypassing
// config lookup. This is the seam that keeps the hook tests intact.
func withHook(h *Herd, m hooks.Hook) *Herd {
	h.newHook = func(config.HooksConfig) hooks.Hook { return m }
	return h
}
```

**About `sessionRow.Project`:** `tmux.SessionRecord.Project` does not exist yet — Task 4 adds it, along with widening `ListSessions`'s `SplitN(line, "\t", 9)` to 10. Until then, `SplitN` caps at 9 substrings, so a row with a non-empty `Project` would leave `Branch` holding `"feat\tmyapp"`. Declaring the field now keeps `fakes_test.go` stable across Task 4, but **no test may set `Project` (or use `Sessions` at all) until Task 4 Step 1 is done** — Tasks 2 and 3 do not, so this is safe. Task 4's `TestSessions_rebuildsCompleteRef` is what proves the widening landed.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/herd/... -v`
Expected: PASS for all of `TestRef_carriesActiveProfile`, `TestNew_nilRegistryMeansNoProfile`, `TestRef_tmuxNameDiffersByType`, `TestWithProfile_swapsConfigAndProfile`, `TestWithProfile_errorsWhenProfilesDisabled`, `TestHookFor_defaultsToConfiguredHooks`, and the five path tests.

`fakeGit`, `fakeTmux`, and `mockHook` are unused this task. Go does not complain about unused types, but if `golangci-lint` flags `withHook` as unused, leave it — Task 3 uses it. If the linter blocks the commit, note it and move `fakes_test.go` into Task 3 instead of adding a nolint.

- [ ] **Step 8: Verify, record, and commit**

Run: `make check`
Expected: green. Coverage rises slightly — `herd` lands well covered.

Append to spec §14.1 under `**Task 2 — herd skeleton**`. Record the two API decisions this task locks in, because every later plan codes against them:

> `New` takes `(cfg, registry, deps)`, not `(cfg, profile, deps)` — `WithProfile` needs `ProfilesDir`, and §8.1's `herd.New(cfg, registry.Active, …)` sample nil-panics when profiles are off. `cmd.activeProfile()` is gone as a result.
>
> Hooks are resolved per operation via an unexported `newHook func(config.HooksConfig) hooks.Hook` field on `Herd`, defaulted in `New` and overridden by tests. §6 said hooks would not appear at all; they do not appear in the *exported* API, which is what §3.2's constraint actually requires.

Also record whether the `withHook` / `fakeGit` / `fakeTmux` set in `fakes_test.go` tripped the `unused` linter before Task 3 consumed it (Step 7 flags this as a possibility).

```bash
git add internal/herd docs/superpowers/specs
git commit -m "feat: add internal/herd skeleton — identity, config, paths

Herd holds cfg plus the active profile, so Ref always carries the
profile and h.Ref takes no profile argument. That is the whole
mechanism: the shortest path to a Ref is the correct one, and the
profile-blind SessionName(\"\", …) literal has nowhere to live.

No behaviour moves yet."
```

---

### Task 3: project domain → `herd`

First domain in. `project` has no dependencies — it never imports `tmux` — so it moves cleanest (spec §12.1). Its callers migrate with it, and `internal/project` is deleted in the same commit.

**Files:**
- Create: `internal/herd/project.go`
- Create: `internal/herd/project_test.go` (moved from `internal/project/project_test.go`)
- Modify: `cmd/root.go:14-44` (composition root)
- Modify: `cmd/services.go:28-42` (delete `newProjectService`, `activeProfile`)
- Modify: `cmd/project.go` (all three commands)
- Modify: `cmd/tui.go:119-125`
- Modify: `internal/tui/model.go` (`Model.projSvc` → `Model.herd`; `profileCache`)
- Modify: `internal/tui/actions.go:108-109,202-203,264-265`
- Modify: `internal/tui/form.go:137-138`
- Modify: `internal/tui/agent_picker.go:98-99`
- Delete: `internal/project/` (both files)

**Interfaces:**
- Consumes: `Herd`, `Ref`, `hookFor`, `cloneDir`, `projectNames`, `ErrAlreadyCloned`, `AlreadyClonedError` (Task 2).
- Produces:
  ```go
  type Project struct {
      Name   string
      Config config.ProjectConfig
      Path   string // absolute, derived from repo URL + projects_dir
      Cloned bool
  }

  func (h *Herd) Projects() []Project              // all, sorted, no filesystem access
  func (h *Herd) Project(name string) (Project, error) // one, with Cloned status
  func (h *Herd) Clone(project string) error       // *AlreadyClonedError if the path exists
  ```
  Plus, in `cmd`: the package-level `var h *herd.Herd`, which every later task's `cmd` code uses.

- [ ] **Step 1: Move the tests**

Move `internal/project/project_test.go` → `internal/herd/project_test.go`. Transform:
- line 1: `package project_test` → `package herd`
- Delete its `mockGitRunner` (15-20), `cloneCall` (20), `mockHook` (30), `hookCall` (35) — `fakes_test.go` supplies all four. `mockGitRunner` becomes `fakeGit` with `CloneFn`; assert with `g.called("Clone", repo, path)` instead of inspecting `cloneCall` slices.
- `makeConfig` (49) stays, but returns a `*Herd`: rename to `projectHerd(t, projectsDir, projects) (*Herd, *fakeGit)` and have it call `New(cfg, nil, Deps{Git: g})`.
- `project.NewService(cfg, git, hook)` → `withHook(projectHerd(…))`
- `svc.List()` → `h.Projects()`, `svc.Show(n)` → `h.Project(n)`, `svc.Clone(n)` → `h.Clone(n)`
- `project.ErrAlreadyCloned` → `ErrAlreadyCloned`, `*project.AlreadyClonedError` → `*AlreadyClonedError`
- **Delete `TestCloneAll_MixedResults` (246) and `TestCloneAll_Empty` (282).** `CloneAll` has no non-test callers and does not move.

Add one test the old package could not have had — that `Clone` is reachable through the same `Herd` the rest of the domain uses:

```go
func TestClone_underProfile_usesProfileConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "trunk"},
		},
	}
	g := &fakeGit{}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Git: g})

	if err := h.Clone("myapp"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp")
	if !g.called("Clone", "git@github.com:user/myapp.git", want, "trunk") {
		t.Errorf("clone did not target %s; calls=%v", want, g.Calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/herd/...`
Expected: FAIL — `undefined: Project`, `h.Projects undefined`, `h.Clone undefined`.

- [ ] **Step 3: Write `internal/herd/project.go`**

Bodies move from `internal/project/project.go`: `List` (78-95), `Show` (98-115), `Clone` (119-155). The only real change is that path derivation goes through `h.cloneDir` instead of being inlined three times.

```go
package herd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// Project is a configured project with its derived clone path.
type Project struct {
	Name   string
	Config config.ProjectConfig
	Path   string // absolute path derived from repo URL + projects_dir
	Cloned bool   // true if Path exists on the filesystem
}

// Projects returns every configured project sorted by name. It does not touch
// the filesystem, so Cloned is always false — use Project for that.
func (h *Herd) Projects() []Project {
	names, _ := h.projectNames("") // "" cannot error
	entries := make([]Project, 0, len(names))
	for _, name := range names {
		path, _ := h.cloneDir(name) // unparseable repo URL yields an empty path
		entries = append(entries, Project{
			Name:   name,
			Config: h.cfg.Projects[name],
			Path:   path,
		})
	}
	return entries
}

// Project returns one project including its Cloned status.
func (h *Herd) Project(name string) (Project, error) {
	path, err := h.cloneDir(name)
	if err != nil {
		return Project{}, err
	}
	_, statErr := os.Stat(path)
	return Project{
		Name:   name,
		Config: h.cfg.Projects[name],
		Path:   path,
		Cloned: statErr == nil,
	}, nil
}

// Clone clones a project's repo into its derived path under projects_dir.
// Returns *AlreadyClonedError (wrapping ErrAlreadyCloned) if the path exists.
func (h *Herd) Clone(project string) error {
	path, err := h.cloneDir(project)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return &AlreadyClonedError{Path: path}
	}

	p := h.cfg.Projects[project]
	hook := h.hookFor(project)
	attrs := map[string]string{
		semconv.HookAttrProject:  project,
		semconv.HookAttrRepo:     p.Repo,
		semconv.HookAttrCloneDir: path,
	}

	if err := hook.Trigger(semconv.HookPreClone, attrs, h.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("pre-clone hook: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	if err := h.git.Clone(p.Repo, path, p.DefaultBranch); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}
	if err := hook.Trigger(semconv.HookPostClone, attrs, h.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("post-clone hook: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/herd/... -v`
Expected: PASS, including the moved `TestClone_TriggersHooks` and `TestClone_PreHookFailure_StopsClone`.

- [ ] **Step 5: Build the composition root**

`cmd/root.go` — replace the `cfg` + `registry` globals with a single `h`. Keep `cfg` for now: Tasks 4 and 5 still read it, and Task 6 removes what is left.

```go
var (
	cfgFile     string
	noTmux      bool
	profileFlag string
	cfg         *config.Config
	registry    *config.ProfileRegistry
	// h is the domain. It is the only service any command constructs, and
	// it is constructed exactly once, here.
	h *herd.Herd
)
```

In `PersistentPreRunE` (line 36), after the existing `config.Load`:

```go
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, registry, err = config.Load(cfgFile, resolveProfileArg(profileFlag))
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		h = herd.New(cfg, registry, herd.Deps{
			Tmux: tmux.NewRealRunner(),
			Git:  git.NewRealRunner(),
		})
		return nil
	},
```

- [ ] **Step 6: Migrate the project callers**

`cmd/services.go`: delete `newProjectService` (31-33). Delete `activeProfile` (37-42) **and** its uses — there are three (`session.go:233`, `session.go:242`, and the two shims at 47/69/89 which stay until Task 4). Replace `activeProfile()` in `cmd/session.go:233` and `:242` with `h.Profile()`. Leave `showSessionForProfile` / `stopSessionForProfile` / `listSessionsForProfile` alone; they die in Task 4. They call `activeProfile()`, so keep a local `prof := h.Profile()` in each rather than reviving the helper.

`cmd/project.go`:
- `ListProjectCmd.Run`: `svc := newProjectService(); entries := svc.List()` → `entries := h.Projects()`
- `ShowProjectCmd.Run`: `svc.Show(args[0])` → `h.Project(args[0])`
- `CloneProjectCmd.Run`: delete both `hooks.New` + `project.NewService` blocks (107-109, 134-136). The `--all` loop calls `h.Clone(name)`; the single path calls `h.Clone(name)` then `h.Project(name)` for the path line. `*project.AlreadyClonedError` → `*herd.AlreadyClonedError`. Drop the `hooks` and `project` imports; keep `sort` (the `--all` loop still sorts) — or better, replace the loop's manual name-gathering (100-104) with `for _, p := range h.Projects()`, which is already sorted, and drop `sort` too.

`cmd/tui.go:119-125`:
```go
func runTUIDirect(tmuxClient *tmux.Client) error {
	wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, &hooks.NoOp{})
	sesSvc := newSessionService()

	insideTmux := os.Getenv("TMUX") != ""
	m := tui.NewModel(cfg, wtSvc, sesSvc, h, tmuxClient, insideTmux, registry)
	…
}
```
`projSvc` is gone; `h` takes its slot. Drop the `project` import.

`internal/tui/model.go`:
- `NewModel`: replace the `projSvc *project.Service` parameter with `herd *herd.Herd`; drop the `project` import.
- `Model`: replace `projSvc *project.Service` with `herd *herd.Herd`.
- `profileBundle` (111-115): drop the `projSvc` field; the struct keeps `cfg` and `wtSvc` until Task 5.
- `switchProfile` (594-628): delete the `project.NewService` line (613). Add `herd` to the bundle, built with `m.herd.WithProfile(next)`:
  ```go
  	bundle, ok := m.profileCache[next]
  	if !ok {
  		nextHerd, err := m.herd.WithProfile(next)
  		if err != nil {
  			m.statusMsg = fmt.Sprintf("profile switch failed: %v", err)
  			return m, nil
  		}
  		cfg := nextHerd.Config()
  		wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), m.tmuxClient, &hooks.NoOp{})
  		bundle = profileBundle{cfg: cfg, wtSvc: wtSvc, herd: nextHerd}
  		m.profileCache[next] = bundle
  	}
  	m.cfg = bundle.cfg
  	m.wtSvc = bundle.wtSvc
  	m.herd = bundle.herd
  ```
  `config.LoadProfile` is now called inside `WithProfile`, so drop the direct `config.LoadProfile` call and, if nothing else in the file uses it, the `config` import stays (it is used for `*config.Config` fields).
- `NewModel`'s seed (147-149): `m.profileCache[registry.Active] = profileBundle{cfg: cfg, wtSvc: wtSvc, herd: herdArg}`.

`internal/tui/actions.go`, `form.go`, `agent_picker.go` — three identical shapes. Each has:
```go
	projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
	_ = projSvc.Clone(project)
```
Replace each with `_ = hrd.Clone(project)`, where `hrd` is the `*herd.Herd` captured alongside `cfg` in the enclosing closure. Sites: `actions.go:108-109` and `202-203` and `264-265` (this last one is `cloneAction`, which checks the error — keep `if err := hrd.Clone(project); err != nil { return errMsg{err: err} }`), `form.go:137-138`, `agent_picker.go:98-99`.

`form.go` and `agent_picker.go` capture services through struct fields (`formModel.cfg` / `agentPickerPending.cfg`). Add a `herd *herd.Herd` field beside each `cfg` field and populate it at construction — `newFormModel(ctx, m.cfg, m.tmuxClient)` becomes `newFormModel(ctx, m.cfg, m.herd, m.tmuxClient)`, and `agentPickerPending` gains `herd: m.herd`. Update `showForm`, `showTrackForm` (`model.go:692`), and both `newAgentPicker` call sites (`actions.go:84`, `:139`) accordingly.

Drop the `projectpkg` import from all three files.

- [ ] **Step 7: Delete `internal/project`**

```bash
git rm -r internal/project
```

Run: `go build ./... && go vet ./...`
Expected: clean. Any remaining reference to `internal/project` is a caller Step 6 missed — `git grep -n 'internal/project'` must return nothing.

- [ ] **Step 8: Verify**

Run: `make check`
Expected: green, ≥80%.

If coverage dropped: the moved project tests did not come with the code. `go test -coverprofile=/tmp/c.out ./internal/herd/... && go tool cover -func=/tmp/c.out | grep project.go` — every function in `project.go` should show non-zero.

- [ ] **Step 9: Record and commit**

Append to spec §14.1 under `**Task 3 — project domain**`. Known going in:

> `CloneAll` was dropped rather than moved — zero non-test callers; `cmd/project.go` ran its own loop. Its two tests were deleted.
>
> `Project(name) (Project, error)` was added for `ch show project`, which needs one project with `Cloned` status. §6 listed only `Projects()`.

This is the first stage to migrate front-end callers, so it is the first real evidence for §13's "project folding is the weakest link" worry. Record your read: did folding `project` in earn its place, or does it still look like the first cut to reverse?

```bash
git add -A
git commit -m "refactor: fold project into internal/herd

First domain in. project never imported tmux, so it moves cleanest and
proves the shape: Clone resolves its own hook from cfg instead of taking
one at construction, which is what let cmd and tui stop building
services.

cmd/root.go now constructs the one Herd every command uses. CloneAll is
dropped — it had no non-test callers."
```

---

### Task 4: session domain → `herd`

The heart of it. `session.Service` is the package with no config (spec §2.1) — the asymmetry that made a session addressable only if you got lucky. After this task there is exactly one lookup path, and it is keyed on a `Ref` that carries the profile.

**Files:**
- Create: `internal/herd/session.go`
- Create: `internal/herd/session_test.go` (moved from `internal/session/session_test.go`)
- Modify: `internal/tmux/client.go:10-21,228-262` (add `@codeherd_project`)
- Modify: `internal/tmux/client_test.go` (the list-sessions format assertions)
- Modify: `cmd/services.go` (delete the three `*ForProfile` shims and `newSessionService`)
- Modify: `cmd/session.go` (all four commands)
- Modify: `cmd/worktree.go:182-198` (the `--attach` session start)
- Modify: `cmd/plugin.go:47-63`
- Modify: `cmd/errors.go:31-53`
- Modify: `cmd/tui.go:119-125`
- Modify: `internal/tui/model.go` (drop `Model.sesSvc` — it is assigned and never read)
- Modify: `internal/tui/actions.go` (four `session.NewService` + `Start` sites)
- Modify: `internal/tui/agent_picker.go:113-127`
- Modify: `internal/tui/delete_teardown_test.go`
- Modify: `cmd/services_test.go`, `cmd/session_internal_test.go`
- Delete: `internal/session/` (both files)

**Interfaces:**
- Consumes: everything from Tasks 2 and 3, plus `h.worktreePath`, `h.cloneDir`.
- Produces:
  ```go
  type Handle struct {
      ID         string // tmux session_id ("$1") — stable across renames
      Ref        Ref
      Type       SessionType
      TmuxName   string // current tmux name; may carry the ⚡ status prefix
      Status     Status
      Annotation string
      StartedAt  time.Time
  }

  type LaunchOpts struct {
      Type   SessionType // zero value SessionTypeAgent
      Agent  string      // agent name; "" means defaults.agent. Ignored for shell.
      Attach bool
  }

  type StopOpts struct {
      Type SessionType // ignored when All is true
      All  bool        // stop every type for this Ref
  }

  func (h *Herd) Launch(ref Ref, opts LaunchOpts) (Handle, error)
  func (h *Herd) Resolve(ref Ref, t SessionType) (Handle, error)
  func (h *Herd) Sessions() ([]Handle, error)             // profile-filtered
  func (h *Herd) StopSessions(ref Ref, opts StopOpts) ([]Handle, error) // stopped handles
  func (h *Herd) SetStatus(canonicalName string, status Status, annotation string) error
  ```
  Task 5 calls `h.StopSessions` from `Teardown`.

- [ ] **Step 1: Stamp the project on the session**

`Ref.Project` cannot be recovered from a tmux record today: `work-myapp-feat` could be profile `work` + project `myapp`, or a project literally named `work-myapp` (spec §11). Without it, `Sessions()` returns `Handle`s whose `Ref` is missing exactly one field — and a `Ref` missing only `Project` compiles into `Teardown`. Stamp it.

`internal/tmux/client.go`, `SessionRecord` (10-21) — add after `Branch`:
```go
	Project       string // @codeherd_project — the project the session belongs to, "" when unset
```

`ListSessions` (228-262): append `\t#{@codeherd_project}` to the format string, change both `9`s to `10` (`SplitN(line, "\t", 10)` and the pad loop), and add `Project: fields[9]` to the record literal.

`internal/semconv/semconv.go` — add beside the other option names (line 17):
```go
	TmuxOptionProject       = "@codeherd_project"
```

Sessions started before this change have no `@codeherd_project`, so their `Handle.Ref.Project` is `""`. That fails loudly and safely: `Teardown` on such a Ref returns `project "" is not configured` rather than deleting the wrong thing. Say so in a comment on the field.

> **Superseded (2026-07-16):** this "fails loudly" guard was never actually reachable — a pre-upgrade session was silently dropped and survived teardown instead. The `session-canonical-compat` change fixed the real cause: matching now keys on the stored `@codeherd_canonical_name`, so such sessions are recognized, killed, and healed (their project recovered and re-stamped). See `docs/superpowers/specs/2026-07-16-session-canonical-compat-design.md`.

Update `internal/tmux/client_test.go`: `git grep -n 'codeherd_branch' internal/tmux/client_test.go` finds the format assertion and the row builders. Add the tenth column to each. Add one record-level test:

```go
func TestListSessions_parsesProject(t *testing.T) {
	r := &mockRunner{stdout: "$1\twork-myapp-feat\twork-myapp-feat\tagent\trunning\t\t\twork\tfeat\tmyapp"}
	got, err := NewClient(r).ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 1 || got[0].Project != "myapp" {
		t.Errorf("Project = %q, want %q", got[0].Project, "myapp")
	}
}
```
(Match `mockRunner`'s actual field names — read the top of `client_test.go` first.)

Run: `go test ./internal/tmux/...` — PASS before continuing.

- [ ] **Step 2: Move the session tests**

Move `internal/session/session_test.go` → `internal/herd/session_test.go`. Transform:
- line 1: `package session_test` → `package herd`
- Delete its `mockRunner`, `mockHook`, `hookCall`, `newService`, `findCall`, `newSessionEnv` helpers — `fakes_test.go` supplies fakes; `findCall` becomes `f.called(…)` and `newSessionEnv` becomes a local helper in `session_test.go` (it parses `new-session` args, which is session-specific).
- `session.Service` → `*Herd`, built by a local helper:
  ```go
  func sessionHerd(t *testing.T, f *fakeTmux) (*Herd, string) {
  	t.Helper()
  	dir := t.TempDir()
  	cfg := &config.Config{
  		Defaults: config.DefaultsConfig{ProjectsDir: dir, Agent: "claude"},
  		Projects: map[string]config.ProjectConfig{
  			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
  		},
  		Agents: map[string]config.AgentConfig{"claude": {Cmd: "claude"}},
  	}
  	return New(cfg, nil, Deps{Tmux: f, Git: &fakeGit{}}), dir
  }
  ```
  (Check `config.AgentConfig`'s real field names in `internal/config/agent.go` before writing this.)
- `svc.Start(session.StartRequest{Project: p, Branch: b, Path: path, Type: …, Cmd: …, Env: …, Profile: prof})` → `h.Launch(h.Ref(p, b), LaunchOpts{Type: …, Agent: …})`. `Path` and `CloneDir` are no longer passed — `Launch` derives them. Tests that fed an arbitrary `Path` must now `os.MkdirAll` the derived worktree path under the herd's `projects_dir`; `sessionHerd` returns `dir` for exactly that.
- `svc.Show(p, b, t)` → `h.Resolve(h.Ref(p, b), t)`; `svc.Stop(p, b, t)` → `h.StopSessions(h.Ref(p, b), StopOpts{Type: t})`.
- **Delete the `ShowByName` / `StopByName` tests.** Those methods were the escape hatch for the missing profile parameter; nothing addresses a session by name any more except `SetStatus`, which keeps its own tests.
- `session.ErrSessionNotFound` → `ErrSessionNotFound`, etc.

Add the test that names the defect. This is the one the old package could not express, because `Stop` had no profile parameter to get wrong:

```go
// Under an active profile, sessions are named <profile>-<project>-<branch>.
// session.Service.Stop hardcoded an empty profile via SessionName("", …), so
// it searched for myapp-feat and missed work-myapp-feat entirely. Here the
// profile rides on the Ref and there is no parameter to omit.
func TestStopSessions_underProfile_matchesProfileScopedSession(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{Type: SessionTypeAgent})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 1 {
		t.Fatalf("stopped %d sessions, want 1", len(stopped))
	}
	if killed := f.killed(); len(killed) != 1 || killed[0] != "$1" {
		t.Errorf("killed = %v, want [$1] — the session was addressed by name, not ID", killed)
	}
}

// StopOpts.All is what Teardown uses: both types die, addressed by ID.
func TestStopSessions_all_stopsBothTypesByID(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "work-myapp-feat~sh", Canonical: "work-myapp-feat",
			Type: "shell", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{All: true})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("stopped %d sessions, want 2", len(stopped))
	}
	killed := f.killed()
	sort.Strings(killed)
	if len(killed) != 2 || killed[0] != "$1" || killed[1] != "$2" {
		t.Errorf("killed = %v, want [$1 $2]", killed)
	}
}

// Stopping a session that isn't running is not an error: Teardown calls this
// unconditionally, and a worktree with no sessions is the common case.
func TestStopSessions_noneRunning_isNotAnError(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{All: true})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want empty", stopped)
	}
}

// Launch stamps the project so Sessions can rebuild a complete Ref.
func TestLaunch_stampsProjectOption(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Launch(h.Ref("myapp", "feat"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !f.called("set-option", semconv.TmuxOptionProject, "myapp") {
		t.Errorf("@codeherd_project was not stamped; calls=%v", f.Calls)
	}
}

// Sessions rebuilds a complete Ref from the tmux options, so a handle from a
// list can be fed straight back into Teardown.
func TestSessions_rebuildsCompleteRef(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d handles, want 1", len(got))
	}
	want := Ref{Profile: "work", Project: "myapp", Branch: "feat"}
	if got[0].Ref != want {
		t.Errorf("Ref = %+v, want %+v", got[0].Ref, want)
	}
}

// Sessions is profile-scoped: another profile's sessions are not ours.
func TestSessions_filtersByActiveProfile(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "home-myapp-feat", Canonical: "home-myapp-feat",
			Type: "agent", Profile: "home", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "$1" {
		t.Errorf("got %+v, want only the work-profile session", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/herd/...`
Expected: FAIL — `undefined: Handle`, `h.Launch undefined`, `h.StopSessions undefined`.

- [ ] **Step 4: Write `internal/herd/session.go`**

`Start` (`session.go:77-158`), `List` (176-197), and `SetStatus` (319-350) move nearly verbatim. `Show`/`ShowByName`/`Stop`/`StopByName` — four methods, one duplicated `ListSessions`-and-match loop each — collapse into `handles` + `Resolve` + `StopSessions`.

```go
package herd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// Handle is a live session.
type Handle struct {
	ID         string // tmux session_id ("$1") — stable across renames
	Ref        Ref
	Type       SessionType
	TmuxName   string // current tmux name; may carry the ⚡ status prefix
	Status     Status
	Annotation string
	StartedAt  time.Time
}

// LaunchOpts configures a session start. The zero value starts the default
// agent, detached.
type LaunchOpts struct {
	Type   SessionType // zero value means SessionTypeAgent
	Agent  string      // agent name; "" means defaults.agent. Ignored for shell.
	Attach bool        // front ends read Handle.ID and attach themselves
}

// StopOpts selects which of a Ref's sessions to stop.
type StopOpts struct {
	Type SessionType // ignored when All is true
	All  bool        // stop every type for this Ref
}

// Launch starts a detached tmux session for ref and returns its handle.
//
// The session command runs with these env vars, which override conflicting
// keys in the agent's configured Env:
//
//   - CODEHERD_SESSION       canonical session name
//   - CODEHERD_PROJECT       project name
//   - CODEHERD_BRANCH        identity branch
//   - CODEHERD_CLONE_DIR     main git clone path
//   - CODEHERD_WORKTREE_PATH worktree root
//   - CODEHERD_PROFILE       profile name (only when a profile is active)
//
// Returns *SessionExistsError if a session for this ref and type is already
// running, and ErrPathNotFound if the worktree does not exist on disk.
func (h *Herd) Launch(ref Ref, opts LaunchOpts) (Handle, error) {
	if opts.Type == "" {
		opts.Type = SessionTypeAgent
	}

	// Scope the existence check to (ref, type) so agent and shell sessions coexist.
	switch _, err := h.Resolve(ref, opts.Type); {
	case err == nil:
		return Handle{}, &SessionExistsError{Ref: ref, Type: opts.Type}
	case !errors.Is(err, ErrSessionNotFound):
		return Handle{}, err
	}

	path, err := h.worktreePath(ref)
	if err != nil {
		return Handle{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Handle{}, fmt.Errorf("%w: %s", ErrPathNotFound, path)
		}
		return Handle{}, fmt.Errorf("checking worktree path: %w", err)
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return Handle{}, err
	}

	cmd, env, err := h.sessionCommand(opts)
	if err != nil {
		return Handle{}, err
	}

	canonical := ref.CanonicalName()
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrWorktreePath: path,
		semconv.HookAttrSessionName:  canonical,
	}
	if err := hook.Trigger(semconv.HookPreSession, attrs, path); err != nil {
		return Handle{}, fmt.Errorf("pre-session hook: %w", err)
	}

	sessionEnv := make(map[string]string, len(env)+6)
	for k, v := range env {
		sessionEnv[k] = v
	}
	// Codeherd-stamped vars win over user-supplied Env.
	sessionEnv[semconv.SessionEnvVar] = canonical
	sessionEnv[semconv.HookAttrProject] = ref.Project
	sessionEnv[semconv.HookAttrBranch] = ref.Branch
	sessionEnv[semconv.HookAttrWorktreePath] = path
	if cloneDir != "" {
		sessionEnv[semconv.HookAttrCloneDir] = cloneDir
	}
	if ref.Profile != "" {
		sessionEnv[semconv.EnvProfile] = ref.Profile
	}

	// Capture the session ID atomically at creation; a separate
	// display-message round-trip would race with short-lived commands.
	tmuxName := ref.tmuxName(opts.Type)
	id, err := h.tmux.NewSessionWithEnv(tmuxName, path, sessionEnv, cmd)
	if err != nil {
		return Handle{}, fmt.Errorf("creating tmux session: %w", err)
	}

	now := time.Now().UTC()
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionStatus, semconv.StatusRunning)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionStartedAt, now.Format(time.RFC3339))
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionCanonicalName, canonical)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionSessionType, string(opts.Type))
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionBranch, ref.Branch)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionProject, ref.Project)
	if ref.Profile != "" {
		_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionProfile, ref.Profile)
	}

	if err := hook.Trigger(semconv.HookPostSession, attrs, path); err != nil {
		return Handle{}, fmt.Errorf("post-session hook: %w", err)
	}

	return Handle{
		ID:        id,
		Ref:       ref,
		Type:      opts.Type,
		TmuxName:  tmuxName,
		Status:    StatusRunning,
		StartedAt: now,
	}, nil
}

// sessionCommand resolves the command and env a session runs with. A shell
// session runs $SHELL; an agent session runs its configured command.
func (h *Herd) sessionCommand(opts LaunchOpts) (cmd string, env map[string]string, err error) {
	if opts.Type == SessionTypeShell {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		return shell, nil, nil
	}
	name := opts.Agent
	if name == "" {
		name = h.cfg.Defaults.Agent
	}
	if name == "" {
		return "", nil, fmt.Errorf("no agent specified; use --agent or set defaults.agent in config")
	}
	agent, err := h.cfg.AgentByName(name)
	if err != nil {
		return "", nil, fmt.Errorf("resolving agent: %w", err)
	}
	return agent.Command(), agent.Env, nil
}

// Resolve returns the live handle for a ref and type.
// Returns ErrSessionNotFound if no such session is running.
func (h *Herd) Resolve(ref Ref, t SessionType) (Handle, error) {
	if t == "" {
		t = SessionTypeAgent
	}
	all, err := h.handles()
	if err != nil {
		return Handle{}, err
	}
	canonical := ref.CanonicalName()
	for _, hd := range all {
		if hd.Ref.CanonicalName() == canonical && hd.Type == t {
			return hd, nil
		}
	}
	return Handle{}, fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, canonical, t)
}

// Sessions returns every live session belonging to the active profile. With
// no active profile, that is every session codeherd started.
func (h *Herd) Sessions() ([]Handle, error) {
	all, err := h.handles()
	if err != nil {
		return nil, err
	}
	var out []Handle
	for _, hd := range all {
		if hd.Ref.Profile == h.profile {
			out = append(out, hd)
		}
	}
	return out, nil
}

// StopSessions kills the sessions matching ref and returns the handles it
// stopped. Sessions are killed by tmux session ID, never by a rebuilt name.
// Stopping nothing is not an error — Teardown calls this unconditionally.
func (h *Herd) StopSessions(ref Ref, opts StopOpts) ([]Handle, error) {
	all, err := h.handles()
	if err != nil {
		return nil, err
	}
	if opts.Type == "" && !opts.All {
		opts.Type = SessionTypeAgent
	}

	canonical := ref.CanonicalName()
	var stopped []Handle
	for _, hd := range all {
		if hd.Ref.CanonicalName() != canonical {
			continue
		}
		if !opts.All && hd.Type != opts.Type {
			continue
		}
		if err := h.tmux.KillSession(hd.ID); err != nil {
			return stopped, fmt.Errorf("killing session %s: %w", hd.Ref.CanonicalName(), err)
		}
		stopped = append(stopped, hd)
	}
	return stopped, nil
}

// SetStatus transitions an agent session's status and annotation, addressing
// it by canonical name.
//
// This is the one operation that does not take a Ref, and it is deliberate:
// `ch plugin handle-claude` receives a bare name from $CODEHERD_SESSION and
// cannot recover a Ref from it — the profile prefix is ambiguous, since
// work-myapp-feat could be profile "work" + project "myapp", or a project
// literally named "work-myapp". One narrow escape hatch beats re-exporting
// name resolution.
//
// Errors are suppressed: a hook must never fail the agent it is reporting on.
func (h *Herd) SetStatus(canonicalName string, status Status, annotation string) error {
	if canonicalName == "" {
		return nil
	}
	if status != StatusRunning && status != StatusWaiting {
		return nil
	}

	records, _ := h.tmux.ListSessions()
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == canonicalName && SessionType(r.SessionType) == SessionTypeAgent {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return nil // session not found — suppress
	}

	_ = h.tmux.SetOption(actualName, semconv.TmuxOptionStatus, string(status))
	_ = h.tmux.SetOption(actualName, semconv.TmuxOptionAnnotation, annotation)

	hasPrefix := strings.HasPrefix(actualName, semconv.StatusPrefix)
	if status == StatusRunning && hasPrefix {
		_ = h.tmux.RenameSession(actualName, strings.TrimPrefix(actualName, semconv.StatusPrefix))
	} else if status != StatusRunning && !hasPrefix {
		_ = h.tmux.RenameSession(actualName, semconv.StatusPrefix+actualName)
	}
	return nil
}

// handles lists every codeherd session tmux knows about, across all profiles.
// It is the single place a tmux record becomes a Handle — the six copies of
// this loop are what let Show and Stop disagree about identity.
func (h *Herd) handles() ([]Handle, error) {
	records, err := h.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}
	out := make([]Handle, 0, len(records))
	for _, r := range records {
		if r.CanonicalName == "" {
			continue // not a codeherd session
		}
		out = append(out, handleFrom(r))
	}
	return out, nil
}

func handleFrom(r tmux.SessionRecord) Handle {
	hd := Handle{
		ID:       r.ID,
		Ref:      Ref{Profile: r.Profile, Project: r.Project, Branch: r.Branch},
		Type:     SessionType(r.SessionType),
		TmuxName: r.Name,
		Status:   Status(r.Status),

		Annotation: r.Annotation,
	}
	if r.StartedAt != "" {
		hd.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}
	return hd
}

```

Add `"errors"` to the import block.

**One behaviour note for the reviewer.** `Handle.Ref` is rebuilt from `@codeherd_branch`, which stores the raw identity branch, and `Ref.CanonicalName()` re-flattens it — so `Resolve`'s comparison is against a re-derived canonical name, not the stored `@codeherd_canonical_name`. These agree for every session `Launch` created. If they ever disagree, the stored name is authoritative; prefer `r.CanonicalName` in the match if a test surfaces a mismatch, and record it in spec §14.1.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/herd/... -v -run 'Launch|Resolve|Sessions|StopSessions|SetStatus|Start'`
Expected: PASS, including the moved `TestStart_TriggersHooks` and the five new tests from Step 2.

- [ ] **Step 6: Migrate the session callers**

`cmd/services.go`: delete `newSessionService` (24-26), `showSessionForProfile` (50-64), `stopSessionForProfile` (68-80), `listSessionsForProfile` (84-100). The file is now just `newWorktreeService` — leave it; Task 5 empties it.

`cmd/session.go`:
- `ListSessionCmd.Run`: `sessions, err := h.Sessions()`; print `s.Ref.CanonicalName()`, `s.Type`, `s.Status`.
- `ShowSessionCmd.Run`: `info, err := h.Resolve(h.Ref(project, branch), c.sessionType())`; fields become `info.Ref.CanonicalName()`, `info.Type`, `info.Status`, `info.Annotation`, `info.StartedAt`.
- `CreateSessionCmd.Run` (160-285): the agent/shell resolution block (165-189), the `hooks.New` + `worktree.NewService` block (191-194), and the `session.NewService` + `Start` block (254-266) all collapse. The worktree-create-if-missing block (196-240) **stays as-is** until Task 5 — it still uses `wtSvc`. The tail becomes:
  ```go
  	handle, err := h.Launch(h.Ref(project, branch), herd.LaunchOpts{
  		Type:   c.sessionType(),
  		Agent:  flagAgent, // "" means defaults.agent — resolved inside Launch
  		Attach: c.Attach,
  	})
  	if err != nil {
  		fmt.Fprintln(cmd.OutOrStdout())
  		return sessionErr(cmd, err)
  	}
  	fmt.Fprintln(cmd.OutOrStdout(), "done")
  	if !c.Attach {
  		shellSuffix := ""
  		if c.Shell {
  			shellSuffix = " --shell"
  		}
  		fmt.Fprintf(cmd.OutOrStdout(), "Attach with: ch attach session %s %s%s\n", project, branch, shellSuffix)
  		return nil
  	}
  	return execTmuxAttach(handle.ID)
  ```
  Delete `resolveAgentName` (27-35) — `Launch` owns that fallback now. Its one other caller is `cmd/worktree.go:170`, handled below.
- `DeleteSessionCmd.Run`: the confirm probe becomes `h.Resolve(…)`; the stop becomes:
  ```go
  	if _, err := h.StopSessions(h.Ref(project, branch), herd.StopOpts{Type: sessionType}); err != nil {
  		fmt.Fprintln(cmd.OutOrStdout())
  		return sessionErr(cmd, err)
  	}
  ```
  `StopSessions` no longer errors when nothing is running, so the "not found" message now comes from the `Resolve` probe. With `--force` and no session, the command succeeds silently. That is a deliberate behaviour change: stopping an already-stopped session is not a failure. Note it in spec §14.1.
- `AttachSessionCmd.Run`: `info, err := h.Resolve(h.Ref(project, branch), sessionType)` then `execTmuxAttach(info.ID)`.
- `sessionTypeFromFlag` (371-376) returns `herd.SessionType`. Make it a method for symmetry: each command struct with a `Shell` field gets `func (c *XCmd) sessionType() herd.SessionType`. Or keep the free function returning `herd.SessionType` — either is fine; be consistent.

`cmd/worktree.go:165-199` (the `--attach` tail): delete the `resolveAgentName` + `cfg.AgentByName` + `session.NewService` + `Start` block. It becomes:
```go
	if c.Attach {
		flagAgent := ""
		if cmd.Flags().Changed("agent") {
			flagAgent = c.Agent
		}
		ref := h.Ref(project, branch)
		fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", ref.CanonicalName())
		handle, err := h.Launch(ref, herd.LaunchOpts{Agent: flagAgent, Attach: true})
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return sessionErr(cmd, err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "done")
		return execTmuxAttach(handle.ID)
	}
```
This silently fixes spec §3.1's second row for the `--attach` path: `semconv.SessionName("", project, branch)` at line 179 becomes `ref.CanonicalName()`, which carries the profile. The `Start` call at 183-191 also never passed `Profile` at all — under a profile it created an *unprefixed* session. Both die here.

`cmd/plugin.go:47-63`: delete the `tmux.NewClient` + `session.NewService` lines. `sesSvc.SetStatus(sessionName, semconv.StatusRunning, "")` → `h.SetStatus(sessionName, herd.StatusRunning, "")`, and likewise for the two `StatusWaiting` calls. Drop the `hooks`, `session`, and `tmux` imports.

**Watch out:** `plugin handle-claude` runs under `PersistentPreRunE`, so `h` is built. But `pluginCmd` is added directly to `rootCmd` in `init()` (line 79) — confirm `PersistentPreRunE` still runs for it. `cmd/plugin_test.go` covers this path; if `h` is nil there, the test will panic and tell you.

`cmd/errors.go`: `sessionErr` now matches `herd.ErrSessionExists` / `herd.ErrSessionNotFound` / `herd.ErrPathNotFound` / `herd.ErrNotCloned` / `herd.ErrWorktreeNotFound`. `*session.SessionExistsError` → `*herd.SessionExistsError`; its `sesErr.Project` / `.Branch` become `sesErr.Ref.Project` / `sesErr.Ref.Branch`. Leave `worktreeErr` alone and leave the `os.Exit(1)` alone — collapsing the two translators and fixing the exit is Plan 2's job (spec §12), and doing it here would change exit codes mid-collapse.

`cmd/tui.go:121`: delete `sesSvc := newSessionService()` and drop it from the `tui.NewModel` call.

`internal/tui/model.go`: delete the `sesSvc *session.Service` field (65). It is **assigned and never read** (spec §3.2) — deleting it is free. Drop the `session` import. Update `NewModel`'s signature and `cmd/tui.go`'s call.

`internal/tui/actions.go` — four `session.NewService(...)` + `Start(...)` sites (64-80, 121-134, 233-246, 417-430). Each becomes:
```go
	handle, err := hrd.Launch(hrd.Ref(project, branch), herd.LaunchOpts{Agent: agentName})
	if err != nil {
		return errMsg{err: err}
	}
	return attachMsg{session: handle.ID}
```
with `Type: herd.SessionTypeShell` for the `shellAction` site (233). The captured `profile` variable (`m.activeProfile()`) disappears from all four — the Ref carries it. `hrd` is `m.herd`, captured in the enclosing method exactly like `cfg` is today.

Note what this fixes for free: `shellAction` and `startSessionAfterCreate` pass `Branch: branch` where `branch` came from `sel.Branch` — the **display** branch. `hrd.Ref(project, branch)` has the same problem until Task 5 gives the TUI `Workspace.Ref`. Do not chase it here; Task 5 closes it.

`internal/tui/agent_picker.go:113-127`: same transformation; `pending.tmuxClient` and `pending.profile` are no longer needed for the Launch, but leave the fields — `pending` still carries `tmuxClient` for the worktree call until Task 5.

`internal/tui/delete_teardown_test.go`: the two regression tests build `Model{sesSvc: …, wtSvc: …}`. Drop `sesSvc` (the field is gone). Keep both tests passing against the current `confirmDeleteAll` — they are Task 5's target, not this one's.

`cmd/services_test.go`: it tests `showSessionForProfile` / `stopSessionForProfile` / `listSessionsForProfile`, all deleted. **Delete the file.** Its coverage moves to `internal/herd/session_test.go`'s profile tests — that is the same assertion, one layer down, where it belongs.

`cmd/session_internal_test.go`: it overrides `newSessionService` (a `var`), which no longer exists. Replace the seam: tests assign `h = herd.New(cfg, registry, herd.Deps{Tmux: fakeRunner, Git: fakeGit})` directly, since `h` is a package var in `cmd`. Read the file first — if a test depends on `newSessionService` returning a specific mock, the equivalent is a `herd.Deps{Tmux: …}` with the same fake.

- [ ] **Step 7: Delete `internal/session`**

```bash
git rm -r internal/session
```

Run: `go build ./... && go vet ./...` and `git grep -n 'internal/session'`
Expected: clean build; grep returns nothing.

- [ ] **Step 8: Verify**

Run: `make check`
Expected: green, ≥80%.

The integration tests are the ones to watch here: `cmd/session_integration_test.go` and `cmd/profiles_integration_test.go` drive real tmux. `TestProfiles_sessionIsolationAcrossProfiles` (`profiles_integration_test.go:136`) covers profile × {create, list} — it must still pass unchanged. If it fails, `Launch` is not stamping the profile the way `Start` did.

- [ ] **Step 9: Record and commit**

Append to spec §14.1 under `**Task 4 — session domain**`. Four things this task's own steps flagged as recordable — go back and collect them rather than trusting memory:

> Sessions now stamp `@codeherd_project` (new tmux option + `SessionRecord.Project`), so a `Handle` from a list carries a complete `Ref`. Sessions started before the upgrade have `Ref.Project == ""` and fail loudly in `Teardown` rather than deleting the wrong thing.
>
> **Behaviour change:** `ch delete session --force` no longer errors when nothing is running. `StopSessions` treats stopping nothing as success because `Teardown` calls it unconditionally.
>
> **Behaviour change:** `ch create worktree --attach` now starts a profile-prefixed session. It never did — `cmd/worktree.go:183-191` passed no `Profile` at all, so under a profile it created an unprefixed session that nothing else could address.
>
> `cmd/services_test.go` was deleted outright; it tested the three `*ForProfile` shims. Its coverage moved down a layer into `internal/herd/session_test.go`'s profile tests.

Also record: whether `Resolve` matching on a re-derived `CanonicalName()` rather than the stored `@codeherd_canonical_name` caused any mismatch (Step 4's note), and whether `PersistentPreRunE` actually runs for the `plugin` command (Step 6's warning — if `h` was nil there, say so; Plan 2 needs to know).

```bash
git add -A
git commit -m "refactor: fold session into internal/herd

session.Service was the only core service without config, so it could not
know the active profile, so Show and Stop took no profile parameter and
addressed sessions by a name rebuilt without one. You could create a
session you could not address.

Launch/Resolve/StopSessions all key on a Ref that carries the profile.
The six copies of the list-and-match loop collapse to one; ShowByName and
StopByName — the escape hatch for the missing parameter — are gone, and
so are cmd/services.go's three dispatch shims.

Sessions now stamp @codeherd_project, so a Handle from a list carries a
complete Ref. Model.sesSvc is deleted: it was assigned and never read."
```

---

### Task 5: worktree domain → `herd` — the defect dies

The last domain and the one that was already broken: `internal/worktree` imports `internal/tmux` and manages sessions with it (spec §2.2), without the profile, and therefore wrongly. Here `Teardown` sits beside the session code and calls it directly with the profile in hand.

**Files:**
- Create: `internal/herd/workspace.go`
- Create: `internal/herd/workspace_test.go` (moved from `internal/worktree/worktree_test.go`)
- Create: `internal/herd/integration_test.go` (moved from `internal/worktree/integration_test.go`)
- Modify: `cmd/services.go` (delete `newWorktreeService`; the file is now empty — delete it)
- Modify: `cmd/worktree.go`, `cmd/session.go`, `cmd/template.go`, `cmd/completion.go`, `cmd/tui.go`
- Modify: `internal/tui/model.go`, `actions.go`, `form.go`, `agent_picker.go`, `remote_picker.go`, `items.go`
- Modify: `internal/tui/delete_teardown_test.go`
- Delete: `internal/worktree/` (all files)

**Interfaces:**
- Consumes: everything from Tasks 2-4, especially `h.StopSessions`, `h.Clone`, `h.worktreePath`, `h.cloneDir`, `h.worktreesRoot`, `git.ParseRef`.
- Produces:
  ```go
  type Workspace struct {
      Ref           Ref    // identity — feed this back into any operation
      Path          string
      IsMain        bool
      DisplayBranch string // for rendering only; never an input
      HeadHint      string // "detached" | "on <branch>" | ""
      Agent, Shell  *Handle // nil when not running
  }

  type EnsureOpts struct {
      AutoClone  bool
      Provision  bool
      StartPoint string // --from
      Track      string // --track: "[<remote>/]<branch>"
  }

  type TeardownOpts struct{ Force bool }

  func (h *Herd) EnsureWorkspace(ref Ref, opts EnsureOpts) (Workspace, error)
  func (h *Herd) Provision(ref Ref) error
  func (h *Herd) List(project string) ([]Workspace, error) // "" = all projects
  func (h *Herd) Teardown(ref Ref, opts TeardownOpts) error
  func (h *Herd) RemoteBranches(project string, fetch bool) ([]RemoteBranch, error)
  ```

Note `RemoteBranches` takes a `fetch` bool rather than being two methods: `worktree.ListRemoteBranches` (completion, no fetch) and `worktree.RemoteBranches` (TUI picker, fetches first) differed by exactly one best-effort `FetchAll` line. One method, one named argument.

`EnsureOpts.Track` and `.StartPoint` are mutually exclusive; `EnsureWorkspace` returns an error if both are set. `cmd/worktree.go` already enforces this via `MarkFlagsMutuallyExclusive`, but the TUI form does not.

- [ ] **Step 1: Move the worktree tests**

Move `internal/worktree/worktree_test.go` → `internal/herd/workspace_test.go` and `internal/worktree/integration_test.go` → `internal/herd/integration_test.go`. Transform:
- line 1: `package worktree` → `package herd`
- Delete `mockHook` (16-20), `hookCall` (20-26), `mockGit` (243), `mockTmuxRunner` (319), `mockTmuxRunnerWithError` (329), `mockTmuxRunnerKillFails` (1056), `mockTmuxRunnerPerSession` (1071) — `fakes_test.go` supplies all of it. The four tmux mocks collapse into `fakeTmux` with `RunFn` or `Sessions` overrides; that collapse is the point of the shared fake.
- Delete the parser tests already moved to `internal/git` in Task 1 (they should already be gone — verify with `git grep -n 'parseWorktreePorcelain\|ParseRef' internal/herd/`).
- `makeService(t, git, tmuxRunner) (*Service, string)` → `workspaceHerd(t, g *fakeGit, f *fakeTmux) (*Herd, string)`, returning `New(cfg, nil, Deps{Tmux: f, Git: g})` with the same tmpDir + myapp config. Keep `cloneDirPath(tmpDir)` unchanged.
- `svc.New(p, b)` → `h.EnsureWorkspace(h.Ref(p, b), EnsureOpts{})`
- `svc.NewFrom(p, b, from)` → `h.EnsureWorkspace(h.Ref(p, b), EnsureOpts{StartPoint: from})`
- `svc.NewTracking(p, b, ref)` → `h.EnsureWorkspace(h.Ref(p, b), EnsureOpts{Track: ref})`. **Careful:** `NewTracking` derives the local branch from the remote ref when `branch` is empty, so the *result's* Ref may differ from the input Ref. `EnsureWorkspace` returns `Workspace`, and `Workspace.Ref` is authoritative — assert on that, not on the input.
- `svc.Delete(DeleteRequest{p, b, force})` → `h.Teardown(h.Ref(p, b), TeardownOpts{Force: force})`
- `svc.List(p)` → `h.List(p)`; entries are `Workspace` now, so `e.Session == "myapp-feat (running)"` becomes `e.Agent != nil`.
- `svc.WorktreePath(p, b)` → the unexported `h.worktreePath` plus an existence check; the tests that covered it fold into `EnsureWorkspace` / `Launch` coverage. If a test only asserted path derivation, it is already covered by `paths_test.go` — delete it rather than duplicating.
- `ErrNotCloned` etc. resolve locally now (same package).

Add the tests that make the shipped defect structurally impossible:

```go
// The defect, stated as a test. A worktree deleted under an active profile
// must take its sessions with it. worktree.Delete rebuilt the names with
// SessionName("", …), searched for myapp-feat, missed work-myapp-feat, and
// force-deleted the worktree anyway — leaving the agent process alive against
// a directory that no longer existed.
func TestTeardown_underProfile_killsSessionsThenDeletesWorktree(t *testing.T) {
	g := &fakeGit{}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "work-myapp-feat~sh", Canonical: "work-myapp-feat",
			Type: "shell", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: g})

	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := h.Teardown(h.Ref("myapp", "feat"), TeardownOpts{Force: true}); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	killed := f.killed()
	sort.Strings(killed)
	if len(killed) != 2 || killed[0] != "$1" || killed[1] != "$2" {
		t.Errorf("killed = %v, want [$1 $2]; a missed kill orphans the agent process", killed)
	}
	if !g.called("Remove", wtPath) {
		t.Errorf("worktree was not removed; calls=%v", g.Calls)
	}
}

// Without --force, a running session blocks the delete rather than being
// killed under the user.
func TestTeardown_runningSessionWithoutForce(t *testing.T) {
	g := &fakeGit{}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feat", Canonical: "myapp-feat",
			Type: "agent", Branch: "feat", Project: "myapp"},
	}}
	h, dir := workspaceHerd(t, g, f)
	if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := h.Teardown(h.Ref("myapp", "feat"), TeardownOpts{})
	if !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("err = %v, want ErrSessionRunning", err)
	}
	if len(f.killed()) != 0 {
		t.Errorf("killed %v without --force", f.killed())
	}
	if g.called("Remove") {
		t.Error("worktree was removed despite a running session")
	}
}

// List joins worktrees to sessions on the Ref, so the join is profile-correct.
// worktree.Service.List hardcoded SessionName("", …) at line 593, which is why
// `ch list worktree`'s "(running)" marker never appeared under a profile.
func TestList_underProfile_findsRunningSession(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "feat"}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(spaces))
	}
	if spaces[0].Agent == nil {
		t.Fatal("running agent session not joined to its workspace under a profile")
	}
	if spaces[0].Agent.ID != "$1" {
		t.Errorf("Agent.ID = %q, want $1", spaces[0].Agent.ID)
	}
}

// A diverged HEAD changes what we render, never what we address. This is the
// other half of the shipped defect: Item.Branch held the display branch and
// round-tripped into wtSvc.Delete.
func TestList_divergedHead_refKeepsIdentityBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		// The worktree was created for "feat" but HEAD now sits on "other".
		return []git.WorktreeInfo{{Path: wtPath, Branch: "other"}}, nil
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: &fakeTmux{}, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].Ref.Branch != "feat" {
		t.Errorf("Ref.Branch = %q, want %q — identity must survive divergence", spaces[0].Ref.Branch, "feat")
	}
	if spaces[0].DisplayBranch != "other" {
		t.Errorf("DisplayBranch = %q, want %q", spaces[0].DisplayBranch, "other")
	}
	if spaces[0].HeadHint != "on other" {
		t.Errorf("HeadHint = %q, want %q", spaces[0].HeadHint, "on other")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/herd/...`
Expected: FAIL — `undefined: Workspace`, `h.Teardown undefined`, `h.EnsureWorkspace undefined`.

- [ ] **Step 3: Write `internal/herd/workspace.go`**

`New` / `NewFrom` / `NewTracking` (`worktree.go:343-526`) are three near-identical 50-line methods differing only in which git call creates the worktree. They collapse into one `EnsureWorkspace` with a switch. `freshenStartPoint` (325-340) moves verbatim. `Provision` is new — it is the copy+template block that `cmd/worktree.go:133-160`, `cmd/session.go:207-236`, and `tui/actions.go:446-477` each spell out separately, and it is where spec §3.1's first row dies.

```go
package herd

import (
	"errors"
	"fmt"
	"os"

	"github.com/xico42/codeherd/internal/filecopy"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/semconv"
)

// Workspace is a worktree together with its sessions — the domain object the
// old split could not express.
type Workspace struct {
	// Ref is identity. Feed it back into any operation. It survives a
	// diverged HEAD, a profile switch, and a rename.
	Ref    Ref
	Path   string
	IsMain bool // true for the main clone dir

	// DisplayBranch is what a front end should render: the branch HEAD is
	// actually on. It is NOT identity and must never be fed back in — that
	// round-trip is what orphaned an agent against a deleted worktree.
	DisplayBranch string

	// HeadHint is "detached", "on <branch>", or "" when HEAD agrees with Ref.
	HeadHint string

	// Agent and Shell are nil when that session type is not running.
	Agent *Handle
	Shell *Handle
}

// EnsureOpts configures workspace creation. The zero value creates the
// worktree from the project's default branch and provisions nothing.
type EnsureOpts struct {
	AutoClone  bool   // clone the project first if it is not cloned
	Provision  bool   // run file copy + .herd templates after creating
	StartPoint string // --from: base the new branch on this ref
	Track      string // --track: "[<remote>/]<branch>"; derives the local name when Ref.Branch is ""
}

// TeardownOpts configures workspace deletion.
type TeardownOpts struct {
	Force bool // kill running sessions instead of refusing
}

// EnsureWorkspace makes the workspace for ref exist: clone if asked, create
// the worktree if missing, provision if asked. It is idempotent on the clone
// but not on the worktree — an existing worktree returns ErrWorktreeExists.
//
// The returned Workspace.Ref is authoritative: with Track, the local branch
// is derived from the remote ref and may differ from the ref passed in.
func (h *Herd) EnsureWorkspace(ref Ref, opts EnsureOpts) (Workspace, error) {
	if opts.StartPoint != "" && opts.Track != "" {
		return Workspace{}, errors.New("cannot combine a start point with a tracking ref")
	}

	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return Workspace{}, err
	}
	if opts.AutoClone {
		// Already cloned is the normal case, not a failure.
		if err := h.Clone(ref.Project); err != nil && !errors.Is(err, ErrAlreadyCloned) {
			return Workspace{}, err
		}
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return Workspace{}, fmt.Errorf("%w: %s", ErrNotCloned, ref.Project)
	}

	// A tracking ref decides the local branch name, so resolve it before the
	// ref is used for anything path-shaped.
	remoteRef := ""
	if opts.Track != "" {
		remotes, _ := h.git.Remotes(cloneDir)
		remote, remoteBranch, _ := git.ParseRef(remotes, opts.Track)
		if ref.Branch == "" {
			ref.Branch = remoteBranch
		}
		remoteRef = remote + "/" + remoteBranch
		if has, _ := h.git.HasLocalBranch(cloneDir, ref.Branch); has {
			return Workspace{}, fmt.Errorf("%w: %s", ErrLocalBranchExists, ref.Branch)
		}
		if err := h.git.Fetch(cloneDir, remote, remoteBranch); err != nil {
			return Workspace{}, fmt.Errorf("fetching %s: %w", remoteRef, err)
		}
	}

	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return Workspace{}, err
	}
	if _, err := os.Stat(wtPath); err == nil {
		return Workspace{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, ref.Project, ref.Branch)
	}

	p := h.cfg.Projects[ref.Project]
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: wtPath,
	}
	if err := hook.Trigger(semconv.HookPreWorktree, attrs, wtPath); err != nil {
		return Workspace{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	root, err := h.worktreesRoot(ref.Project)
	if err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	if err := h.addWorktree(ref, cloneDir, wtPath, remoteRef, opts); err != nil {
		return Workspace{}, err
	}

	if err := hook.Trigger(semconv.HookPostWorktree, attrs, wtPath); err != nil {
		return Workspace{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	if opts.Provision {
		if err := h.Provision(ref); err != nil {
			return Workspace{}, err
		}
	}

	return Workspace{
		Ref:           ref,
		Path:          wtPath,
		IsMain:        wtPath == cloneDir,
		DisplayBranch: ref.Branch,
	}, nil
}

// addWorktree runs the git call that actually creates the worktree. The three
// shapes were three near-identical 50-line methods; only this switch differed.
func (h *Herd) addWorktree(ref Ref, cloneDir, wtPath, remoteRef string, opts EnsureOpts) error {
	switch {
	case opts.Track != "":
		if err := h.git.AddTracking(cloneDir, wtPath, ref.Branch, remoteRef); err != nil {
			return fmt.Errorf("creating tracking worktree for %s: %w", remoteRef, err)
		}
		return nil

	case opts.StartPoint != "":
		startPoint := h.freshenStartPoint(cloneDir, opts.StartPoint)
		if err := h.git.AddNewBranchFrom(cloneDir, wtPath, ref.Branch, startPoint); err != nil {
			return fmt.Errorf("creating worktree from %s: %w", startPoint, err)
		}
		return nil

	default:
		// Try checking out an existing branch; fall back to branching from
		// the project's default.
		addErr := h.git.Add(cloneDir, wtPath, ref.Branch)
		if addErr == nil {
			return nil
		}
		src := h.cfg.Projects[ref.Project].DefaultBranch
		if src == "" {
			src = "main"
		}
		startPoint := h.freshenStartPoint(cloneDir, src)
		if err := h.git.AddNewBranchFrom(cloneDir, wtPath, ref.Branch, startPoint); err != nil {
			return fmt.Errorf("failed to create worktree (add: %v; add -b from %s: %w)", addErr, startPoint, err)
		}
		return nil
	}
}

// freshenStartPoint fetches updates for the source ref and returns the start
// point a new branch should be based on. It prefers a fast-forwarded local
// branch (to preserve un-pushed commits), falling back to the remote-tracking
// ref, or the raw ref when the source is not on a remote (tags, SHAs,
// local-only branches). All git failures here are best-effort.
func (h *Herd) freshenStartPoint(cloneDir, src string) string {
	remotes, _ := h.git.Remotes(cloneDir)
	remote, branch, explicit := git.ParseRef(remotes, src)
	if explicit {
		_ = h.git.Fetch(cloneDir, remote, branch)
		return src
	}
	if err := h.git.Fetch(cloneDir, "origin", src); err != nil {
		return src
	}
	if has, _ := h.git.HasLocalBranch(cloneDir, src); has {
		_ = h.git.FastForward(cloneDir, "origin", src)
		return src
	}
	return "origin/" + src
}

// Provision runs file copy and .herd template processing for a workspace.
//
// The template context is built from ref in one place, which is what kills
// the divergence where `ch create session` rendered a profile-qualified
// SessionName into a .herd file while `ch create worktree`, `ch template`,
// and the TUI rendered a profile-blind one — for the same worktree.
func (h *Herd) Provision(ref Ref) error {
	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return err
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return err
	}

	p := h.cfg.Projects[ref.Project]
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrWorktreePath: wtPath,
	}

	if len(p.Files) > 0 {
		if err := filecopy.New(hook).Copy(p.Files, cloneDir, wtPath, attrs); err != nil {
			return fmt.Errorf("copying files: %w", err)
		}
	}

	if _, err := herdtemplate.New(hook).Process(herdtemplate.ProcessContext{
		Project:      ref.Project,
		Branch:       ref.Branch,
		WorktreePath: wtPath,
		SessionName:  ref.CanonicalName(),
	}, attrs); err != nil {
		return fmt.Errorf("processing templates: %w", err)
	}
	return nil
}

// List returns every workspace for a project, or for all projects when
// project is "". Projects that are not cloned, and projects whose git calls
// fail, are skipped rather than failing the whole listing.
//
// This is the one place worktrees and sessions are joined, and the join is on
// the Ref — which carries the profile. The old split computed identity in
// worktree.Service.List, threw it away into a display string, and made the
// TUI recompute it.
func (h *Herd) List(project string) ([]Workspace, error) {
	names, err := h.projectNames(project)
	if err != nil {
		return nil, err
	}
	sessions, err := h.Sessions()
	if err != nil {
		return nil, err
	}
	byName := make(map[string][]Handle, len(sessions))
	for _, hd := range sessions {
		key := hd.Ref.CanonicalName()
		byName[key] = append(byName[key], hd)
	}

	var out []Workspace
	for _, name := range names {
		cloneDir, err := h.cloneDir(name)
		if err != nil {
			continue
		}
		if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
			continue
		}
		infos, err := h.git.List(cloneDir)
		if err != nil {
			continue
		}
		defaultBranch := h.cfg.Projects[name].DefaultBranch
		for _, wt := range infos {
			ws := h.workspaceFrom(name, cloneDir, defaultBranch, wt)
			for i := range byName[ws.Ref.CanonicalName()] {
				hd := byName[ws.Ref.CanonicalName()][i]
				switch hd.Type {
				case SessionTypeAgent:
					ws.Agent = &hd
				case SessionTypeShell:
					ws.Shell = &hd
				}
			}
			out = append(out, ws)
		}
	}
	return out, nil
}

// workspaceFrom derives identity and display from one git worktree entry.
func (h *Herd) workspaceFrom(project, cloneDir, defaultBranch string, wt git.WorktreeInfo) Workspace {
	identity := semconv.WorktreeIdentityBranch(wt.Path, cloneDir, defaultBranch, wt.Branch)
	ws := Workspace{
		Ref:           h.Ref(project, identity),
		Path:          wt.Path,
		IsMain:        wt.Path == cloneDir,
		DisplayBranch: wt.Branch,
	}
	switch {
	case wt.Detached:
		ws.HeadHint = "detached"
	case wt.Branch != "" && semconv.FlattenBranch(wt.Branch) != semconv.FlattenBranch(identity):
		ws.HeadHint = "on " + wt.Branch
	}
	return ws
}

// Teardown stops a workspace's sessions and deletes its worktree.
//
// The order is not incidental. The TUI killed sessions by ID and then called
// worktree.Delete, which ran a second, profile-blind kill loop that either
// missed or no-opped — and force-deleted the worktree either way, orphaning
// the agent process. One loop, keyed on a Ref that carries the profile.
func (h *Herd) Teardown(ref Ref, opts TeardownOpts) error {
	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s/%s", ErrWorktreeNotFound, ref.Project, ref.Branch)
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return err
	}

	if !opts.Force {
		running, err := h.handles()
		if err != nil {
			return err
		}
		canonical := ref.CanonicalName()
		for _, hd := range running {
			if hd.Ref.CanonicalName() == canonical {
				return fmt.Errorf("%w: %s (%s)", ErrSessionRunning, canonical, hd.Type)
			}
		}
	}

	if _, err := h.StopSessions(ref, StopOpts{All: true}); err != nil {
		return err
	}
	if err := h.git.Remove(cloneDir, wtPath); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	return nil
}

// RemoteBranches returns a project's remote-tracking branches. When fetch is
// true it refreshes all remotes first (best-effort) so the list reflects
// current remote state; completion passes false to stay fast.
func (h *Herd) RemoteBranches(project string, fetch bool) ([]RemoteBranch, error) {
	cloneDir, err := h.cloneDir(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	if fetch {
		_ = h.git.FetchAll(cloneDir)
	}
	branches, err := h.git.ListRemoteBranches(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("listing remote branches: %w", err)
	}
	return branches, nil
}
```

Note the loop-variable capture in `List`: `hd := byName[…][i]` takes a fresh copy per iteration before `&hd` is stored. Go 1.22+ scopes range variables per-iteration, but this indexes explicitly to make the copy obvious to a reviewer.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/herd/... -v`
Expected: PASS, including all four new tests from Step 1 and every moved worktree test.

`TestTeardown_underProfile_killsSessionsThenDeletesWorktree` passing is the moment the shipped defect is dead structurally — roughly two thirds of the way through Plan 1, exactly as spec §12.1 predicted.

- [ ] **Step 5: Migrate the worktree callers**

`cmd/services.go`: delete `newWorktreeService`. The file is now empty — `git rm cmd/services.go`.

`cmd/worktree.go`:
- `ListWorktreeCmd.Run`: `entries, err := h.List(project)`. Rendering changes: `e.Project` → `ws.Ref.Project`; `e.Branch` → `ws.DisplayBranch` (fall back to `"(detached)"` when empty, as today); `e.Path` → `ws.Path`; the `SESSION` column was `"<name> (running)"` or `"--"` and becomes:
  ```go
  	sess := "--"
  	if ws.Agent != nil {
  		sess = ws.Agent.Ref.CanonicalName() + " (running)"
  	}
  ```
  This is spec §3.1's second row fixed: the marker now appears under a profile.
- `CreateWorktreeCmd.Run`: the whole 112-160 block becomes one call:
  ```go
  	ws, err := h.EnsureWorkspace(h.Ref(project, posBranch), herd.EnsureOpts{
  		AutoClone:  false, // the CLI never auto-clones — previously implicit, now stated
  		Provision:  true,
  		StartPoint: c.From,
  		Track:      c.Track,
  	})
  	if err != nil {
  		fmt.Fprintln(cmd.OutOrStdout())
  		return worktreeErr(cmd, project, posBranch, err)
  	}
  ```
  Keep the three progress messages by branching on `c.Track != ""` / `c.From != ""` before the call, as today. The `--attach` tail from Task 4 now uses `ws.Ref` rather than `h.Ref(project, branch)` — `Track` may have derived a different local branch, and `ws.Ref` is authoritative.
- `DeleteWorktreeCmd.Run`: `h.Teardown(h.Ref(project, branch), herd.TeardownOpts{Force: c.Force})`.
- Drop the `config`, `filecopy`, `herdtemplate`, `hooks`, `semconv`, `session`, `tmux`, `worktree`, and `path/filepath` imports.

`cmd/session.go`, `CreateSessionCmd.Run`: the worktree-create-if-missing block (196-240) collapses to:
```go
	ref := h.Ref(project, branch)
	if _, err := h.EnsureWorkspace(ref, herd.EnsureOpts{Provision: true}); err != nil {
		if !errors.Is(err, herd.ErrWorktreeExists) {
			return worktreeErr(cmd, project, branch, err)
		}
		// Already there — that is the common case for `create session`.
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Worktree %s/%s not found, creating...  done\n", project, branch)
	}
```
Note the inversion: today the command probes `WorktreePath` and creates on `ErrWorktreeNotFound`; now it ensures and tolerates `ErrWorktreeExists`. Same outcome, one call. Drop the `config`, `filecopy`, `herdtemplate`, `hooks`, `filepath` imports.

`cmd/template.go:71-89`: `semconv.SessionName("", project, branch)` at line 83 is spec §3.1's first row. Replace the hand-built `ProcessContext` with `h.Provision(h.Ref(project, branch))` — **except** `ch template` supports `--dry-run` and an arbitrary `[dir]`, which `Provision` does not. Two options; take the second:
1. Add `ProvisionOpts{DryRun bool, Dir string}` — but `Dir` breaks `Provision`'s premise that paths derive from the Ref.
2. **Leave `cmd/template.go` calling `herdtemplate` directly, and change only line 83 to `h.Ref(project, branch).CanonicalName()`.** `ch template` is a one-off operation on a directory, not a worktree operation; it is the one caller that legitimately does not go through `herd`. The one-line fix kills the divergence.

Record this in spec §14.1: `Provision` does not subsume `ch template`, and `ch template`'s dry-run/arbitrary-dir shape is why.

`cmd/completion.go:69-70,109-110`: both `completionBranchLister` and `completionRemoteBrancher` are `var`s (overridden in tests). They become:
```go
var completionBranchLister = func(project string) ([]herd.Workspace, error) {
	return h.List(project)
}

var completionRemoteBrancher = func(project string) ([]herd.RemoteBranch, error) {
	return h.RemoteBranches(project, false) // no fetch — completion must stay fast
}
```
The `cfg *config.Config` parameter goes away (`h` is a package var). `branchNames(entries []worktree.ListEntry)` (90) takes `[]herd.Workspace` and reads `ws.Ref.Branch` — identity, which is what a user should be completing, not the display branch. Update `cmd/completion_internal_test.go`'s overrides to the new signatures.

`cmd/tui.go:119-125`:
```go
func runTUIDirect(tmuxClient *tmux.Client) error {
	insideTmux := os.Getenv("TMUX") != ""
	m := tui.NewModel(h, tmuxClient, insideTmux)
	…
}
```
`cfg`, `wtSvc`, and `registry` all come off `h` now (`h.Config()`, `h.Profile()`, `h.Profiles()`). Drop the `hooks` and `worktree` imports.

`internal/tui/model.go`:
- `Model`: delete `wtSvc`; keep `cfg` (many render paths read it) but source it from `m.herd.Config()`. Delete `registry` and `profileCache` — `Model.herd` plus `herd.Profiles()` / `herd.Profile()` replaces both. `switchProfile` becomes:
  ```go
  func (m Model) switchProfile(direction int) (Model, tea.Cmd) {
  	names := m.herd.Profiles()
  	if len(names) < 2 {
  		return m, nil
  	}
  	idx := indexOf(names, m.herd.Profile())
  	if idx < 0 {
  		return m, nil
  	}
  	n := len(names)
  	next := names[((idx+direction)%n+n)%n]

  	nextHerd, err := m.herd.WithProfile(next)
  	if err != nil {
  		m.statusMsg = fmt.Sprintf("profile switch failed: %v", err)
  		return m, nil
  	}
  	m.herd = nextHerd
  	m.cfg = nextHerd.Config()
  	m = m.syncProfileKeyEnabled()
  	m.statusMsg = "Switched to profile " + next
  	return m, m.refreshCmd()
  }
  ```
  The `profileCache` existed to avoid re-reading a profile TOML on every switch. `WithProfile` re-reads it. That is one small file read per keypress — acceptable, and it deletes the cache plus the shared-registry mutation and its race commentary (`model.go:621-624`). If a reviewer objects, the cache belongs on `Herd`, not on the TUI. `syncProfileKeyEnabled` reads `len(m.herd.Profiles()) > 1`.
- `refreshCmd` (462-559): ~100 lines collapse to:
  ```go
  func (m Model) refreshCmd() tea.Cmd {
  	hrd := m.herd
  	return func() tea.Msg {
  		spaces, err := hrd.List("")
  		if err != nil {
  			return errMsg{err: err}
  		}
  		return itemsMsg(buildItems(hrd, spaces))
  	}
  }
  ```
  The profile-snapshot comment (466-474) goes away with the registry mutation it guarded: `hrd` is an immutable value captured by pointer, and `WithProfile` returns a new one rather than mutating in place. That race is now structurally impossible — say so in the commit.
- `remoteBranchesMsg.branches` is `[]herd.RemoteBranch` (an alias for `git.RemoteBranch`, so the TUI need not import `git`).
- `fetchRemoteBranchesCmd` (654-662): `hrd.RemoteBranches(project, true)`.
- `showTrackForm` (690): `rb worktree.RemoteBranch` → `herd.RemoteBranch`.

`internal/tui/items.go`: `buildItems(data refreshResult)` → `buildItems(hrd *herd.Herd, spaces []herd.Workspace) []list.Item`. Delete `refreshResult`, `wtEntry`, `agentInfo`, `projEntry` — every one of them existed to carry data `Workspace` now carries. The identity derivation (85-108) — `WorktreeIdentityBranch`, `SessionName`, the divergence switch, the display fallbacks — **all deletes**; `herd.List` did it. `Item` gains a `Ref herd.Ref` field and keeps `Branch` for rendering:
```go
	item := Item{
		Ref:      ws.Ref,
		Project:  ws.Ref.Project,
		Branch:   ws.DisplayBranch,
		Path:     ws.Path,
		IsMain:   ws.IsMain,
		HeadHint: ws.HeadHint,
		HasShell: ws.Shell != nil,
	}
	if ws.Shell != nil {
		item.ShellSessionID = ws.Shell.ID
	}
	if ws.Agent != nil {
		item.Group = groupAgent
		item.HasAgent = true
		item.AgentStatus = string(ws.Agent.Status)
		item.AgentSessionID = ws.Agent.ID
		item.Annotation = ws.Agent.Annotation
	} else {
		item.Group = groupWorktree
	}
```
The project rows (134-143) need the uncloned-project list, which `List` does not return (it skips uncloned projects). Get it from `hrd.Projects()` + `hrd.Project(name)` for `Cloned`, inside `refreshCmd`, and pass it to `buildItems` as a second argument. Keep `buildItems`'s sort (145-161) verbatim.

`internal/tui/actions.go`:
- `confirmDeleteAll` (320-357) — the function this whole plan started from:
  ```go
  func (m Model) confirmDeleteAll() (tea.Model, tea.Cmd) {
  	ref := m.confirm.target.Ref // identity from herd.List — never a display string
  	hrd := m.herd
  	m.confirm, m.screen = nil, screenList

  	return m, func() tea.Msg {
  		if err := hrd.Teardown(ref, herd.TeardownOpts{Force: true}); err != nil {
  			return errMsg{err: err}
  		}
  		return m.refreshCmd()()
  	}
  }
  ```
  The kill-by-ID loop and its five-line comment go away — not because the hazard stopped mattering, but because `Teardown` is the only path and it kills by ID. Delete the comment with the code.
- `confirmDeleteAgent` / `confirmDeleteShell` (359-389): `hrd.StopSessions(ref, herd.StopOpts{Type: herd.SessionTypeAgent})` and `…SessionTypeShell`. They currently swallow the error with `_ =`; return `errMsg` on failure now that there is a real error to report.
- `attachAction`, `shellAction`, `startSessionAfterCreate`, `agent_picker.submit`: each does clone → worktree → copy → template → session by hand. Each becomes:
  ```go
  	if _, err := hrd.EnsureWorkspace(ref, herd.EnsureOpts{AutoClone: true, Provision: true}); err != nil && !errors.Is(err, herd.ErrWorktreeExists) {
  		return errMsg{err: err}
  	}
  	handle, err := hrd.Launch(ref, herd.LaunchOpts{Agent: agentName})
  	if err != nil {
  		return errMsg{err: err}
  	}
  	return attachMsg{session: handle.ID}
  ```
  `AutoClone: true` is spec §7.1's first row made explicit: the TUI auto-clones on attach and the CLI does not, and that is now an argument rather than a difference in which lines someone happened to write.
  Use `sel.Ref` for worktree/agent rows. For `groupProject` rows there is no `Ref` yet — mint one with `hrd.Ref(project, defaultBranch)` exactly as today.
- Delete `runFileCopyAndTemplate` (446-477) and `projectCloneDir` (437-443). `Provision` owns both. `runFileCopyAndTemplate`'s `semconv.SessionName("", proj, branch)` at line 471 is spec §3.1's first row, deleted rather than fixed.
- Drop the `filecopy`, `herdtemplate`, `hooks`, `semconv`, `session`, `worktree`, `projectpkg`, `filepath` imports.

`internal/tui/form.go:137-160`:
```go
	return func() tea.Msg {
		ws, err := hrd.EnsureWorkspace(hrd.Ref(project, branch), herd.EnsureOpts{
			AutoClone:  true,
			StartPoint: baseBranch,
			Track:      tracksRef,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return worktreeCreatedMsg{ref: ws.Ref, path: ws.Path, attach: attach, agent: agent}
	}
```
`worktreeCreatedMsg` (`model.go:42-48`) swaps its `project` + `branch` strings for a `ref herd.Ref`. `Provision` stays false here: `startSessionAfterCreate` provisions, and doing it in both would run the templates twice. Confirm against `model.go:283-286` — if `attach` is false, nothing provisions, which is **a bug that exists today** (`form.submit` never copies or templates; only the attach path does). Fix it: pass `Provision: true` here and drop it from `startSessionAfterCreate`. Note it in spec §14.1 as a defect found during the collapse.

`internal/tui/remote_picker.go`: `worktree.RemoteBranch` → `herd.RemoteBranch` (3 sites).

`internal/tui/delete_teardown_test.go`: both tests keep their names and their intent — they are the regression tests for the shipped defect and they carry forward against `Teardown` (spec §10). `Model{wtSvc: …}` becomes `Model{herd: New(cfg, reg, Deps{Tmux: client})}`; `newConfirmModel(Item{Project: …, Branch: …, AgentSessionID: …})` gains `Ref: herd.Ref{…}`. `TestConfirmDeleteAll_divergedHeadSessionIsKilled` keeps `Branch: "other"` (the display value) **and** sets `Ref: {Project: "myapp", Branch: "feat"}` — that divergence is exactly what it tests, and it now cannot reach `Teardown`.

- [ ] **Step 6: Delete `internal/worktree`**

```bash
git rm -r internal/worktree
```

Run: `go build ./... && go vet ./...` and `git grep -n 'internal/worktree\|internal/session\|internal/project'`
Expected: clean build; grep returns nothing.

- [ ] **Step 7: Verify**

Run: `make check`
Expected: green, ≥80%.

Then verify the defect is dead end-to-end, not just in a unit test. This exercises the real thing — spec §3.1's third row said the old kill loop was always dead code, so a passing unit test alone is not proof:

```bash
export CODEHERD_TMUX_SOCKET=$(mktemp -d)/tmux.sock
make build
# with a profile active, create a worktree + agent session, then delete the
# worktree from the TUI and confirm no orphaned tmux session or agent process:
tmux -S "$CODEHERD_TMUX_SOCKET" list-sessions
```
Expected after the delete: `no server running` or a list with neither `<profile>-<project>-<branch>` nor its `~sh` sibling. Before this task, the agent session survived.

- [ ] **Step 8: Record and commit**

Append to spec §14.1 under `**Task 5 — worktree domain**`. This is the largest stage and the one Plan 2 depends on most. Collect from its own steps:

> `Provision` does not subsume `ch template`. `ch template` takes an arbitrary `[dir]` and supports `--dry-run`, neither of which fits `Provision`'s premise that paths derive from the `Ref`. It keeps its direct `herdtemplate` call and its own `hooks.New` — the only front end that legitimately does not go through `herd`. Its profile-blind `SessionName("", …)` was fixed in place.
>
> **Behaviour change:** `ch list worktree` now shows `(running)` under a profile. `worktree.go:593` hardcoded `""`, so the marker never appeared.
>
> **Behaviour change / bug fix:** the TUI's create-worktree form now provisions when not attaching. `form.submit` never ran file copy or templates; only the attach path did. Found during the collapse, not introduced by it.
>
> `RemoteBranches(project, fetch bool)` replaced `ListRemoteBranches` + `RemoteBranches`, which differed by one best-effort `FetchAll`.
>
> The TUI's `profileCache` was deleted rather than moved: `WithProfile` re-reads the profile TOML per switch. Record whether that was noticeable — if it is, the cache belongs on `Herd`, not on the TUI.

Then answer §14.1's remaining prompts, which only this task can answer:

- Did `Ref` with exported fields hold up, or did an unguarded `herd.Ref{…}` slip in? (§6.1 / §11)
- Did the ~13 methods on `Herd` still feel right after writing them all?
- Is `herd`'s real size near §8.5's ~1,550-line estimate? Did the file split survive?
- Did the session/worktree tests move intact, or need rewriting? (§10 — the known rewrites are `session_test.go`'s `Start` tests, which must now create real worktree dirs since `Launch` derives the path)
- Did the shared `fakeGit` / `fakeTmux` hold up against five hand-rolled tmux mocks and two git mocks?

```bash
git add -A
git commit -m "refactor: fold worktree into internal/herd — the defect dies

internal/worktree imported internal/tmux and managed sessions with it —
without the profile, and therefore wrongly. Nothing prevented it from
importing internal/session; it reimplemented the logic against the raw
tmux client instead. The enforced boundary caused the defect.

Teardown now sits beside the session code and calls it with the profile
in hand: one kill loop, by ID, profile-correct by construction. The TUI's
duplicate loop and worktree.Delete's always-dead one both go away.

Also dead: the profile-blind SessionName(\"\", …) literal (all 9 sites),
the three near-identical New/NewFrom/NewTracking methods, the copy+template
block that was spelled out in three front ends, and the TUI's refreshCmd
+ buildItems identity derivation, which recomputed what List now returns.

Fixes: 'ch list worktree' shows (running) under a profile; .herd templates
render one SessionName regardless of which command created the worktree;
'ch create worktree --attach' no longer starts an unprefixed session under
a profile; the TUI's create-worktree form now provisions when not attaching."
```

---

### Task 6: close out Plan 1 and hand off

Plan 1's contract (spec §12): the three packages are gone, `cmd`/`tui` compile against `herd`, the defect is dead structurally, coverage holds. This task proves it and writes the handoff — **the handoff is not optional**. Plans 2 and 3 are written in fresh sessions whose only context is the spec.

**Files:**
- Modify: `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` (§12 status table, §14.1)
- Modify: `CLAUDE.md` (package layout)

- [ ] **Step 1: Prove the contract**

Run each and record the actual number:

```bash
git grep -n 'internal/worktree\|internal/session\|internal/project'   # want: no output
git grep -n 'semconv.SessionName'                                     # want: only internal/herd/herd.go
git grep -rn 'tmux.NewClient(tmux.NewRealRunner())'                   # was 10; want: cmd/tui.go only
git grep -rn 'hooks.New(' -- cmd internal/tui                         # was 13; want: no output
find internal/herd -name '*.go' -not -name '*_test.go' | xargs wc -l  # spec §8.5 predicted ~1,550
make check
```

`git grep -n 'semconv.SessionName'` returning only `herd.go` is the whole plan in one command: nine profile-blind literals, gone, with no place to come back to.

Some of these will not be fully clean yet — `hooks.New` in `cmd/template.go` legitimately survives (Step 5 of Task 5 explains why), and `cmd/errors.go` still has its two translators and its `os.Exit`. Those are Plan 2's scope. Record what is actually left rather than forcing it.

- [ ] **Step 2: Update `CLAUDE.md`**

The "Package layout" section lists `internal/session`, `internal/worktree`, `internal/project` — all deleted. Replace those three bullets with one:

```markdown
- **`internal/herd`** — the domain. Owns projects, worktrees, and sessions together, because they are one thing: a workspace. `Herd` holds `cfg` + the active profile + the exec-boundary runners; `Ref` is identity (always the *identity* branch, never the display branch) and always carries the profile. Obtain a `Ref` from `h.Ref(project, branch)` or `Workspace.Ref` — never build one by hand. Operations: `EnsureWorkspace`, `Launch`, `List`, `Resolve`, `StopSessions`, `Teardown`, `Clone`, `Provision`, `SetStatus`. One error vocabulary lives here.
- **`internal/git`** — mechanism; `WorktreeRunner` + `CloneRunner` + `Runner` union, `RealRunner`, porcelain parsers. Never sees `cfg` or the profile.
```

Update the "Key design patterns" bullets that name the old packages: "Mocking via interfaces" now says `internal/tmux` exposes `Runner` and `internal/git` exposes `Runner`; tests fake at those two seams. Add:

```markdown
- **Domain vs mechanism**: needs `cfg`, the profile, or identity to decide something → `internal/herd`. Does not → a support package. This is why `filecopy` and `herdtemplate` stayed out: they never needed the profile, which is why they were never implicated in the profile bugs.
- **Never build a `herd.Ref` by hand**: `h.Ref(project, branch)` supplies the profile; a literal `herd.Ref{Project: p, Branch: b}` is silently addressing the no-profile world. This is the convention that replaced `semconv.SessionName("", …)`, which failed nine times.
```

- [ ] **Step 3: Curate the handoff — spec §14.1**

Tasks 1-5 each appended their own notes under `**Task N — …**` headings, in their own commits. **Do not rewrite them from memory — you do not have it.** Read what is there:

```bash
git log --oneline -- docs/superpowers/specs/2026-07-15-herd-domain-package-design.md
```

Your job is curation, not reconstruction:

1. **Read §14.1 top to bottom.** Delete the `_Not started_` placeholder and the commented-out prompt block — the prompts have either been answered by a task or turned out not to apply.
2. **Merge the five per-task sections into one narrative**, organised by §14's categories (assumptions wrong / API changes / decisions reversed / traps / deferred work), not by task number. A reader of Plan 2 cares what is true now, not which stage discovered it. Keep the §-references — "§10 was wrong about X" is the useful form, because it tells the next session which paragraph to distrust.
3. **Collect the behaviour changes into one list.** Four are known going in (Tasks 4 and 5 recorded them); there may be more. This list is also the changelog entry when this ships, so write it for a user, not for a reviewer.
4. **Add what only this task can measure:** the grep counts from Step 1, `herd`'s real line count and file split vs §5/§8.5's ~1,550 estimate, and the final coverage number.
5. **Add what Plan 2 inherits**, which is the section Plan 2's author reads first: `cmd/errors.go`'s two translators and its `os.Exit`-inside-`RunE`; `cmd/template.go`'s surviving `hooks.New` + direct `herdtemplate` call; whether `Model.cfg` can go away entirely; anything a task flagged as deferred.
6. **Do not smooth over a contradiction.** If Task 3 recorded that folding `project` in looked right and Task 5 recorded that `herd` feels too big, both go in. §13 called `project` the weakest link and reversible; a later session needs the disagreement, not a consensus you invented.

If a task recorded nothing, that is a real signal — say "Tasks 1-2 surfaced nothing worth recording" rather than leaving a reader wondering whether they were skipped.

Then set §12's status table: Plan 1 → `done`.

- [ ] **Step 4: Verify and commit**

Run: `make check`
Expected: green.

```bash
git add -A
git commit -m "docs: record Plan 1 handoff and update the package layout

internal/session, internal/worktree, and internal/project are gone.
semconv.SessionName has exactly one caller left, inside herd, so the
profile-blind literal has nowhere to come back to.

Records the real API against the spec's §6 sketch, the four behaviour
changes this refactor shipped, and what Plan 2 inherits."
```

---

## What Plan 1 does not do

Named here so a reviewer does not flag them as omissions:

- **`cmd/errors.go` keeps its two translators and its `os.Exit(1)` inside `RunE`.** Spec §9 and Plan 2. Collapsing them mid-collapse would change exit codes while the domain underneath is still moving.
- **The TUI still prints raw error text** instead of matching sentinels. Spec §9 and Plan 2 — the vocabulary it needs now exists in one package, which is the prerequisite.
- **Front ends are not yet thin.** Line counts will not hit spec §8.5's estimates; that is Plan 2's target, and Plan 2's first task should re-measure rather than trust the estimate.
- **The profile × operation integration matrix is not filled.** Spec §10's three gap cells — `StopSessions`, `Teardown`, `Resolve` under an active profile — get *unit* coverage here (Tasks 4 and 5) but not integration coverage against real tmux. That is Plan 3, and it is the gate that would have caught the original defect.
