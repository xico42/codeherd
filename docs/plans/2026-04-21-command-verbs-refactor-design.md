# Command Verbs Refactor Design

Reshape the CLI surface from `<subject> <verb>` to `<verb> <subject>`, delete the planned-but-unimplemented remote phase and its config scaffolding, unify agent and shell sessions under one concept, and refactor each command into a self-contained struct.

## Context

The CLI today mixes three problems:

1. **Dead commands.** `ch up`, `ch down`, `ch status`, `ch ssh` are stubs that print `not implemented`. They were placeholders for a planned Digital Ocean remote-execution phase that never materialized.
2. **Dead config surface.** The entire `config` subtree (`init`, `show`, `get`, `set`, `profile *`) plus env overlays (`DIGITALOCEAN_TOKEN`, `CODEHERD_REGION`, etc.) and the root `--token` flag exist almost exclusively to configure the remote phase. Of the nine `DefaultsConfig` fields, only `ProjectsDir` and `Agent` are read by implemented commands. `ProfileConfig` is consumed only by the `up` stub.
3. **Inconsistent session model.** `semconv` defines `SessionTypeAgent` and `SessionTypeShell`, but the CLI only creates agent sessions while the TUI creates shell sessions by bypassing `session.Service` and writing tmux directly. `session.Service.List()` and `Show()` filter out shell sessions entirely. `ch worktree shell` is a third, unrelated path that just `syscall.Exec`s a shell with no session tracking at all.
4. **Package-level command state.** Every cobra command is a package-level `var` with inline `RunE` closures and package-level flag variables. There is no encapsulation; every file adds globals to the `cmd` package.

This refactor addresses all four at once.

## Decisions

- **Delete** `cmd/up.go`, `cmd/down.go`, `cmd/status.go`, `cmd/ssh.go` — hard cut, no deprecation aliases.
- **Delete the entire `config` CLI subtree** — `cmd/config.go`, `cmd/config_test.go`. No deprecation aliases. Users edit `~/.config/codeherd/config.toml` by hand until a proper config design lands.
- **Delete** `internal/do/`, `internal/provision/`, `internal/state/` — all remote-phase packages.
- **Shrink `internal/config`** — remove DO-related fields, `ProfileConfig`, `ApplyEnv`, `ApplyFlags`, `SetKey`, `DeleteSection`, `IsValidKeyPath`, `Redact`, `Validate`, and the `defaultImage` constant plus its seeding in `Load`. Keep only `Load`, `Save`, `Path`, `RepoPath`, `AgentByName`, `AgentNames`, tilde expansion, projects-dir default.
- **Remove** root `--token` flag and `PersistentPreRunE`'s `cfg.ApplyEnv()` / `cfg.ApplyFlags()` calls.
- **Remove** the dead root `--no-color` flag (declared but never read).
- **Drop `--no-create` from `create session`** — auto-creation of the worktree becomes unconditional. The flag's only job was to opt out of that behavior, which served the "I don't want codeherd creating state I didn't ask for" case; with the verb reshape there is now an explicit alternative (`ch create worktree <p> <b>` then `ch create session <p> <b>`).
- **Reshape CLI surface** from `<subject> <verb>` to `<verb> <subject>`.
- **Singular canonical subjects with plural + short aliases** (kubectl-style). `ch list worktree`, `ch list worktrees`, `ch list wt` all work; docs use singular.
- **Unify agent and shell sessions under one concept** (Branch 1). `session.Service.Start` takes a `Type` field. `List`/`Show` stop filtering by type. The TUI's direct-tmux shell path is removed; it calls `session.Service` instead.
- **Drop `ch worktree shell`** — replaced by `ch create session <proj> <br> --shell`.
- **Drop `ch worktree template`** — redundant with `ch template [dir]`, which already auto-detects project and branch from the directory.
- **Each command becomes a struct** with flag fields and a `Cobra()` method. No package-level flag vars. `cfg` stays package-level (no constructor DI).
- **Verb groupers** are bare `*cobra.Command` with `<verb>Cmd` Go identifiers (`deleteCmd`, not `delete` — `delete` shadows the built-in).
- **Session addressing is `<project> <branch> [--shell]`** for every session verb. No opaque tmux names in the CLI.
- **Rename `session.Service.Start` remains `Start`** internally, even though the CLI verb is `create`. The CLI verb is a user-facing choice; the service keeps its running-state vocabulary.

