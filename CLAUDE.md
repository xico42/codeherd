# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

`codeherd` is a personal Go CLI for managing parallel agentic coding sessions. It organizes projects and git worktrees, configures per-agent environments with deterministic port allocation, and orchestrates tmux sessions where AI coding agents run independently. Full context in `docs/project.md`.

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

## Git worktrees

Worktrees for this project live at `~/.config/superpowers/worktrees/remote-dev/<branch-name>`.

```bash
git worktree add ~/.config/superpowers/worktrees/remote-dev/<branch> -b <branch>
```

## Coverage requirement

Before marking any task complete, run:
```bash
make check
```
This runs coverage (80% minimum), integration tests, lint, and build. New code must include tests sufficient to keep aggregate coverage at or above 80%.

## Architecture

### Command groups

Commands are organized by function:

- **Session management** (`session`, `tui`) — start, list, attach, stop agent sessions; interactive dashboard
- **Project & worktree management** (`project`, `worktree`) — clone repos, manage git worktrees
- **Configuration** (`config`) — manage projects, agents, and settings
- **Remote execution** (`up`, `down`, `status`, `ssh`, `notify`) — planned; ephemeral DO droplets for remote sessions

Session, project, and worktree commands run locally — they execute git/tmux/filesystem directly on whatever machine codeherd is invoked. They operate on `projects_dir` (configurable, default `~/projects`) and local tmux.

### Package layout

- **`main.go`** — entrypoint; delegates to `cmd.Execute()`; `version` var set via `-ldflags`
- **`cmd/`** — Cobra commands; `root.go` wires `PersistentPreRunE` to load config; each command is a separate file
- **`internal/config`** — TOML config at `~/.config/codeherd/config.toml`; `Load()` returns defaults on missing file; `ApplyEnv()` overlays env vars; `ApplyFlags()` overlays CLI flags; includes `[projects]`, `[agents]`, `projects_dir`; `RepoPath()` derives filesystem paths from git URLs (e.g. `git@github.com:user/myapp.git` → `github.com/user/myapp`); `AgentByName()` / `AgentNames()` for named agent lookup
- **`internal/session`** — tmux session lifecycle: start, stop, list, attach; session state via tmux user-defined options
- **`internal/worktree`** — git worktree operations: new, delete, list, shell, env
- **`internal/project`** — project clone and directory management
- **`internal/tmux`** — typed tmux command wrapper (NewClient, Runner interface for testing)
- **`internal/tui`** — Bubble Tea v2 dashboard with session/worktree/project views
- **`internal/herdtemplate`** — processes `.herd` template files with Go `text/template`; custom funcs: `port "name"` (deterministic FNV-1a hash), `env "VAR" "default"`; renders any `*.herd` file to its unsuffixed counterpart
- **`internal/semconv`** — semantic conventions (session naming, path conventions)
- **`internal/state`** — JSON state at `~/.local/share/codeherd/state.json`; tracks active droplet (future remote phase)
- **`internal/do`** — thin wrapper around godo; `DropletsService` interface (future remote phase)
- **`internal/provision`** — renders cloud-init user-data via `embed.FS` + `text/template` (future remote phase)

### Config and state paths (XDG-compliant)

| Purpose | Path |
|---|---|
| Config | `~/.config/codeherd/config.toml` |
| Droplet state (future) | `~/.local/share/codeherd/state.json` |
| Binary | `~/.local/bin/ch` |

### Key design patterns

- **Named agents**: `[agents.<name>]` in config define cmd/args/env; selected via `--agent` flag or TUI picker; `AgentByName()` for lookup
- **Session state in tmux**: session metadata stored as tmux user-defined options on sessions, not in state files
- **Mocking**: `internal/do` exposes `DropletsService` interface; `internal/tmux` exposes `Runner` interface; `internal/worktree` exposes `WorktreeRunner` interface — tests use mock implementations
- **Missing file = empty defaults**: `config.Load()` and `state.Load()` return zero-value structs (not errors) when the file doesn't exist
- **`syscall.Exec` for interactive commands**: `ch worktree shell`, `ch session attach` replace the process rather than spawning a child
- **Local execution**: all session/project/worktree commands run git/tmux via `os/exec` on the local machine — no SSH indirection
- **Integration tests**: tagged with `//go:build integration` and run separately via `make test-integration`

### What's implemented

- All session management: start, list, attach, stop, show, with named agent support and automatic worktree creation
- All project management: list, show, clone
- All worktree management: list, new, delete, shell, env
- Config management: init, show, set, get, profiles
- TUI dashboard with session/worktree/project views
- Herd template processing with deterministic ports

### What's planned

- `ch setup` — one-command project bootstrapping (clone + worktree + env + session)
- Remote execution phase: `up`, `down`, `status`, `ssh` for ephemeral DO droplets
