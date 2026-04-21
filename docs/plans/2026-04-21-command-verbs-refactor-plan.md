# Command Verbs Refactor Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape the codeherd CLI from `<subject> <verb>` to `<verb> <subject>`, remove the unimplemented remote-execution phase and its config scaffolding, unify agent and shell sessions under one concept, and refactor each cobra command into a self-contained struct.

**Architecture:** Bottom-up. First demolish the dead remote phase and dead config surface. Then unify session types in `internal/session` and ripple signature changes through its callers (`cmd/session.go`, `internal/tui`, `internal/worktree`). Finally refactor the CLI in `cmd/` into struct-per-command using a central `cmd/register.go` wire-up. Docs update last, as a single mechanical sweep.

**Tech Stack:** Go 1.x, Cobra CLI, tmux, `internal/config` (TOML-backed), `charm.land/bubbletea/v2` (TUI), `testify` (tests).

**Spec:** `docs/plans/2026-04-21-command-verbs-refactor-design.md`

---

## Conventions

- Every task ends with `make check` (coverage ≥ 80% + integration + lint + build) and a focused commit.
- Every commit uses Conventional Commits: `feat:`, `refactor:`, `test:`, `docs:`, `chore:`. Co-author trailers are optional and left to the implementer's preference — the project has mixed practice here.
- The build must be green at the end of every task. If a task touches callers of a changed API, update all callers in the same task.
- No deprecation aliases. No backward-compat shims.
- File paths in this plan are absolute from the repo root.

## Pre-flight audit

Before starting, run this grep to see every stale command-grammar reference you'll be touching across the refactor:

```bash
grep -rn 'session start\|session stop\|session attach\|session show\|session list\|worktree new\|worktree shell\|worktree template\|worktree delete\|worktree list\|project list\|project show\|project clone\|--no-create\|--token\|ch up\|ch down\|ch status\|ch ssh\|ch config' cmd/ internal/ docs/ README.md CLAUDE.md plugins/ 2>/dev/null
```

This is a map of the total work, not a task — each task below updates the matching hits in its scope. The final grep in Task 21 confirms all have been cleared.

## File Structure

### Files deleted

```
cmd/up.go
cmd/down.go
cmd/status.go
cmd/ssh.go
cmd/config.go
cmd/config_test.go
internal/do/              (entire package)
internal/provision/       (entire package)
internal/state/           (entire package)
internal/config/errors_test.go
```

### Files created

```
cmd/register.go           verb-grouper + struct-command wire-up
cmd/services.go           newWorktreeService, newSessionService, newProjectService
cmd/errors.go             worktreeErr, sessionErr (new grammar strings)
cmd/tui.go                runTUI*, createCodeherdSession, respawnIfDead (extracted from root.go)
```

### Files rewritten

```
cmd/root.go               shrunk: drop --token, --no-color, AddGroup; call registerCommands()
cmd/project.go            ListProjectCmd, ShowProjectCmd, CloneProjectCmd
cmd/worktree.go           ListWorktreeCmd, CreateWorktreeCmd, DeleteWorktreeCmd
cmd/session.go            ListSessionCmd, ShowSessionCmd, CreateSessionCmd, DeleteSessionCmd, AttachSessionCmd
cmd/template.go           TemplateCmd
cmd/plugin.go             unchanged logic; just verify build
internal/config/config.go shrunk (see Chunk 1 Task 3)
internal/session/session.go unified agent/shell (see Chunk 2)
internal/worktree/worktree.go updated Delete (see Chunk 2 Task 8)
internal/tui/actions.go   shell action routed through session.Service (see Chunk 2 Task 9)
```

### Docs updated

```
README.md
CLAUDE.md
docs/project.md
plugins/claude/README.md
```

---

## Chunk 1: Demolition

Remove all dead code — remote stubs, config CLI, remote internal packages, and the dead config fields. At the end of this chunk the build is green and the CLI surface is just the survivors (project/worktree/session/template/plugin), still under the old `<subject> <verb>` grammar.

### Task 1: Delete remote stub CLI commands

**Why:** `ch up`, `ch down`, `ch status`, `ch ssh` all print `not implemented` and do nothing. Remove them and the `remote` cobra group.

**Files:**
- Delete: `cmd/up.go`
- Delete: `cmd/down.go`
- Delete: `cmd/status.go`
- Delete: `cmd/ssh.go`
- Modify: `cmd/root.go` — drop the `remote` entry from `rootCmd.AddGroup(...)`.
- Modify: `cmd/root_test.go` — remove `"up"`, `"down"`, `"status"`, `"ssh"` from any subcommand iteration in `TestExecute_Subcommands`.

- [ ] **Step 1: Inspect the test to find what needs updating**

```bash
grep -n 'up\|down\|status\|ssh' cmd/root_test.go
```

- [ ] **Step 2: Delete the four stub files**

```bash
rm cmd/up.go cmd/down.go cmd/status.go cmd/ssh.go
```

- [ ] **Step 3: Drop the `remote` group from `cmd/root.go`**

Remove the entry `&cobra.Group{ID: "remote", Title: "Remote Execution (planned):"}` from the `AddGroup` call. The other three groups (`sessions`, `projects`, `config`) stay for now; they go away later when the verb reshape lands.

- [ ] **Step 4: Update `cmd/root_test.go`**

Two places to edit:

1. `TestExecute_Subcommands` — remove `"up"`, `"down"`, `"status"`, `"ssh"` from the iteration list. Verify the updated test still asserts coverage of the remaining subcommands.
2. `TestExecute_ConfigLoadError` (around line 49–56) — this test passes `"up"` as the subcommand purely to exercise `PersistentPreRunE`'s config-load branch. After deletion `"up"` yields an unknown-command error, which still returns non-nil so the test passes *by coincidence* for the wrong reason. Replace `"up"` with a surviving subcommand that does reach PersistentPreRunE, e.g. `"project"`. Note: in Chunk 1 `project` is still registered as the old `<subject> <verb>` form; once Chunk 3 lands it will be reachable via the verb grouper — either way, `"project"` is a valid subcommand at the point this test runs.

- [ ] **Step 5: Run make check and confirm green**

```bash
make check
```

Expected: all tests pass, lint clean, coverage ≥ 80%, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add -A cmd/
git commit -m "$(cat <<'EOF'
refactor: delete unimplemented remote-execution stub commands

Remove ch up, ch down, ch status, ch ssh. They were stubs that printed
"not implemented" and reserved CLI surface for a remote-droplet phase
that never materialized. Also drop the 'remote' cobra group from root.
EOF
)"
```

---

### Task 2: Delete config CLI subtree and drop root flag plumbing

**Why:** The `config` command tree (`init`, `show`, `get`, `set`, `profile *`) plus the root `--token` / `--no-color` flags and `ApplyEnv`/`ApplyFlags` hooks exist almost entirely to service the now-deleted remote phase. Rip them out; a future design will decide what config CLI (if any) codeherd needs.

**Files:**
- Delete: `cmd/config.go`
- Delete: `cmd/config_test.go`
- Modify: `cmd/root.go` — drop `token` variable, `noColor` variable, both `--token` and `--no-color` persistent-flag declarations, and the `cfg.ApplyEnv()` / `cfg.ApplyFlags(token)` calls inside `PersistentPreRunE`. Drop the `config` entry from `rootCmd.AddGroup(...)`.
- Modify: `cmd/root_test.go` — remove `"config"` from any subcommand iteration.

- [ ] **Step 1: Inspect root.go PersistentPreRunE and flag wiring**

```bash
sed -n '20,70p' cmd/root.go
```

- [ ] **Step 2: Delete the config CLI files**

```bash
rm cmd/config.go cmd/config_test.go
```

- [ ] **Step 3: Edit `cmd/root.go`**

Changes:
- Remove `token string` and `noColor bool` from the package-level `var` block.
- Remove the two `PersistentFlags().StringVar(&token, ...)` / `...BoolVar(&noColor, ...)` calls.
- Inside `PersistentPreRunE`, remove the `cfg.ApplyEnv()` and `cfg.ApplyFlags(token)` calls so the body becomes just: load cfg, return.
- Remove the `config` group from `AddGroup`. The remaining groups (`sessions`, `projects`) stay for now.

- [ ] **Step 4: Update `cmd/root_test.go`**

Remove `"config"` from the subcommand iteration list.

- [ ] **Step 5: Run make check**

```bash
make check
```

Expected: green. If `internal/config` tests fail because `cmd/config.go`'s deletion exposed an import cycle or unused symbol, that's a real issue — read the error, narrow scope.

- [ ] **Step 6: Commit**

```bash
git add -A cmd/
git commit -m "$(cat <<'EOF'
refactor: delete config CLI subtree and root flag plumbing