## Command Surface

### Before → after

| Before | After |
|---|---|
| `ch project list` | `ch list project` |
| `ch project show <name>` | `ch show project <name>` |
| `ch project clone [<name>]` | `ch clone project [<name>]` |
| `ch worktree list [project]` | `ch list worktree [project]` |
| `ch worktree new <p> <b>` | `ch create worktree <p> <b>` |
| `ch worktree delete <p> <b>` | `ch delete worktree <p> <b>` |
| `ch worktree shell <p> <b>` | (removed — use `ch create session <p> <b> --shell`) |
| `ch worktree template <p> <b>` | (removed — use `ch template <dir>`) |
| `ch session start <p> <b>` | `ch create session <p> <b>` |
| `ch session stop <name>` | `ch delete session <p> <b> [--shell]` |
| `ch session list` | `ch list session` |
| `ch session show <name>` | `ch show session <p> <b> [--shell]` |
| `ch session attach <name>` | `ch attach session <p> <b> [--shell]` |
| `ch template [dir]` | unchanged |
| `ch plugin handle-claude` | unchanged (hidden) |
| `ch` (root) | unchanged (launches TUI) |
| `ch up` / `ch down` / `ch status` / `ch ssh` | (removed) |
| `ch config *` (all) | (removed) |

### Aliases

Every leaf subject gets cobra `Aliases` covering plural and a short form:

| Subject | Aliases |
|---|---|
| `project` | `projects`, `pr` |
| `worktree` | `worktrees`, `wt` |
| `session` | `sessions`, `ses` |

### Flags

| Command | Flags |
|---|---|
| `clone project` | `--all` |
| `create worktree` | `--from`, `--attach`, `--agent` |
| `delete worktree` | `--force` |
| `create session` | `--shell`, `--attach`, `--agent` (no `--no-create`; worktree is always auto-created if missing) |
| `delete session` | `--shell`, `--force` |
| `show session` | `--shell` |
| `attach session` | `--shell` |
| `template` | `--project`, `--branch`, `--dry-run` |

## Architecture

### Struct pattern

Each command is a struct whose exported fields hold its flag values. A `Cobra()` method builds and returns the `*cobra.Command`, binding flags to the struct fields. `Run` is a method on the struct.

```go
type CreateWorktreeCmd struct {
    From   string
    Attach bool
    Agent  string
}

func (c *CreateWorktreeCmd) Cobra() *cobra.Command {
    cmd := &cobra.Command{
        Use:     "worktree <project> <branch>",
        Aliases: []string{"worktrees", "wt"},
        Short:   "Create a new worktree for a project",
        Args:    cobra.ExactArgs(2),
        RunE:    c.Run,
    }
    cmd.Flags().StringVar(&c.From, "from", "", "base branch")
    cmd.Flags().BoolVar(&c.Attach, "attach", false, "start a session after creation")
    cmd.Flags().StringVar(&c.Agent, "agent", "", "agent to use with --attach")
    return cmd
}

func (c *CreateWorktreeCmd) Run(cmd *cobra.Command, args []string) error {
    // Reads package-level cfg, constructs services via cmd/services.go helpers.
}
```

### File layout

```
cmd/
├── root.go              rootCmd, Execute(), PersistentPreRunE (load cfg)
├── register.go          verb groupers + wires all struct commands
├── services.go          newWorktreeService, newSessionService, newProjectService
├── errors.go            worktreeErr, sessionErr (shared error printers)
├── tui.go               runTUI, runTUIInTmux, runTUIDirect
├── project.go           ListProjectCmd, ShowProjectCmd, CloneProjectCmd
├── worktree.go          ListWorktreeCmd, CreateWorktreeCmd, DeleteWorktreeCmd
├── session.go           List/Show/Create/Delete/AttachSessionCmd
├── template.go          TemplateCmd
├── plugin.go            pluginCmd + pluginHandleClaudeCmd (unchanged)
└── *_test.go            one test file per subject
```

One file per subject. Each subject file contains every struct that operates on that subject. No `init()` in subject files — wiring lives in `register.go`.

### register.go

