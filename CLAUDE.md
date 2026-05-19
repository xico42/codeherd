# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

`codeherd` is a Go CLI for managing parallel agentic coding sessions. It organizes projects and git worktrees, configures per-agent environments with deterministic port allocation, and orchestrates tmux sessions where AI coding agents run independently. Full context in `docs/project.md`.

## Build and development commands

```bash
make build           # build ./ch binary
make install         # build + install to ~/.local/bin/ch
make test            # go test ./...
make test-integration # go test -tags integration ./...
make lint            # golangci-lint run ./...
make check           # coverage (80%+) → integration tests → lint → build
make setup           # deps → verify (full first-time setup)
make clean           # remove local binary
```

Run a single package's tests:
```bash
go test ./internal/config/...
go test ./internal/session/...
# etc.
```

Build with version embedded (done automatically via Makefile):
```bash
go build -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o ch .
```

## Coverage requirement

Before marking any task complete, run:
```bash
make check
```
This runs coverage (80% minimum), integration tests, lint, and build. New code must include tests sufficient to keep aggregate coverage at or above 80%.

## Architecture

### Command groups

Commands are organized by verb:

- **`list`** — `list project` / `list worktree` / `list session`
- **`show`** — `show project <name>` / `show session <project> <branch> [--shell]`
- **`create`** — `create worktree <project> <branch> [--from/--attach/--agent]` / `create session <project> <branch> [--shell/--attach/--agent]`
- **`delete`** — `delete worktree <project> <branch> [--force]` / `delete session <project> <branch> [--shell/--force]`
- **`clone`** — `clone project [<name>] [--all]`
- **`attach`** — `attach session <project> <branch> [--shell]`
- **`template`** — root-level; `template [dir] [--project/--branch/--dry-run]`
- **Hidden**: `plugin handle-claude`
- **Default**: `ch` with no subcommand launches the TUI dashboard

All commands run locally — they execute git/tmux/filesystem directly on whatever machine codeherd is invoked. They operate on `projects_dir` (configurable, default `~/projects`) and local tmux.

### Package layout

- **`main.go`** — entrypoint; delegates to `cmd.Execute()`; `version` var set via `-ldflags`
- **`cmd/`** — Cobra commands; `root.go` wires `PersistentPreRunE` to load config; each command is a separate file
- **`cmd/register.go`** — wires the verb-grouper command tree; each verb group (`list`, `show`, `create`, etc.) is registered here
- **`cmd/services.go`** — read-only service constructor helpers shared across commands
- **`cmd/errors.go`** — error-printer helpers for consistent CLI error formatting
- **`cmd/tui.go`** — TUI launch machinery (extracted from root)
- **`internal/config`** — TOML config at `~/.config/codeherd/config.toml`; `Load()` returns empty `Config` on missing file; holds `Defaults{ProjectsDir, Agent}`, `Projects`, `Agents`; `RepoPath()` derives filesystem paths from git URLs (e.g. `git@github.com:user/myapp.git` → `github.com/user/myapp`); `AgentByName()` / `AgentNames()` for named agent lookup
- **`internal/session`** — tmux session lifecycle: start, stop, list, attach, show; `StartRequest.Type` and `SessionInfo.Type` unify agent and shell handling; `Service.Show`/`Stop` address by `(project, branch, sessionType)`; `SessionExistsError` carries `Project/Branch/Type`; `SetStatus` is agent-only; session state via tmux user-defined options
- **`internal/worktree`** — git worktree operations: new, delete, list, shell, env
- **`internal/project`** — project clone and directory management
- **`internal/tmux`** — typed tmux command wrapper (`NewClient`, `Runner` interface for testing)
- **`internal/tui`** — Bubble Tea v2 dashboard with session/worktree/project views
- **`internal/herdtemplate`** — processes `.herd` template files with Go `text/template`; custom funcs: `port "name"` (deterministic FNV-1a hash), `env "VAR" "default"`; renders any `*.herd` file to its unsuffixed counterpart
- **`internal/semconv`** — semantic conventions (session naming, path conventions)

### Config and state paths (XDG-compliant)

| Purpose | Path |
|---|---|
| Config | `~/.config/codeherd/config.toml` |
| Binary | `~/.local/bin/ch` |

### Key design patterns

- **Struct-per-command**: each CLI command is a struct with a `Cobra()` method; flags are exported fields on the struct; verb groupers are wired in `cmd/register.go`
- **Named agents**: `[agents.<name>]` in config define cmd/args/env; selected via `--agent` flag or TUI picker; `AgentByName()` for lookup
- **Session types**: agent (default) vs shell; both are first-class in `internal/session` via `StartRequest.Type` and `SessionInfo.Type`; `Show`/`Stop`/`delete session` accept `--shell` to target the shell type
- **Session state in tmux**: session metadata stored as tmux user-defined options on sessions, not in state files
- **Mocking via interfaces**: `internal/tmux` exposes `Runner`; `internal/worktree` exposes `WorktreeRunner` — tests use mock implementations
- **Missing file = empty defaults**: `config.Load()` returns an empty `Config` (not an error) when the file doesn't exist
- **`syscall.Exec` for interactive commands**: `attach session`, `create worktree --attach`, and related commands replace the process rather than spawning a child
- **Local execution**: all session/project/worktree commands run git/tmux via `os/exec` on the local machine — no SSH indirection
- **Integration tests**: tagged with `//go:build integration` and run separately via `make test-integration`
- **Tmux isolation in tests**: tests that touch a real tmux server must isolate it. Set `CODEHERD_TMUX_SOCKET` to a path under `t.TempDir()` (the `internal/tmux` `RealRunner` reads this env var and prepends `-S <path>` to every tmux call), also clear `$TMUX` so tmux does not think it is nested, and probe with a throwaway `tmux -S <socket> new-session` — `t.Skip` if it fails so sandboxed CI environments do not flake. Cleanup must `tmux -S <socket> kill-server`. The `cmd_test` package exposes `useIsolatedTmux(t)` + `tmuxCmd(t, args...)` helpers; tests in `package cmd` inline the same setup. Never call bare `exec.Command("tmux", …)` from a test — it will leak into the developer's outer tmux server and collide with parallel test runs.