Drop ch config init/show/get/set/profile*, the --token flag, the
--no-color flag (declared but never read), cfg.ApplyEnv(),
cfg.ApplyFlags(), and the 'config' cobra group. These served the
remote-droplet phase that was deleted in the previous commit; a future
design will decide whether codeherd needs a CLI-driven config at all.
EOF
)"
```

---

### Task 3: Shrink `internal/config`

**Why:** With the config CLI gone, most of `internal/config` is dead. Strip the package down to what the surviving commands (session, worktree, project, template, TUI) actually read.

**Files:**
- Modify: `internal/config/config.go` — remove the fields, methods, and constants listed below.
- Delete: `internal/config/errors_test.go` (covers only `SetKey` / `DeleteSection`, which are being removed).
- Modify: `internal/config/config_test.go` — remove tests that exercise removed surface.
- Modify: `go.mod` / `go.sum` via `go mod tidy` if `go-playground/validator/v10` becomes unused.

**Remove from `DefaultsConfig`:**
- `Token`, `SSHKeyID`, `Region`, `Size`, `TailscaleAuthKey`, `Image`, `GitIdentityFile` fields.
- The `defaultImage` constant and its seeding in `Load`.

**Remove from `Config`:**
- `Profiles map[string]ProfileConfig` field and the `ProfileConfig` struct entirely.
- `Profile()` method.

**Remove standalone:**
- `ApplyEnv()`, `ApplyFlags()` methods.
- `SetKey()`, `DeleteSection()`, `IsValidKeyPath()` functions.
- `Redact()` function.
- `Validate()` method (plus the `configValidator` package-level `var` and the `validator` import).
- The `sync.Once` key-map machinery (`keyInitOnce`, `defaultsKeys`, `profileKeys`, `projectKeys`, `initKeyMaps`, `tomlFields`).
- `extractPreamble()` (only used by `SetKey`).

**Keep:** `Config`, `DefaultsConfig{ProjectsDir, Agent}`, `ProjectConfig`, `AgentConfig`, `Load`, `Save`, `Path`, `RepoPath`, `AgentByName`, `AgentNames`, `expandTilde`, `expandPaths`, `defaultProjectsDir`.

**Test file:** In `internal/config/config_test.go`, delete these test functions (list may be approximate — run the test file and delete any that reference removed surface):

```
TestConfig_ApplyEnv
TestConfig_ApplyFlags
TestConfig_Validate
TestConfig_Redact
TestIsValidKeyPath
TestSetKey_*
TestDeleteSection_*
TestConfig_Profile
TestConfig_SetKey_CreatesNewProfile
TestExtractPreamble
```

Also strip field-specific assertions on `Token`, `SSHKeyID`, `Region`, `Size`, `Image`, `TailscaleAuthKey`, `GitIdentityFile` from surviving tests (typically `TestLoad_*`, `TestLoad_ExistingFile`, `TestSave_*`).

- [ ] **Step 1: Open and survey the two files**

```bash
wc -l internal/config/config.go internal/config/config_test.go internal/config/errors_test.go
```

Review what's there to understand the surgery.

- [ ] **Step 2: Rewrite `internal/config/config.go`**

Keep only the kept symbols listed above. The file should shrink from ~340 lines to roughly 90–120.

- [ ] **Step 3: Delete `internal/config/errors_test.go`**

```bash
rm internal/config/errors_test.go
```

- [ ] **Step 4: Trim `internal/config/config_test.go`**

Delete the named test functions above and strip field-specific assertions on removed fields.

- [ ] **Step 5: Run `go mod tidy` if the validator dep became unused**

```bash
go mod tidy
```

If `github.com/go-playground/validator/v10` disappears from `go.mod`, good. If it remains (used elsewhere), leave it.

- [ ] **Step 6: Run make check**

```bash
make check
```

Expected: green. If any caller still expects a removed field (e.g. a test outside `internal/config` or a TUI file), fix it — likely it's reading a field that never mattered anyway.

- [ ] **Step 7: Commit**

```bash
git add -A internal/config/ go.mod go.sum
git commit -m "$(cat <<'EOF'
refactor: shrink internal/config to only what implemented commands read

Remove DefaultsConfig fields that served only the remote phase (Token,
SSHKeyID, Region, Size, Image, TailscaleAuthKey, GitIdentityFile),
the defaultImage constant and its seeding, ProfileConfig and its
Profiles map, ApplyEnv/ApplyFlags, SetKey/DeleteSection/IsValidKeyPath,
Redact, Validate, and the related key-map machinery. Surviving API:
Config, DefaultsConfig{ProjectsDir, Agent}, ProjectConfig, AgentConfig,
Load, Save, Path, RepoPath, AgentByName, AgentNames.
EOF
)"
```

---

### Task 4: Delete remote-phase internal packages

**Why:** `internal/do` (Digital Ocean API wrapper), `internal/provision` (cloud-init template rendering), and `internal/state` (droplet state JSON) have no remaining callers after Tasks 1–3. Delete them.

**Files:**
- Delete: `internal/do/` (entire directory)
- Delete: `internal/provision/` (entire directory)
- Delete: `internal/state/` (entire directory)
- Modify: `go.mod` / `go.sum` via `go mod tidy` to drop `github.com/digitalocean/godo` and any other now-unused deps.

- [ ] **Step 1: Verify no remaining imports**

```bash
grep -rn 'internal/do\|internal/provision\|internal/state' --include='*.go' .
```

Expected: no results. If any remain, they're in code that survives — stop and investigate before deleting.

- [ ] **Step 2: Remove the directories**

```bash
rm -rf internal/do internal/provision internal/state
```

- [ ] **Step 3: Tidy modules**

```bash
go mod tidy
```

Expected: `github.com/digitalocean/godo` leaves `go.mod`.

- [ ] **Step 4: Run make check**

```bash
make check
```

- [ ] **Step 5: Commit**

```bash
git add -A internal/ go.mod go.sum
git commit -m "$(cat <<'EOF'
refactor: delete remote-phase internal packages

Remove internal/do, internal/provision, and internal/state. No
callers remain after the CLI demolition in the prior commits.
EOF
)"
```

---

## Chunk 2: Session Unification (internal/)

Unify agent and shell sessions under one `session.Service`. The TUI stops creating tmux sessions directly; every tmux-backed session flows through the service. `worktree.Service.Delete` learns to kill both types.

At the end of this chunk, `internal/` is done. The CLI is still under the old `<subject> <verb>` grammar; Chunk 3 reshapes it.

### Task 5: Add `Type` to `session.StartRequest` and `SessionInfo`; update `Start`

**Why:** Foundation for the unified session model. `Start` learns to branch on `Type`, pick the right tmux name (`<p>-<b>` for agent, `<p>-<b>~sh` for shell), type-scope the existence check, and set `@codeherd_session_type` from the request.

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`