```go
func registerCommands(root *cobra.Command) {
    listCmd   := &cobra.Command{Use: "list",   Short: "List resources"}
    createCmd := &cobra.Command{Use: "create", Short: "Create resources"}
    deleteCmd := &cobra.Command{Use: "delete", Short: "Delete resources"}
    showCmd   := &cobra.Command{Use: "show",   Short: "Show details for a resource"}
    cloneCmd  := &cobra.Command{Use: "clone",  Short: "Clone a project from remote"}
    attachCmd := &cobra.Command{Use: "attach", Short: "Attach to a running session"}

    listCmd.AddCommand(
        (&ListProjectCmd{}).Cobra(),
        (&ListWorktreeCmd{}).Cobra(),
        (&ListSessionCmd{}).Cobra(),
    )
    createCmd.AddCommand(
        (&CreateWorktreeCmd{}).Cobra(),
        (&CreateSessionCmd{}).Cobra(),
    )
    deleteCmd.AddCommand(
        (&DeleteWorktreeCmd{}).Cobra(),
        (&DeleteSessionCmd{}).Cobra(),
    )
    showCmd.AddCommand(
        (&ShowProjectCmd{}).Cobra(),
        (&ShowSessionCmd{}).Cobra(),
    )
    cloneCmd.AddCommand((&CloneProjectCmd{}).Cobra())
    attachCmd.AddCommand((&AttachSessionCmd{}).Cobra())

    root.AddCommand(listCmd, createCmd, deleteCmd, showCmd, cloneCmd, attachCmd)
    root.AddCommand((&TemplateCmd{}).Cobra())
    root.AddCommand(pluginCmd)
}
```

`registerCommands` is called once from `cmd/root.go`'s `init()`.

### Help output

Cobra's auto-generated help now reads by verb at the top level:

```
Usage:
  ch [command]

Available Commands:
  list     List resources
  create   Create resources
  delete   Delete resources
  show     Show details for a resource
  clone    Clone a project from remote
  attach   Attach to a running session
  template Process .herd template files in a directory
  help     Help about any command
```

Each verb shows its subjects:

```
$ ch list --help
Usage:
  ch list [command]

Available Commands:
  project   List all configured projects
  worktree  List worktrees (all projects, or a single project)
  session   List all active sessions
```

Cobra's existing groups (`sessions`, `projects`, `config`, `remote`) go away — the verb tree replaces the need for grouping at the root.

## Package Changes

### `internal/config`

Keep:
- `Config{Defaults, Projects, Agents, path}`
- `DefaultsConfig{ProjectsDir, Agent}` — only these two fields
- `ProjectConfig`, `AgentConfig` (unchanged)
- `Load()`, `Save()`, `Path()`
- `RepoPath()`, `AgentByName()`, `AgentNames()`
- Tilde expansion and projects-dir default

Remove:
- `DefaultsConfig.Token`, `.SSHKeyID`, `.Region`, `.Size`, `.Image`, `.TailscaleAuthKey`, `.GitIdentityFile`
- `ProfileConfig` struct, `Profiles` map, `Profile()` method
- `ApplyEnv()`, `ApplyFlags()`
- `SetKey()`, `DeleteSection()`, `IsValidKeyPath()`
- `Redact()`, `Validate()`
- The `"github.com/go-playground/validator/v10"` dependency if it becomes unused

### `internal/session`

`StartRequest` gains a `Type` field:

```go
type StartRequest struct {
    Project string
    Branch  string
    Path    string
    Type    string // semconv.SessionTypeAgent | SessionTypeShell
    Cmd     string
    Env     map[string]string
    Attach  bool
}
```

Full post-refactor `Service` method matrix:

| Method | Signature | Type filter | Notes |
|---|---|---|---|
| `Start` | `Start(req StartRequest) (sessionID string, err error)` | Scopes `ErrSessionExists` check by `(CanonicalName, Type)` | Empty `req.Type` defaults to `SessionTypeAgent`. Shell type uses `semconv.ShellSessionName(p, b)` as tmux name (`<p>-<b>~sh`); agent type uses `semconv.SessionName(p, b)` (`<p>-<b>`). Canonical name is `<p>-<b>` for both types. |
| `List` | `List() ([]SessionInfo, error)` | None | Returns sessions of all types. `SessionInfo.Type` is populated; callers filter. Two entries with identical `Name` but different `Type` are possible. |
| `Show` | `Show(project, branch, sessionType string) (*SessionInfo, error)` | Matches on `(CanonicalName == <p>-<b>) && (SessionType == sessionType)` | Returns `ErrSessionNotFound` if no session of that type exists. |
| `Stop` | `Stop(project, branch, sessionType string) error` | Same as `Show` | Kills the tmux session. No persisted state to update. |
| `SetStatus` | unchanged: `SetStatus(name, status, annotation string) error` | Keeps the `SessionType == SessionTypeAgent` filter | `plugin handle-claude` only has the canonical session name from `CODEHERD_SESSION`; status transitions are an agent-lifecycle concept and don't apply to shell sessions. |

