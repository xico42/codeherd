# Dynamic shell completion for `ch`

**Issue:** #7 — Add better auto complete support
**Date:** 2026-05-24
**Status:** Approved

## Problem

Several `ch` commands take positional arguments and flag values drawn from a
static or runtime set of known values:

- `ch run <agent>` — agents are defined in config
- `--profile` / `-p` — profiles are discoverable at config time
- Any command taking `<project>` / `<branch>` positionals, or `--agent` /
  `--from` flags — these map to config-time or runtime state

Today the CLI offers no shell completion for any of these. Users must remember
and retype exact names.

## Goal

Provide dynamic shell completion that suggests the already-known valid values
for arguments and flags, easing CLI interaction.

## Non-goals

- Changing the behavior of any existing command. Completion only adds hints; it
  never alters what a command does when run.
- A custom completion installer. We rely on Cobra's built-in
  `ch completion <shell>` command and document its use.

## Approach

Central completion file (`cmd/completion.go`) holding reusable completion
functions, wired into each command's `Cobra()` method.

Alternatives rejected:

- **Inline closures per command** — duplicates the config-loading boilerplate
  across ~6 commands and cannot be unit-tested in isolation.
- **New `internal/completion` package** — completion needs `cmd`'s service
  constructors and package globals; a separate package would only call back into
  `cmd`, adding indirection for no gain.

## Architecture

### Completion data providers (`cmd/completion.go`)

Each provider has the Cobra completion signature
`func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)`:

| Function | Data source | Kind |
|---|---|---|
| `completeAgents` | `cfg.AgentNames()` | config-time |
| `completeProjects` | sorted keys of `cfg.Projects` | config-time |
| `completeProfiles` | `config.DiscoverProfiles(profilesDir)` | config-time |
| `completeBranches` | `worktree.Service.List(project)` for `args[0]` | runtime (git) |
| `completeSessions` | `session.Service.List()` | runtime (tmux) |

All providers return `cobra.ShellCompDirectiveNoFileComp` so the shell never
falls back to filename completion. On **any** error (config load failure, git or
tmux failure, missing project), a provider returns
`nil, cobra.ShellCompDirectiveNoFileComp` — completion silently yields nothing
rather than erroring or leaking a stack trace into the user's prompt.

### Config loading during completion

Cobra does **not** run `PersistentPreRunE` before invoking completion functions,
so the package globals `cfg` and `registry` are nil during a completion call.

Add a helper:

```go
// completionConfig loads config for a completion call, reading the already-parsed
// --profile and --config flag values off cmd. PersistentPreRunE does not run
// during completion, so the cfg/registry globals are unavailable here.
// Returns an empty (non-nil) config on any load error so callers can treat the
// result uniformly.
func completionConfig(cmd *cobra.Command) *config.Config
```

It reads the `--config` and `--profile` flag values via `cmd.Flags()`
(Cobra populates flags already typed on the line before calling completion
functions). It then resolves the profile through the **same precedence the
runtime uses** — `config.Load(cfgFile, resolveProfileArg(profileFlag))` — and
returns the resulting `*config.Config`. On error it returns an empty
`&config.Config{}` so providers need no nil checks.

`resolveProfileArg` (already in `cmd/root.go`, added in commit `6cdce4d`)
applies the precedence `defaults.main_profile < CODEHERD_PROFILE < --profile`:
when `--profile` is unset it falls back to the `CODEHERD_PROFILE` env var.
Completion **must** reuse it rather than reading the flag alone — otherwise
completion fired from inside a profile-mode session (where `CODEHERD_PROFILE` is
stamped into the tmux env but `--profile` is not retyped, e.g. nested
`ch run <agent>`) would complete against the wrong profile's branches and
sessions. Reusing `resolveProfileArg` keeps runtime providers
(`completeBranches`, `completeSessions`) profile-correct in exactly the cases
that commit targets.

### Positional dispatch

Commands taking `<project> <branch>` use a single `ValidArgsFunction` that
switches on `len(args)`:

- `len(args) == 0` → `completeProjects`
- `len(args) == 1` → `completeBranches` for the project named in `args[0]`
- otherwise → no completions

### Per-command wiring

| Command | arg[0] | arg[1] | flag completions |
|---|---|---|---|
| `run` | agents | — | — |
| `create session` | projects | branches | `--agent`, `--from` |
| `delete session` | projects | branches | — |
| `show session` | projects | branches | — |
| `attach session` | projects | branches | — |
| `create worktree` | projects | branches | `--agent`, `--from` |
| `delete worktree` | projects | branches | — |
| `show project` | projects | — | — |
| `clone project` | projects | — | — |
| `list worktree` | projects | — | — |
| root (persistent) | — | — | `--profile` |

- Positional completion attached via `ValidArgsFunction` in each command's
  `Cobra()` method.
- Flag value completion attached via
  `cmd.RegisterFlagCompletionFunc("agent", completeAgents)` (and `from` →
  `completeBranches`, `profile` → `completeProfiles`) in the same method.
- `--profile` is a persistent flag on root; its completion is registered once on
  `rootCmd`.

Note: `completeBranches` for the `--from` flag completes against the project
positional already present on the line (`args[0]` when available); when no
project is resolvable it returns nothing.

### Enablement (docs only)

Cobra auto-generates `ch completion bash|zsh|fish|powershell`. Add a README
section showing per-shell sourcing, e.g.:

```bash
# zsh
ch completion zsh > "${fpath[1]}/_ch"
# bash
ch completion bash | sudo tee /etc/bash_completion.d/ch
```

No installer subcommand.

## Testing

- **Config-time providers** (`completeAgents`, `completeProjects`,
  `completeProfiles`): unit tests call the provider directly with a fake `cfg`
  (or a temp config file for `completionConfig`) and assert the returned slice
  and `ShellCompDirective`.
- **Runtime providers** (`completeBranches`, `completeSessions`): use the
  existing `worktree.WorktreeRunner` and `tmux.Runner` mocks — no real git or
  tmux needed.
- **Positional dispatch**: test the `ValidArgsFunction` at `len(args)` 0, 1, and
  2 to confirm it routes to projects, branches, and nothing respectively.
- **Error paths**: assert a load/runtime error yields
  `nil, ShellCompDirectiveNoFileComp`.
- **Profile precedence**: a test sets `CODEHERD_PROFILE` (name via
  `config.EnvProfileForTest()`) with no `--profile` flag and asserts
  `completionConfig` resolves the env-sourced profile; another sets both and
  asserts `--profile` wins. Tests must clear any ambient `CODEHERD_PROFILE`
  first so the developer's environment cannot taint the result.

All new tests are plain `go test` (no integration tag), keeping aggregate
coverage ≥ 80% per the project requirement. `make check` must pass before the
task is complete.

## Out of scope / future

- Completion for `--config` file paths (let the shell's default file completion
  handle it on that flag).
- A `ch completion install` convenience command.