**Design decisions:**
- `StartRequest.Type` defaults to `semconv.SessionTypeAgent` when empty (back-compat for existing callers that don't set it).
- Tmux name depends on type: `SessionName(p, b)` for agent, `ShellSessionName(p, b)` for shell. Canonical name stays `SessionName(p, b)` for both.
- Existence check scopes by `(CanonicalName == <p>-<b>) && (SessionType == req.Type)`. Without this, creating a shell session for a branch that already has an agent session would spuriously return `ErrSessionExists`.
- `SessionInfo` gains a `Type` field populated from the tmux option.

- [ ] **Step 1: Write a failing test for creating a shell session alongside an agent session**

Add to `internal/session/session_test.go`:

```go
func TestService_Start_ShellAndAgentCoexist(t *testing.T) {
    path := t.TempDir()
    r := newMockRunner()
    svc := newService(t, r)

    // Start an agent session
    _, err := svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeAgent, Cmd: "agent",
    })
    if err != nil {
        t.Fatalf("agent Start: %v", err)
    }

    // Starting a shell session for the same project/branch must succeed
    _, err = svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
    })
    if err != nil {
        t.Fatalf("shell Start coexisting with agent: %v", err)
    }

    // Re-starting the same type must fail with ErrSessionExists
    _, err = svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeAgent, Cmd: "agent",
    })
    if !errors.Is(err, session.ErrSessionExists) {
        t.Fatalf("duplicate agent Start: want ErrSessionExists, got %v", err)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/session/ -run TestService_Start_ShellAndAgentCoexist -v
```

Expected: FAIL (the new `Type` field doesn't exist yet and existence check is not type-scoped).

- [ ] **Step 3: Update `StartRequest` and `Start`**

In `internal/session/session.go`:

Add `Type string` to `StartRequest`. Inside `Start`:
- Default `req.Type` to `semconv.SessionTypeAgent` when empty.
- Derive `tmuxName`: `SessionName(p, b)` if agent, `ShellSessionName(p, b)` if shell.
- Scope the existence check: iterate `records`, match both `CanonicalName == name` AND `SessionType == req.Type`.
- Pass `tmuxName` to `NewSessionWithEnv` and to the `SetOption` calls.
- Set `@codeherd_session_type` to `req.Type`.
- Keep canonical name (`SessionName(p, b)`) as the value for `TmuxOptionCanonicalName`.

Add `Type string` to `SessionInfo`.

- [ ] **Step 4: Run the new test — it should pass**

```bash
go test ./internal/session/ -run TestService_Start_ShellAndAgentCoexist -v
```

Expected: PASS.

- [ ] **Step 5: Run the full `internal/session` test suite**

```bash
go test ./internal/session/ -v
```

Expected: all existing tests pass. If existing tests assume agent is the only type, they'll pass unchanged because `Type` defaults to agent.

- [ ] **Step 6: Commit**

```bash
git add internal/session/
git commit -m "$(cat <<'EOF'
feat(session): add Type to StartRequest and SessionInfo

Add Type string to StartRequest (defaults to SessionTypeAgent when
empty). Start now derives the tmux session name from Type (agent uses
SessionName, shell uses ShellSessionName), type-scopes the existence
check, and records @codeherd_session_type on the new session.
SessionInfo gains a Type field so List/Show callers can disambiguate.
EOF
)"
```

---

### Task 6: Remove the agent-only filter in `List`; populate `SessionInfo.Type`

**Why:** Today `Service.List()` filters `r.SessionType != semconv.SessionTypeAgent`, so shell sessions are invisible. Remove that filter; return all codeherd-tagged sessions; let callers render or filter.

**Files:**
- Modify: `internal/session/session.go` (`List` method)
- Modify: `internal/session/session_test.go` (new test covering shell sessions appearing)

- [ ] **Step 1: Write a failing test that lists shell sessions**

Add to `internal/session/session_test.go`:

```go
func TestService_List_IncludesShellSessions(t *testing.T) {
    path := t.TempDir()
    r := newMockRunner()
    svc := newService(t, r)

    _, _ = svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeAgent, Cmd: "agent",
    })
    _, _ = svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
    })

    infos, err := svc.List()
    if err != nil { t.Fatal(err) }
    if len(infos) != 2 {
        t.Fatalf("List returned %d, want 2", len(infos))
    }
    var sawAgent, sawShell bool
    for _, i := range infos {
        switch i.Type {
        case semconv.SessionTypeAgent: sawAgent = true
        case semconv.SessionTypeShell: sawShell = true
        }
    }
    if !sawAgent || !sawShell {
        t.Fatalf("List missing a type: agent=%v shell=%v", sawAgent, sawShell)
    }
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/session/ -run TestService_List_IncludesShellSessions -v
```

Expected: FAIL.

- [ ] **Step 3: Remove the filter and populate Type**

In `Service.List`:
- Delete the `if r.SessionType != semconv.SessionTypeAgent { continue }` line.
- Populate `info.Type = r.SessionType` on each returned `SessionInfo`.

- [ ] **Step 4: Run the new test — it should pass**

```bash
go test ./internal/session/ -run TestService_List_IncludesShellSessions -v
```

- [ ] **Step 5: Run full internal/session tests**

```bash
go test ./internal/session/ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/session/
git commit -m "$(cat <<'EOF'
feat(session): List returns sessions of all types

Drop the SessionType == agent filter in Service.List so shell
sessions appear alongside agent sessions. Populate SessionInfo.Type
from the tmux option; callers can filter or render per type.
EOF
)"
```

---

### Task 7: Change `Show` / `Stop` to `(project, branch, sessionType)` and update all callers

**Why:** The spec's session-addressing contract: every session verb takes `<project> <branch>` plus optional type. Opaque tmux names do not leak into the CLI. `SetStatus` is unchanged (canonical-name-only, agent-type-scoped — it serves the Claude hook path).

**Files:**
- Modify: `internal/session/session.go` (`Show`, `Stop` signatures)
- Modify: `internal/session/session_test.go` (update all call sites)
- Modify: `cmd/session.go` (callers of `Show` and `Stop`)
- Modify: `internal/tui/actions.go` (callers of `Stop`)

**Signature changes:**

```go
// Before:
func (s *Service) Show(name string) (*SessionInfo, error)
func (s *Service) Stop(name string) error

// After:
func (s *Service) Show(project, branch, sessionType string) (*SessionInfo, error)
func (s *Service) Stop(project, branch, sessionType string) error
```

Both resolve by `(CanonicalName == SessionName(project, branch)) && (SessionType == sessionType)`. Empty `sessionType` defaults to `SessionTypeAgent`.

- [ ] **Step 1: Update `Show` and `Stop` in `internal/session/session.go`**

Replace the single-argument signatures. Update error messages to use `<project>/<branch>` formatting:

```go
return nil, fmt.Errorf("%w: %s/%s (%s)", ErrSessionNotFound, project, branch, sessionType)
```

- [ ] **Step 2: Update `internal/session/session_test.go` call sites**

Every `svc.Show("app-main")` becomes `svc.Show("app", "main", semconv.SessionTypeAgent)`. Same for `Stop`. Add one new test case covering shell-type Show:

```go
func TestService_Show_ShellType(t *testing.T) {
    path := t.TempDir()
    r := newMockRunner()
    svc := newService(t, r)
    _, _ = svc.Start(session.StartRequest{
        Project: "app", Branch: "main", Path: path,
        Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
    })
    info, err := svc.Show("app", "main", semconv.SessionTypeShell)
    if err != nil { t.Fatal(err) }
    if info.Type != semconv.SessionTypeShell {
        t.Fatalf("Show().Type = %q, want shell", info.Type)
    }
    // Agent-type Show must return ErrSessionNotFound
    if _, err := svc.Show("app", "main", semconv.SessionTypeAgent); !errors.Is(err, session.ErrSessionNotFound) {
        t.Fatalf("agent Show for shell-only session: want ErrSessionNotFound, got %v", err)
    }
}
```

- [ ] **Step 3: Update callers in `cmd/session.go`**

In `sessionShowCmd.RunE`:
- Replace `args[0]` single-arg use with parsing two args as `project, branch`.
- Pass `semconv.SessionTypeAgent` as `sessionType` (we wire the `--shell` flag properly in Chunk 3 when we reshape the command; for now keep it agent-only so this task stays focused).

In `sessionAttachCmd.RunE`:
- Same pattern: parse two args, pass `SessionTypeAgent`.

In `sessionStopCmd.RunE`:
- Same pattern: parse two args, pass `SessionTypeAgent`.
- Update the user-facing confirmation string to use `<project>/<branch>`.

**Note:** These commands also need their `Use` strings updated to reflect two args, and `Args: cobra.ExactArgs(2)`. This is transitional — Chunk 3 replaces the whole file with struct-based commands.

- [ ] **Step 4: Update callers in `internal/tui/actions.go`**

Find the two `sesSvc.Stop(canonicalName)` calls (`confirmDeleteAll` and `confirmDeleteAgent`). Change each to:

```go
_ = sesSvc.Stop(project, branch, semconv.SessionTypeAgent)
```

`project` and `branch` are already in scope in both functions.

**Note about tests that pass coincidentally:** `cmd/session_test.go` contains `TestSessionAttach_tooFewArgs`, `TestSessionStop_tooFewArgs`, and `TestSessionShow_tooFewArgs`, which pass 0 args to commands that currently require 1. After this task they require 2 — the tests still pass because 0 args still fail arg validation, but for the wrong reason. This is acceptable transitional state; Chunk 3 Task 15 rewrites these tests properly when the full command surface is reshaped.

**Integration test is untouched by this task:** `cmd/session_integration_test.go` (build-tagged `integration`) only exercises `session start` (which this task does not modify) and `--no-create` (still wired). It remains green here. It gets reshaped in Chunk 3 Task 15 when `session start` is replaced by `create session` and `--no-create` is dropped.

- [ ] **Step 5: Run full test suite**

```bash
make test
```

Expected: all unit tests pass. If `cmd/session_test.go` asserts on old command semantics (e.g. passing a single session name), update those assertions to the new two-arg form.

- [ ] **Step 6: Run `make check`**

```bash
make check
```

Expected: green.

- [ ] **Step 7: Commit**

```bash
git add -A internal/ cmd/
git commit -m "$(cat <<'EOF'
refactor(session): address Show/Stop by (project, branch, type)

Change Service.Show and Service.Stop to take (project, branch,
sessionType) instead of a single canonical name. Internal tmux names
(<p>-<b>~sh) no longer leak into the CLI or TUI surface. Update all
callers: cmd/session.go transitional two-arg parsing, internal/tui
action handlers pass sessionType=agent explicitly. SetStatus is
unchanged — hook callers only have the canonical name from
CODEHERD_SESSION, and status transitions apply to agent sessions only.
EOF
)"
```

---

### Task 8: Update `worktree.Service.Delete --force` to kill both session types

**Why:** A worktree can have an agent session, a shell session, or both. `--force` must leave nothing running against the removed path.

**Files:**
- Modify: `internal/worktree/worktree.go` (`Delete` method)
- Modify: `internal/worktree/worktree_test.go` (new test case)

- [ ] **Step 1: Inspect the current Delete implementation**

```bash
grep -n 'Delete\|SessionName\|ShellSessionName\|KillSession' internal/worktree/worktree.go
```

- [ ] **Step 2: Write a failing test**

Add to `internal/worktree/worktree_test.go`:

```go
func TestService_Delete_Force_KillsBothSessionTypes(t *testing.T) {
    // Set up a worktree with an agent session and a shell session both active.
    // Call Delete with Force=true.
    // Assert: both tmux sessions were killed.
}
```

(Follow the existing test harness pattern — look at a similar existing test for the setup boilerplate. The test should observe the `KillSession` calls via the mock tmux runner.)

- [ ] **Step 3: Verify test fails**

```bash
go test ./internal/worktree/ -run TestService_Delete_Force_KillsBothSessionTypes -v
```

- [ ] **Step 4: Update `Delete`**

Where `Delete` currently checks for and (under `Force`) kills `SessionName(p, b)`, extend it to also kill `ShellSessionName(p, b)` when that session exists.

Ignore errors from `KillSession` when the session doesn't exist — pattern match on the tmux error or use a "list first, filter to existing, kill" approach that matches how `Delete` handles the agent session today.

- [ ] **Step 5: Run the new test**

```bash
go test ./internal/worktree/ -run TestService_Delete_Force_KillsBothSessionTypes -v
```

Expected: PASS.

- [ ] **Step 6: Run `make check`**

```bash
make check
```

- [ ] **Step 7: Commit**

```bash
git add internal/worktree/
git commit -m "$(cat <<'EOF'
feat(worktree): Delete --force kills both agent and shell sessions

Under the unified session model a worktree can have two tmux-backed
sessions. Extend Service.Delete to kill the shell-type session
(<p>-<b>~sh) in addition to the agent-type session (<p>-<b>) when
--force is passed, so 'ch delete worktree ... --force' leaves no
dangling sessions attached to the removed path.
EOF
)"
```

---

### Task 9: Route TUI shell-session creation through `session.Service`

**Why:** The TUI's shell action currently creates tmux sessions directly (`tmuxClient.NewSession(shellName, path)`) and sets `@codeherd_session_type=shell` by hand. Move this through `session.Service.Start` with `Type: SessionTypeShell` so there's one code path for every tmux-backed session.

**Files:**
- Modify: `internal/tui/actions.go` (the shell action around line 180–224)
- Modify: `internal/tui/actions_test.go` (rewrite `TestShellAction_*` against a mocked `sesSvc.Start` call)

- [ ] **Step 1: Read the current shell action**

```bash
sed -n '180,225p' internal/tui/actions.go
```

- [ ] **Step 2: Replace the direct-tmux block**

Replace the tail of the `return func() tea.Msg { ... }` body (starting at `shellName := semconv.ShellSessionName(...)`) with a `session.Service.Start` call.

Important: `h` (the `hooks.Hook`) is *only* in scope inside the `if branch == ""` group-3 fallback block in the current function. In the normal worktree-item flow, `h` does not exist. Construct it explicitly:

```go
// If the shell session already exists, attach by stable session ID.
if shellSessionID != "" {
    return attachMsg{session: shellSessionID}
}

shellCmd := os.Getenv("SHELL")
if shellCmd == "" { shellCmd = "/bin/sh" }

// Always resolve hook config from the selected project. The group-3 branch
// above may have already done this, but the worktree-item path does not.
projCfg := cfg.Projects[project]
h := hooks.New(projCfg.Hooks)

sesSvc := session.NewService(tmuxClient, h)
sessionID, err := sesSvc.Start(session.StartRequest{
    Project: project,
    Branch:  branch,
    Path:    path,
    Type:    semconv.SessionTypeShell,
    Cmd:     shellCmd,
})
if err != nil {
    return errMsg{err: err}
}
return attachMsg{session: sessionID}
```

The `shellSessionID != ""` short-circuit stays — reattach is not the same as create.

The `h` variable declared inside the `if branch == ""` block shadows the new outer `h`. That's fine, but to avoid lint confusion you may rename the inner one (e.g. `bootH`) or hoist the construction up. Either works; pick whichever keeps the diff smallest.

- [ ] **Step 3: Rewrite `TestShellAction_*`**

Open `internal/tui/actions_test.go` and find the test block (around lines 845–958 per the spec review). Before this task, the test asserted on two tmux calls: `NewSession(shellName, path)` and two `SetOption` calls.

After this task, the action calls `session.Service.Start`, which funnels through `tmux.Runner` as the following call sequence (verify against the current `session.Service.Start` implementation):

1. `ListSessions` — for the existence check
2. `NewSessionWithEnv(shellName, path, env, cmd)` — where `env` contains `CODEHERD_SESSION=<p>-<b>`
3. `SetOption(shellName, "@codeherd_status", "running")`
4. `SetOption(shellName, "@codeherd_started_at", <rfc3339>)`
5. `SetOption(shellName, "@codeherd_canonical_name", "<p>-<b>")`
6. `SetOption(shellName, "@codeherd_session_type", "shell")`
7. `SessionID(shellName)` — to return the stable ID

Two options for the test rewrite:

**Option A (preferred for minimal diff):** keep `mockRunner`, extend the pre-seeded command responses to cover the full sequence above. For most of the existing `TestShellAction_*` cases, that means adding stub responses for `ListSessions` (returning "no matches") and for the additional `SetOption` calls. Assertions then shift from "was `NewSession` called?" to "was `NewSessionWithEnv` called with Type=shell env and shell command?". The `attachMsg.session` value being the return of `SessionID()` also changes what the test asserts about the attach target.

**Option B (cleaner, more work):** introduce a `sessionStarter` interface on `Model` that wraps just `Start(req session.StartRequest) (string, error)`. The TUI's shell action calls `m.sessionStarter.Start(...)`. Tests inject a fake. This requires threading a new dependency through `tui.NewModel` and `runTUIDirect`.

Go with Option A unless the test file's existing mocks make it painful. Option B is the right structural choice but adds churn outside the scope of this task.

Record your choice in the commit message.

- [ ] **Step 4: Run TUI tests**

```bash
go test ./internal/tui/ -v
```

- [ ] **Step 5: Run `make check`**

```bash
make check
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "$(cat <<'EOF'
refactor(tui): route shell-session creation through session.Service

The TUI's shell action previously bypassed session.Service and built
tmux sessions directly. Under the unified session model, every tmux-
backed session flows through session.Service.Start with an explicit
Type field. The TUI's shell action now does the same with
Type: SessionTypeShell and Cmd: $SHELL. No behavioral change for
users; removes the duplicate code path.
EOF
)"
```

---

## Chunk 3: CLI Struct Refactor

With the internal APIs settled, reshape the CLI to `<verb> <subject>` and refactor every command into a struct. At the end of this chunk the user-facing grammar changes from `ch worktree new` to `ch create worktree`, from `ch session start` to `ch create session`, etc.

### Task 10: Extract TUI launch code from `cmd/root.go` to `cmd/tui.go`

**Why:** `cmd/root.go` currently holds `runTUI`, `runTUIInTmux`, `runTUIDirect`, `createCodeherdSession`, `respawnIfDead`, and the tmux-attach helpers. Split them out so `root.go` stays focused on rootCmd wiring and `tui.go` owns TUI launch semantics. Pure code move; no logic changes.

**Files:**
- Create: `cmd/tui.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Move the functions**

Cut `runTUI`, `runTUIInTmux`, `runTUIDirect`, `createCodeherdSession`, `respawnIfDead`, and `execTmuxAttach` (if in root.go, else leave where it is) into a new `cmd/tui.go`. Keep the same `package cmd` and imports scoped to what's used.

- [ ] **Step 2: Verify `root.go` no longer imports anything only those functions used**

```bash
goimports -l cmd/root.go cmd/tui.go
```

If either file has unused imports, fix.

- [ ] **Step 3: `make check`**

```bash
make check
```

- [ ] **Step 4: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): extract TUI launch code to cmd/tui.go

Pure code move. cmd/root.go retains only rootCmd wiring and
PersistentPreRunE; TUI launch semantics (runTUI, runTUIInTmux,
runTUIDirect, createCodeherdSession, respawnIfDead) move to
cmd/tui.go. No behavior change.
EOF
)"
```

---

### Task 11: Create `cmd/services.go` and `cmd/errors.go`

**Why:** Factor the existing service-construction helpers (`newWorktreeService`, `newSessionService`, `newProjectService`) into one file, and collect the existing error-printer helpers (`worktreeErr`, `sessionErr`) with their strings rewritten for the new grammar.

**Files:**
- Create: `cmd/services.go`
- Create: `cmd/errors.go`
- Modify: `cmd/worktree.go` — remove the migrated helpers.
- Modify: `cmd/session.go` — remove the migrated helpers.

**`cmd/services.go`:** contains one function per service. Each reads the package-level `cfg` directly. No DI, no parameters. These helpers wire `&hooks.NoOp{}` for hooks — they are for **read-only** paths (`list`, `show`) where hooks don't fire. Write paths (`clone`, `create`, `delete`, `start`) construct their own service inline with a project-bound `hooks.New(projCfg.Hooks)` because the hook needs the project config.

```go
package cmd

import (
    "github.com/xico42/codeherd/internal/hooks"
    "github.com/xico42/codeherd/internal/project"
    "github.com/xico42/codeherd/internal/session"
    "github.com/xico42/codeherd/internal/tmux"
    "github.com/xico42/codeherd/internal/worktree"
)

// newWorktreeService returns a *worktree.Service for read-only paths
// (list, show). Write paths (create, delete) construct their own service
// inline because they need hooks bound to the project's config.
func newWorktreeService() *worktree.Service {
    return worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}

// newSessionService returns a *session.Service for read-only paths
// (list, show). Create/delete paths construct their own service inline
// with a project-bound hook.
func newSessionService() *session.Service {
    return session.NewService(tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}

// newProjectService returns a *project.Service for read-only paths
// (list, show). Clone constructs its own service inline with a
// project-bound hook.
func newProjectService() *project.Service {
    return project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})
}
```

This is a deliberate split. The alternative — factories that take a project name and build a hook — is cleaner but would hide the hook-invocation contract inside the helper; leaving write-path construction inline keeps the hook calls visible at the call site.

**`cmd/errors.go`:** contains `worktreeErr` and `sessionErr`, with user-facing strings rewritten to the new grammar:

```
"Run 'ch project clone %s' first"       → "Run 'ch clone project %s' first"
"Run 'ch worktree new %s %s' first"     → "Run 'ch create worktree %s %s' first"
"Attach with 'ch session attach %s'"    → "Attach with 'ch attach session %s %s'"  (now takes project + branch)
"Stop it first or use --force"          → unchanged text
```

- [ ] **Step 1: Create `cmd/services.go` with the three helpers**

- [ ] **Step 2: Create `cmd/errors.go` with `worktreeErr` / `sessionErr`**

Port the existing bodies from `cmd/worktree.go` and `cmd/session.go`. Update the strings listed above.

- [ ] **Step 3: Delete the helpers from `cmd/worktree.go` and `cmd/session.go`**

Leave the rest of those files alone — they'll be rewritten in Tasks 14 and 15.

- [ ] **Step 4: `make check`**

Expected: green. If the error-string rewrites break any test that asserts on exact error text, update the test assertion. Do not revert the string change.

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): extract services and error helpers

Factor service-constructor helpers into cmd/services.go and
error-printer helpers into cmd/errors.go. Error strings reference
the new verb-subject grammar (ch clone project, ch create worktree,
ch attach session). No behavior change beyond the error text.
EOF
)"
```

---

### Task 12: Create `cmd/register.go` skeleton and rewire `cmd/root.go`

**Why:** Introduce the central wire-up function that builds the verb-grouper tree and registers struct commands under each verb. Keep the old `<subject> <verb>` commands wired in parallel for now so the build stays green until each subject file is converted (Tasks 13–16). Each subject conversion removes the old wiring and adds the new.

**Files:**
- Create: `cmd/register.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create `cmd/register.go` with an initially-empty `registerCommands`**