`Start`'s existing-session check must type-scope: agent and shell sessions share a canonical name, so an unscoped check would spuriously fail when creating a shell session for a branch that already has an agent session.

`SessionInfo` gains a `Type` field. `ListSessionCmd`'s table output adds a TYPE column (`agent` | `shell`) so users can disambiguate two same-canonical-name rows.

Existing callers migrate:

- **CLI** — `cmd/session.go` structs build a `StartRequest` with `Type` from the `--shell` flag and call `Show`/`Stop` with `(project, branch, sessionType)`.
- **CLI `--attach`** — `CreateWorktreeCmd` with `--attach` builds an agent `StartRequest` (unchanged semantics).
- **TUI** — see below.
- **Hooks** — `plugin handle-claude` calls `SetStatus` only; signature unchanged.

### `internal/tui`

`actions.go` currently constructs shell tmux sessions directly:

```go
tmuxClient.NewSession(shellName, path)
tmuxClient.SetOption(shellName, semconv.TmuxOptionCanonicalName, sessionName)
tmuxClient.SetOption(shellName, semconv.TmuxOptionSessionType, semconv.SessionTypeShell)
```

This block is replaced by a `session.Service.Start` call with `Type: SessionTypeShell`. The `Cmd` is the user's `$SHELL` (with fallback to `/bin/sh`), matching the current `worktree shell` behavior. The TUI call site has `project`, `branch`, and `path` in scope already, so there is no new context wiring required.

The TUI no longer has special-case code for shell sessions; every tmux-backed session goes through `session.Service`.

`model.go` already inspects `SessionType` to render differently for agent vs shell; that logic is unchanged.

### `internal/worktree`

`worktree.Service.Delete` currently checks and optionally kills the agent session (`semconv.SessionName(p, b)`) when `--force` is used. Update it to check and kill **both** session types — agent (`<p>-<b>`) and shell (`<p>-<b>~sh`) — so that `ch delete worktree <p> <b> --force` leaves no dangling sessions attached to the removed path.

### Error messages

User-facing error strings in `cmd/errors.go` (migrated from `cmd/worktree.go` and `cmd/session.go`) reference commands by the new grammar. Today's strings:

- `"Run 'ch project clone %s' first"` → `"Run 'ch clone project %s' first"`
- `"Run 'ch worktree new %s %s' first"` → `"Run 'ch create worktree %s %s' first"`
- `"Attach with 'ch session attach %s'"` → `"Attach with 'ch attach session %s %s'"` (now takes project + branch)
- `"Stop it first or use --force"` — unchanged text but context updated for unified session model

### `cmd/root.go`

Simplifications:
- Drop `token` variable and `--token` flag.
- Drop `noColor` variable and `--no-color` flag (declared but never read).
- Drop `rootCmd.AddGroup` calls — no more verb grouping by subject.
- `PersistentPreRunE` shrinks: no `cfg.ApplyEnv()` and no `cfg.ApplyFlags()`.
- Call `registerCommands(rootCmd)` from `init()`.
- `runTUI`, `runTUIInTmux`, `runTUIDirect`, `createCodeherdSession`, `respawnIfDead` move to `cmd/tui.go` for file-length hygiene.

### `cmd/services.go`

Hosts the per-call service constructor helpers (`newWorktreeService`, `newSessionService`, `newProjectService`). Each reads the package-level `cfg` directly — same pattern as today, just consolidated in one file. No DI, no parameters.

### Struct naming

Final list of command structs (verb-subject-Cmd form, no plural):

- `cmd/project.go` — `ListProjectCmd`, `ShowProjectCmd`, `CloneProjectCmd`
- `cmd/worktree.go` — `ListWorktreeCmd`, `CreateWorktreeCmd`, `DeleteWorktreeCmd`
- `cmd/session.go` — `ListSessionCmd`, `ShowSessionCmd`, `CreateSessionCmd`, `DeleteSessionCmd`, `AttachSessionCmd`
- `cmd/template.go` — `TemplateCmd`

### `main.go`

Unchanged.