```go
package cmd

import "github.com/spf13/cobra"

// registerCommands wires verb groupers and the struct-backed subject
// commands under them. Called once from root.init().
func registerCommands(root *cobra.Command) {
    listCmd   := &cobra.Command{Use: "list",   Short: "List resources"}
    createCmd := &cobra.Command{Use: "create", Short: "Create resources"}
    deleteCmd := &cobra.Command{Use: "delete", Short: "Delete resources"}
    showCmd   := &cobra.Command{Use: "show",   Short: "Show details for a resource"}
    cloneCmd  := &cobra.Command{Use: "clone",  Short: "Clone a project from remote"}
    attachCmd := &cobra.Command{Use: "attach", Short: "Attach to a running session"}

    // Subject wiring fills in during Tasks 13–16.

    root.AddCommand(listCmd, createCmd, deleteCmd, showCmd, cloneCmd, attachCmd)
}
```

- [ ] **Step 2: Call `registerCommands(rootCmd)` from `cmd/root.go`'s `init()`**

Add at the end of `init()`:

```go
registerCommands(rootCmd)
```

The old `<subject> <verb>` cobra.Commands stay wired via their existing `init()` functions — during transition we have both trees. Users see both forms in `ch --help` until the next tasks remove the old wiring.

- [ ] **Step 3: `make check`**

Expected: green. Run `./ch --help` manually to visually confirm both command trees coexist.

- [ ] **Step 4: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
feat(cmd): add register.go with verb-grouper skeleton

Introduce the central wire-up function that will host the struct-
backed <verb> <subject> commands. Verb groupers (list/create/delete/
show/clone/attach) are in place as bare *cobra.Command with no
subjects yet — those land per-subject in the next tasks, each
replacing the old <subject> <verb> wiring as it goes.
EOF
)"
```

---

### Task 13: Refactor `cmd/project.go` into struct commands

**Why:** First subject conversion. Replace the package-level `projectCmd`, `projectListCmd`, `projectShowCmd`, `projectCloneCmd` vars with `ListProjectCmd`, `ShowProjectCmd`, `CloneProjectCmd` structs. Remove the old `init()` that registers `projectCmd` under root; register the structs via `register.go` instead.

**Files:**
- Modify: `cmd/project.go`
- Modify: `cmd/register.go`
- Modify: `cmd/project_test.go` (if it exists — verify; currently may not)

- [ ] **Step 1: Inspect existing `cmd/project.go`**

```bash
wc -l cmd/project.go; ls cmd/project_test.go 2>&1
```

- [ ] **Step 2: Rewrite `cmd/project.go` to struct form**

Replace the whole file with three struct types and their `Cobra()` methods:

```go
package cmd