## Session Addressing

Every session verb addresses a session by `<project> <branch>` plus an optional `--shell` type flag.

```
ch create session <project> <branch> [--shell] [--attach] [--agent]
ch delete session <project> <branch> [--shell] [--force]
ch show   session <project> <branch> [--shell]
ch attach session <project> <branch> [--shell]
ch list   session                               # shows all types; add --shell or --agent later if needed
```

Internal tmux name details (`<p>-<b>`, `<p>-<b>~sh`) do not leak into the CLI. The canonical name (`<p>-<b>`) remains the display name and is what `list`/`show` print.

`stop session` is folded into `delete session` — there is no persisted state, so the two are synonyms under the current model. A future design can split them if session persistence is introduced.

## Test Migration

Test files with stale expectations need updating alongside the source. No new test design required — they are mechanical rewrites against the new API and command grammar.

- **`cmd/config_test.go`** — delete entirely.
- **`cmd/root_test.go`** — remove `up`, `down`, `status`, `ssh`, `config` from the subcommand iteration in `TestExecute_Subcommands`.
- **`cmd/session_test.go`** — rewrite assertions for the new grammar (`create session` / `delete session` / `attach session` / `show session`) and the new addressing scheme (`<project> <branch>` + `--shell`). Remove the `--no-create` coverage (`TestSessionStart_noCreateFlag_recognized` and friends).
- **`cmd/worktree_test.go`** — rewrite for `create worktree` / `delete worktree` / `list worktree`. Remove `worktree shell` and `worktree template` test cases.
- **`cmd/template_test.go`** — unchanged.
- **`cmd/plugin_*_test.go`** — unchanged; `plugin handle-claude` is unchanged.
- **`internal/config/config_test.go`** — remove `TestConfig_ApplyEnv`, `TestConfig_ApplyFlags`, `TestConfig_Validate`, `TestConfig_Redact`, `TestIsValidKeyPath`, `TestSetKey_*`, `TestDeleteSection_*`, `TestConfig_Profile`, `TestConfig_SetKey_CreatesNewProfile`, and any field-specific assertions on `Token`, `SSHKeyID`, `Region`, `Size`, `Image`, `TailscaleAuthKey`, `GitIdentityFile`.
- **`internal/config/errors_test.go`** — delete entirely (covers only `SetKey` / `DeleteSection`).
- **`internal/session/session_test.go`** — update for the new `Start` / `List` / `Show` / `Stop` signatures and the type-scoped existence check. Add coverage for shell-type sessions coexisting with agent sessions under the same canonical name.
- **`internal/tui/actions_test.go`** — rewrite `TestShellAction_*` against a mocked `session.Service.Start` call instead of direct tmux mock assertions.

## Docs

README and `CLAUDE.md` are updated in the same refactor to avoid leaving the repo's public face inconsistent with the code.

- **`README.md`** — command examples in the "Usage" / "Commands" sections rewritten to the new grammar. Remove any references to `ch up`/`down`/`status`/`ssh`/`config`.
- **`plugins/claude/README.md`** — command references updated.
- **`docs/project.md`** — remove forward-looking remote-phase copy and the associated config references.
- **`CLAUDE.md`** — the "Package layout" and "Key design patterns" sections are rewritten to reflect the actual surviving architecture (no `internal/do`, `internal/provision`, `internal/state`; no `ApplyEnv`/`ApplyFlags`; no `config profile`; session package handles both agent and shell types).

## Migration

Hard cut, no deprecation aliases. Users running `ch session start foo bar` after this lands get cobra's "unknown command" error and must learn the new form. This project is a personal tool; the blast radius is one user.

## Out of Scope

- **Config rebuild.** A future design will decide whether codeherd needs a CLI-driven config at all, and if so, what shape. This refactor explicitly rips the existing one out rather than reshape it, so the next design starts from a clean slate.
- **Session persistence.** `stop` and `delete` are the same operation today because session state lives as tmux options and dies with the tmux session. Introducing a separate "stopped but preserved" state requires persisted metadata and crash-recovery logic — a separate design.
- **Remote execution phase.** Removed from this refactor. If reintroduced, it gets a fresh CLI design, not a revival of the `up`/`down`/`status`/`ssh` stubs.
- **Full dependency injection.** `cfg` stays package-level. Converting to constructor-injected deps is a separate mechanical refactor with its own cost-benefit analysis; not bundled here.