import (
    "errors"
    "fmt"
    "sort"
    "text/tabwriter"

    "github.com/spf13/cobra"

    "github.com/xico42/codeherd/internal/hooks"
    "github.com/xico42/codeherd/internal/project"
)

// ── ListProjectCmd ───────────────────────────────────────────────────

type ListProjectCmd struct{}

func (c *ListProjectCmd) Cobra() *cobra.Command {
    return &cobra.Command{
        Use:     "project",
        Aliases: []string{"projects", "pr"},
        Short:   "List all configured projects",
        Args:    cobra.NoArgs,
        RunE:    c.Run,
    }
}

func (c *ListProjectCmd) Run(cmd *cobra.Command, args []string) error {
    svc := newProjectService()
    entries := svc.List()
    w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
    fmt.Fprintln(w, "NAME\tREPO\tBRANCH")
    for _, e := range entries {
        fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.Config.Repo, e.Config.DefaultBranch)
    }
    return w.Flush()
}

// ── ShowProjectCmd ───────────────────────────────────────────────────

type ShowProjectCmd struct{}

func (c *ShowProjectCmd) Cobra() *cobra.Command {
    return &cobra.Command{
        Use:     "project <name>",
        Aliases: []string{"projects", "pr"},
        Short:   "Show config for a project",
        Args:    cobra.ExactArgs(1),
        RunE:    c.Run,
    }
}

func (c *ShowProjectCmd) Run(cmd *cobra.Command, args []string) error {
    svc := newProjectService()
    e, err := svc.Show(args[0])
    if err != nil { return fmt.Errorf("show project: %w", err) }
    cloned := "no"
    if e.Cloned { cloned = "yes" }
    w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
    fmt.Fprintf(w, "Name:\t%s\n", e.Name)
    fmt.Fprintf(w, "Repo:\t%s\n", e.Config.Repo)
    fmt.Fprintf(w, "Branch:\t%s\n", e.Config.DefaultBranch)
    fmt.Fprintf(w, "Path:\t%s\n", e.Path)
    fmt.Fprintf(w, "Cloned:\t%s\n", cloned)
    return w.Flush()
}

// ── CloneProjectCmd ──────────────────────────────────────────────────

type CloneProjectCmd struct {
    All bool
}

func (c *CloneProjectCmd) Cobra() *cobra.Command {
    cmd := &cobra.Command{
        Use:     "project [<name>]",
        Aliases: []string{"projects", "pr"},
        Short:   "Clone a project's repo into projects_dir",
        RunE:    c.Run,
    }
    cmd.Flags().BoolVar(&c.All, "all", false, "clone all configured projects")
    return cmd
}

func (c *CloneProjectCmd) Run(cmd *cobra.Command, args []string) error {
    if c.All {
        names := make([]string, 0, len(cfg.Projects))
        for name := range cfg.Projects { names = append(names, name) }
        sort.Strings(names)
        hadFailure := false
        for _, name := range names {
            projCfg := cfg.Projects[name]
            h := hooks.New(projCfg.Hooks)
            svc := project.NewService(cfg, project.NewRealGitRunner(), h)
            err := svc.Clone(name)
            switch {
            case err == nil:
                fmt.Fprintf(cmd.OutOrStdout(), "Cloning %s... done\n", name)
            default:
                var ace *project.AlreadyClonedError
                if errors.As(err, &ace) {
                    fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", ace)
                } else {
                    fmt.Fprintf(cmd.ErrOrStderr(), "Error: failed to clone %s: %v\n", name, err)
                    hadFailure = true
                }
            }
        }
        if hadFailure { return fmt.Errorf("one or more clones failed") }
        return nil
    }

    if len(args) == 0 {
        return fmt.Errorf("requires a project name, or use --all")
    }
    name := args[0]
    projCfg := cfg.Projects[name]
    h := hooks.New(projCfg.Hooks)
    svc := project.NewService(cfg, project.NewRealGitRunner(), h)
    fmt.Fprintf(cmd.OutOrStdout(), "Cloning %s... ", name)
    err := svc.Clone(name)
    switch {
    case err == nil:
        fmt.Fprintln(cmd.OutOrStdout(), "done")
        if e, showErr := svc.Show(name); showErr == nil {
            fmt.Fprintf(cmd.OutOrStdout(), "  Path: %s\n", e.Path)
        }
    default:
        fmt.Fprintln(cmd.OutOrStdout())
        var ace *project.AlreadyClonedError
        if errors.As(err, &ace) {
            fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", ace)
        } else {
            return fmt.Errorf("failed to clone %s: %w", name, err)
        }
    }
    return nil
}
```

No `init()` in this file.

- [ ] **Step 3: Register the three structs in `cmd/register.go`**

Inside `registerCommands`, after creating the verb groupers:

```go
listCmd.AddCommand((&ListProjectCmd{}).Cobra())
showCmd.AddCommand((&ShowProjectCmd{}).Cobra())
cloneCmd.AddCommand((&CloneProjectCmd{}).Cobra())
```

- [ ] **Step 4: `make check`**

Expected: green. `ch list project`, `ch show project <n>`, `ch clone project [<n>]` all work. The old `ch project list/show/clone` paths are gone (the old `projectCmd` was registered from a now-deleted `init()`).

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): convert project subject to struct commands

Replace package-level projectCmd / projectListCmd / projectShowCmd /
projectCloneCmd with ListProjectCmd, ShowProjectCmd, CloneProjectCmd
structs. Wire via register.go. User-facing grammar changes from
'ch project list|show|clone' to 'ch list|show|clone project' with
plural/short aliases (projects, pr). Behavior identical.
EOF
)"
```

---

### Task 14: Refactor `cmd/worktree.go` into struct commands

**Why:** Second subject conversion. Replace the old worktree cobra vars with `ListWorktreeCmd`, `CreateWorktreeCmd`, `DeleteWorktreeCmd` structs. Drop `worktree shell` (replaced by `ch create session --shell` in Task 15) and drop `worktree template` (redundant with `ch template`).

**Files:**
- Modify: `cmd/worktree.go`
- Modify: `cmd/register.go`
- Modify: `cmd/worktree_test.go`

**Command shape and body-porting map:**

| New struct | Port body from | Current file lines (approx) |
|---|---|---|
| `ListWorktreeCmd` | `worktreeListCmd.RunE` | `cmd/worktree.go` 40–64 |
| `CreateWorktreeCmd` | `worktreeNewCmd.RunE` | `cmd/worktree.go` 79–168 |
| `DeleteWorktreeCmd` | `worktreeDeleteCmd.RunE` | `cmd/worktree.go` 179–206 |

Aliases on all three: `"worktrees"`, `"wt"`.

Flag migration (package-level var → struct field):
- `worktreeNewFrom` → `CreateWorktreeCmd.From`
- `worktreeNewAttach` → `CreateWorktreeCmd.Attach`
- `worktreeNewAgent` → `CreateWorktreeCmd.Agent`
- `worktreeForce` → `DeleteWorktreeCmd.Force`

- [ ] **Step 1: Rewrite `cmd/worktree.go`**

Model closely on the project.go conversion. Port the existing `Run` bodies from the line ranges above — the logic doesn't change, only the outer structure.

Drop from the old file:
- `worktreeShellCmd` and its `init` wiring
- `worktreeTemplateCmd` and its `init` wiring
- The package-level flag vars (`worktreeNewFrom`, `worktreeNewAttach`, `worktreeNewAgent`, `worktreeForce`, `worktreeTemplateDryRun`) — moved to struct fields
- The file's `init()`

Keep in `cmd/errors.go` (already migrated in Task 11): `worktreeErr`.

- [ ] **Step 2: Update `cmd/worktree_test.go`**

Rewrite assertions to use the new command grammar:
- `ch worktree list` → `ch list worktree`
- `ch worktree new <p> <b>` → `ch create worktree <p> <b>`
- `ch worktree delete <p> <b>` → `ch delete worktree <p> <b>`
- Delete any `ch worktree shell` / `ch worktree template` tests.

- [ ] **Step 3: Register structs in `cmd/register.go`**

```go
listCmd.AddCommand((&ListWorktreeCmd{}).Cobra())
createCmd.AddCommand((&CreateWorktreeCmd{}).Cobra())
deleteCmd.AddCommand((&DeleteWorktreeCmd{}).Cobra())
```

- [ ] **Step 4: `make check`**

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): convert worktree subject to struct commands

Replace package-level worktree cobra vars with ListWorktreeCmd,
CreateWorktreeCmd, DeleteWorktreeCmd structs. Drop 'ch worktree shell'
(superseded by 'ch create session --shell' in the next commit) and
'ch worktree template' (redundant with 'ch template'). Package-level
flag globals move to struct fields. Aliases: worktrees, wt.
EOF
)"
```

---

### Task 15: Refactor `cmd/session.go` into struct commands with unified agent/shell

**Why:** Third subject conversion and the one that lights up the unified session model for users. Replace the old session cobra vars with `ListSessionCmd`, `ShowSessionCmd`, `CreateSessionCmd`, `DeleteSessionCmd`, `AttachSessionCmd`. Every session verb except `list` addresses sessions by `<project> <branch> [--shell]`. Drop `--no-create`.

**Files:**
- Modify: `cmd/session.go`
- Modify: `cmd/register.go`
- Modify: `cmd/session_test.go`
- Modify: `cmd/session_internal_test.go`

**Command shape and body-porting map:**

| New struct | Port body from | Current file lines (approx) |
|---|---|---|
| `ListSessionCmd` | `sessionListCmd.RunE` | `cmd/session.go` 160–172 |
| `ShowSessionCmd` | `sessionShowCmd.RunE` | `cmd/session.go` 181–197 |
| `CreateSessionCmd` | `sessionStartCmd.RunE` | `cmd/session.go` 57–152 |
| `DeleteSessionCmd` | `sessionStopCmd.RunE` | `cmd/session.go` 249–276 |
| `AttachSessionCmd` | `sessionAttachCmd.RunE` | `cmd/session.go` 206–213 |

Aliases on all five: `"sessions"`, `"ses"`.

Flag migration (package-level var → struct field):
- `sessionStartAttach` → `CreateSessionCmd.Attach`
- `sessionStartAgent` → `CreateSessionCmd.Agent`
- `sessionStartNoCreate` → **removed** (spec: auto-create is unconditional)
- `sessionStopForce` → `DeleteSessionCmd.Force`

New fields (didn't exist before):
- `CreateSessionCmd.Shell` (bool) — bound to `--shell`
- `ShowSessionCmd.Shell`, `DeleteSessionCmd.Shell`, `AttachSessionCmd.Shell` — same

| Struct | Usage | Flags |
|---|---|---|
| `ListSessionCmd` | `list session` (aliases `sessions`, `ses`) | none |
| `ShowSessionCmd` | `show session <p> <b>` | `--shell` |
| `CreateSessionCmd` | `create session <p> <b>` | `--shell`, `--attach`, `--agent` |
| `DeleteSessionCmd` | `delete session <p> <b>` | `--shell`, `--force` |
| `AttachSessionCmd` | `attach session <p> <b>` | `--shell` |

`list session` prints a table with columns SESSION, TYPE, STATUS (plus optional ANNOTATION and STARTED when populated). The TYPE column disambiguates rows with identical canonical names.

`CreateSessionCmd` unconditionally auto-creates the worktree if it doesn't exist (same behavior as today's `session start` without `--no-create`). There is no opt-out flag.

- [ ] **Step 1: Rewrite `cmd/session.go`**

Port the existing `Run` bodies, adapted to:
- Accept `<project> <branch>` as two positional args (not `<canonical-name>`).
- Read `--shell` into the struct field and pass `semconv.SessionTypeShell` (or agent) to `session.Service.Show`/`Stop`/`Start`.
- For `CreateSessionCmd`:
  - If `--shell`, `Cmd` = `os.Getenv("SHELL")` with `/bin/sh` fallback. No `Env` map.
  - Otherwise, resolve the agent via `cfg.AgentByName(...)` as today and use `agent.Command()` + `agent.Env`.
- For `ListSessionCmd`: print a TYPE column.
- For `AttachSessionCmd`: compose the session ID from `svc.Show(project, branch, sessionType)` and `execTmuxAttach(info.SessionID)`.

Drop:
- `sessionStartNoCreate` flag and its plumbing
- All `sessionXxxCmd` package-level vars and their `init()`

- [ ] **Step 2: Rewrite `cmd/session_test.go`, `cmd/session_internal_test.go`, and `cmd/session_integration_test.go`**

All three files carry stale grammar. Update assertions:
- Every `"session start"` → `"create session"`; two args instead of one-canonical-name
- Every `"session stop"` → `"delete session"`; two args
- Every `"session show"` → `"show session"`; two args
- Every `"session attach"` → `"attach session"`; two args
- Every `"session list"` → `"list session"`
- Delete `TestSessionStart_noCreateFlag_recognized` (or similar `--no-create` coverage)
- In `cmd/session_integration_test.go`: **delete** `TestSessionStart_noCreate_failsWhenWorktreeMissing` and `TestSessionStart_noCreate_subprocess` (lines ~76–107). The `--no-create` flag is gone; there's nothing to test. Update `TestSessionStart_autoCreate_createsWorktreeAndStartsSession` to use the new grammar (`create session`).
- Add new coverage: `TestCreateSession_Shell` creating a shell-type session; verify the stored `SessionType` option.

- [ ] **Step 3: Register structs in `cmd/register.go`**

```go
listCmd.AddCommand((&ListSessionCmd{}).Cobra())
showCmd.AddCommand((&ShowSessionCmd{}).Cobra())
createCmd.AddCommand((&CreateSessionCmd{}).Cobra())
deleteCmd.AddCommand((&DeleteSessionCmd{}).Cobra())
attachCmd.AddCommand((&AttachSessionCmd{}).Cobra())
```

- [ ] **Step 4: `make check`**

Expected: green. Manual sanity: `ch list session` shows a TYPE column. `ch create session app main` starts an agent. `ch create session app main --shell` starts a shell. Both coexist.

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): convert session subject to struct commands with unified types

Replace package-level session cobra vars with ListSessionCmd,
ShowSessionCmd, CreateSessionCmd, DeleteSessionCmd, AttachSessionCmd.
Every session verb except list addresses sessions by
<project> <branch> [--shell]. --shell selects shell-type sessions
(command is $SHELL, fallback /bin/sh). --no-create is dropped;
worktree auto-creation in 'create session' is unconditional. List
gains a TYPE column to disambiguate same-canonical-name rows.
Aliases: sessions, ses.
EOF
)"
```

---

### Task 16: Convert `cmd/template.go` into `TemplateCmd` struct

**Why:** The last subject file. `template` is a verb-only root command; grammar-wise it's an exception to `<verb> <subject>`, and that's fine — one exception is cheaper than forcing a subject. Convert to struct form for consistency with the rest.

**Files:**
- Modify: `cmd/template.go`
- Modify: `cmd/register.go`
- Modify: `cmd/template_test.go`

- [ ] **Step 1: Rewrite `cmd/template.go`**

```go
type TemplateCmd struct {
    Project string
    Branch  string
    DryRun  bool
}

func (c *TemplateCmd) Cobra() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "template [dir]",
        Short: "Process .herd template files in a directory",
        Long:  `... (carry over existing Long text) ...`,
        Args:  cobra.MaximumNArgs(1),
        RunE:  c.Run,
    }
    cmd.Flags().StringVar(&c.Project, "project", "", "project name (auto-detected from directory)")
    cmd.Flags().StringVar(&c.Branch, "branch", "", "branch name (auto-detected from git)")
    cmd.Flags().BoolVar(&c.DryRun, "dry-run", false, "print rendered output without writing")
    return cmd
}

func (c *TemplateCmd) Run(cmd *cobra.Command, args []string) error {
    // Port existing body verbatim. Reads cfg, calls herdtemplate.New(h).Process(...).
}
```

Drop the package-level `templateProject`, `templateBranch`, `templateDryRun` vars and the file's `init()`. Also drop the helper functions `resolveProjectFromDir` and `detectGitBranch` — move them into `cmd/template.go` as unexported package-level functions (they're already there; just keep them).

- [ ] **Step 2: Register in `cmd/register.go`**

```go
root.AddCommand((&TemplateCmd{}).Cobra())
```

Place this line after the verb-grouper `AddCommand` call.

- [ ] **Step 3: Update `cmd/template_test.go`**

The command path is unchanged (`ch template [dir]`) so most tests should pass as-is. Verify — any test that instantiates the command struct directly needs the new form.

- [ ] **Step 4: `make check`**

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "$(cat <<'EOF'
refactor(cmd): convert template to TemplateCmd struct

Final subject conversion. TemplateCmd owns its flag fields; package-
level templateProject/templateBranch/templateDryRun vars and the old
init() are removed. User-facing command unchanged ('ch template [dir]').
EOF
)"
```

---

### Task 17: Remove dead group declarations and transitional state from `cmd/root.go`

**Why:** After Tasks 13–16, all old `<subject> <verb>` wiring is gone. The `AddGroup` calls for `sessions` and `projects` no longer have any commands assigned to them — those groups showed up in `ch --help` but now point at nothing.

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Delete the `AddGroup` call**

Remove the entire `rootCmd.AddGroup(...)` block. With the verb tree the help output is already organized by verb.

- [ ] **Step 2: Verify `ch --help` output is clean**

```bash
go build -o ch . && ./ch --help
```

Expected: six verb groupers (list/create/delete/show/clone/attach), `template`, `plugin` (hidden), and the root-level launches-TUI behavior. No empty group titles.

- [ ] **Step 3: `make check`**

- [ ] **Step 4: Commit**

```bash
git add cmd/root.go
git commit -m "$(cat <<'EOF'
chore(cmd): drop empty AddGroup declarations from root

With all subjects now wired under verb groupers in register.go, the
old AddGroup("sessions") / AddGroup("projects") declarations no longer
attach anything — 'ch --help' was showing empty group titles. Remove
them. The verb tree is now the sole top-level organization.
EOF
)"
```

---

## Chunk 4: Docs and Final Verification

Sweep the documentation in the repo to match the new CLI grammar and shrunken architecture. Then a final `make check` to confirm end-to-end green.

### Task 18: Update `README.md`

**Files:** `README.md`

- [ ] **Step 1: Scan the README for command references**

```bash
grep -n 'ch session\|ch worktree\|ch project\|ch config\|ch up\|ch down\|ch status\|ch ssh' README.md
```

- [ ] **Step 2: Rewrite every command reference to the new grammar**

Examples:
- `ch session start foo bar` → `ch create session foo bar`
- `ch session stop foo-bar` → `ch delete session foo bar`
- `ch worktree new foo bar` → `ch create worktree foo bar`
- `ch worktree delete foo bar` → `ch delete worktree foo bar`
- `ch project list` → `ch list project`
- `ch project clone foo` → `ch clone project foo`
- `ch worktree shell foo bar` → `ch create session foo bar --shell` (note: was never in the README, but if referenced, replace)
- Any `ch up`, `ch down`, `ch status`, `ch ssh`, `ch config *` references → remove outright, not rewrite.

- [ ] **Step 3: Remove any "remote droplet" / DigitalOcean copy**

The README may describe the planned remote phase. Delete those paragraphs.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: update README for new CLI grammar

Rewrite command references from <subject> <verb> to <verb> <subject>.
Remove references to ch up/down/status/ssh and ch config — those
commands no longer exist.
EOF
)"
```

---

### Task 19: Update `CLAUDE.md`

**Files:** `CLAUDE.md`

- [ ] **Step 1: Review the "Package layout" section**

Remove bullets for `internal/do`, `internal/provision`, `internal/state`. Update the `internal/config` description to reflect the shrunken surface (no `ApplyEnv`/`ApplyFlags`/`SetKey`/`Profiles`/`Validate`). Update the `internal/session` description to note unified agent/shell types.

- [ ] **Step 2: Review the "Command groups" section**

Rewrite to describe the new verb-first taxonomy (list/create/delete/show/clone/attach + template + plugin) instead of the old subject-first grouping.

- [ ] **Step 3: Review the "Config and state paths" section**

Remove the "Droplet state" row — `~/.local/share/codeherd/state.json` is gone.

- [ ] **Step 4: Review "Key design patterns"**

Remove the `DropletsService` interface reference (internal/do is deleted). Remove the `ApplyEnv`/`ApplyFlags` mention.

- [ ] **Step 5: Review "What's implemented" and "What's planned"**

"What's implemented" should now list the new verb commands. "What's planned" should explicitly call out: future config rebuild, future session persistence, future remote-execution design.

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: update CLAUDE.md for new architecture

Rewrite the Package layout, Command groups, and Key design patterns
sections to reflect the removed packages (internal/do, provision,
state), the shrunken internal/config, the unified session type
handling, and the new verb-first CLI taxonomy.
EOF
)"
```

---

### Task 20: Update `docs/project.md` and `plugins/claude/README.md`

**Files:**
- `docs/project.md`
- `plugins/claude/README.md`

- [ ] **Step 1: `docs/project.md`**

Remove forward-looking copy about the remote-droplet phase. Rewrite command references to the new grammar.

- [ ] **Step 2: `plugins/claude/README.md`**

Update any `ch` command reference (likely `ch plugin handle-claude` which is unchanged, but verify the surrounding copy references the old grammar).

- [ ] **Step 3: Commit**

```bash
git add docs/project.md plugins/claude/README.md
git commit -m "$(cat <<'EOF'
docs: update project.md and claude plugin README

Rewrite command references to the new <verb> <subject> grammar and
remove forward-looking copy about the deleted remote-droplet phase.
EOF
)"
```

---

### Task 21: Final verification and index update

**Files:** none (verification only)

- [ ] **Step 1: Run the full check**

```bash
make check
```

Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds.

- [ ] **Step 2: Manual smoke test of user-facing commands**

```bash
./ch --help
./ch list --help
./ch create --help
./ch list project
./ch list worktree
./ch list session
./ch template --help
```

Expected: each invocation prints sensible output or an appropriate error. No stack traces, no references to removed commands.

- [ ] **Step 3: Grep for any remaining dead references**

```bash
grep -rn 'internal/do\|internal/provision\|internal/state\|ApplyEnv\|ApplyFlags\|ProfileConfig\|--no-create\|--token\|sessionStartNoCreate\|noColor\|configProfile\|worktree new\|worktree shell\|worktree template\|session start\|session stop\|session attach\|session show\|ch up\|ch down\|ch status\|ch ssh\|ch config\|defaultImage' --include='*.go' --include='*.md' .
```

Expected: the only hits are this plan file and the design doc. If anything else matches, clean it up in a small fix-up commit.

- [ ] **Step 4: Final commit (if step 3 found anything)**

```bash
git add -A
git commit -m "$(cat <<'EOF'
chore: sweep remaining stale references

Post-refactor sweep for any leftover references to removed symbols,
packages, flags, or command grammar.
EOF
)"
```

- [ ] **Step 5: Summary**

The branch `feat/command-verbs` now contains the full refactor. Ready for review / merge per the user's preferred flow.

---

## Notes for the Implementer

- **Subject ordering inside `register.go`:** prefer consistent order under each verb (project, worktree, session) for readable help output.
- **Aliases are set per leaf command**, not on the verb grouper. `ch list worktrees` works because `ListWorktreeCmd.Cobra()` sets `Aliases: []string{"worktrees", "wt"}`.
- **Test harness for CLI commands:** follow the existing pattern in `cmd/*_test.go` — construct a bytes.Buffer, set `rootCmd.SetOut`/`SetErr`, call `rootCmd.SetArgs([]string{"list", "worktree"})`, then `rootCmd.Execute()`. The `resetAllFlags` helper in `cmd/root.go` makes it safe to re-run.
- **When a task description says "port existing body verbatim":** that's literal. Copy the current `Run` body line-for-line; the only edits are reading from struct fields instead of package globals and calling the renamed service methods. Don't "improve" the body.
- **If `make check` fails on coverage:** the most likely cause is a rewritten test file losing cases. Audit the test file against the pre-refactor version before adding new cases — the existing cases usually transfer unchanged.
- **If you see a "SessionTypeShell is never used" lint warning after Chunk 2:** verify the TUI action rewrite actually landed. That constant should be referenced by `internal/tui/actions.go` and by `cmd/session.go`'s CreateSessionCmd.
