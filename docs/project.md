# codeherd — Parallel Agentic Coding Session Manager

## Purpose

`codeherd` is a personal CLI for managing parallel agentic coding sessions. It organizes projects and git worktrees, configures per-agent environments with deterministic port allocation, and orchestrates tmux sessions where AI coding agents run independently.

It is agent-agnostic: any CLI tool (Claude Code, Aider, Codex, or a custom script) can be a named agent with its own command, arguments, and environment variables. codeherd manages the container around the agent — the tmux session, the worktree, the environment — not the agent itself.

Each session is independent. A codeherd session running Claude Code with Agent Teams, another running Aider, and a third running a plain shell can coexist. codeherd does not care what runs inside; it cares about the infrastructure that makes parallel work possible.

---

## Inspiration

This project draws from the workflow described in:

> **Claude Code On-The-Go** — https://granda.org/en/2026/01/02/claude-code-on-the-go/

The article runs six Claude Code agents in parallel on an ephemeral cloud VM, using git worktrees for isolation, tmux for session persistence, and deterministic port allocation to avoid conflicts. codeherd takes the same foundational ideas — worktrees, tmux, deterministic ports — and builds a structured CLI around them, with project awareness, named agent configurations, and automatic environment setup.

---

## Design Philosophy

### Primitives first

`codeherd` is built **primitives-first**. The core commands (`project`, `worktree`, `session`) are independent, composable building blocks. Each is useful on its own and testable in isolation.

Higher-level automation (e.g., a single `ch setup` command that clones a project, creates a worktree, generates the environment, and starts a session) will be built **on top of** these primitives, not instead of them. This layering is intentional:

1. **Primitives are stable** — the individual operations (clone a repo, create a worktree, start a session) are well-defined and unlikely to change.
2. **Composite commands are opinionated** — a "start everything" command embeds workflow assumptions that differ between use cases. Building it on stable primitives means the opinions live in one place.
3. **Debuggability** — when something fails in a composite flow, the user can drop down to the primitive that failed and run it in isolation.

### Project-aware, not just worktree-aware

Unlike tools that treat worktrees as the primary unit, codeherd treats **projects** as the organizing concept. A project has a git repository URL, a default branch, and potentially many worktrees. Sessions are anchored to a project and branch — `myapp-feature`, not just `feature`. This means:

- Worktrees from different projects never collide
- The TUI can group sessions by project
- Composite commands can operate on "all worktrees for project X"

### Agent-agnostic with named configurations

Agents are defined once in config and selected at session start:

```toml
[agents.claude]
cmd = "claude"
args = ["--dangerously-skip-permissions"]

[agents.aider]
cmd = "aider"
args = ["--model", "opus"]
```

This decouples the session lifecycle from any specific agent. The same worktree can host a Claude session today and an Aider session tomorrow. Agent selection happens at runtime via `--agent` flag or TUI picker.

### Local-first

All session management commands execute **directly on the machine where codeherd is invoked** — git, tmux, and filesystem operations run locally with no SSH indirection.

This means codeherd works on any machine: a laptop, a cloud VM, a remote server. The same commands, same config structure, same behavior everywhere.

---

## Goals

1. **Project-aware session management** — sessions are anchored to projects and branches, with structured directory layouts and clear naming.
2. **Named agent configurations** — define agents once in config, select at session start. Each agent has its own command, arguments, and environment.
3. **Deterministic environment setup** — `.herd` template processing with hash-based port allocation (`port "name"`) eliminates conflicts across parallel sessions.
4. **Automatic project bootstrapping** — clone, create worktree, generate environment, start session — composable primitives that chain into one-step setup.
5. **Agent-agnostic** — any CLI tool can be a named agent. codeherd manages the session container, not the agent.
6. **TUI for fleet overview** — visual dashboard showing all sessions, their status, and quick actions (attach, create, stop).
7. **Self-contained binary** — a single Go binary with no runtime dependencies beyond git and tmux.

---

## Non-Goals

- Not a replacement for Agent Teams or subagents (complementary — a codeherd session can run Agent Teams inside it)
- Not a general-purpose infrastructure tool (use Terraform/Pulumi for that)
- Not multi-user
- Not a container orchestration tool
- Not a mobile-first tool or push notification system

---

## Use Cases

### Primary: Parallel agentic coding sessions

Run multiple AI coding agents in parallel, each in its own tmux session with an isolated git worktree. Different agents (Claude, Aider, Codex) can run side by side. Switch between sessions via tmux or the TUI.

### Primary: Multi-project development

Work across multiple projects simultaneously. Each project has its own directory layout, worktrees, and sessions. The TUI groups everything by project for quick navigation.

### Secondary: Isolated experiments

Create a worktree for a throwaway branch, start an agent session, let it work, review the results, delete everything. The worktree isolation means experiments never touch the main checkout.

---

## Architecture

### Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.26 |
| CLI framework | [Cobra](https://github.com/spf13/cobra) |
| TUI | [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss) |
| Config format | TOML |
| Linter | golangci-lint |

### Command structure

Commands follow a `ch <verb> <subject>` grammar. The verb expresses the action; the subject narrows it to a resource type.

| Command | Description | Status |
|---|---|---|
| `ch` | Launch the TUI dashboard | Done |
| `ch list project` | List configured projects | Done |
| `ch show project <name>` | Show project details | Done |
| `ch clone project <name>` | Clone a project | Done |
| `ch list worktree` | List all worktrees | Done |
| `ch create worktree <project> <branch>` | Create a worktree | Done |
| `ch delete worktree <project> <branch>` | Delete a worktree | Done |
| `ch list session` | List active sessions | Done |
| `ch create session <project> <branch>` | Start an agent session (or `--shell` for a plain shell) | Done |
| `ch attach session <project> <branch>` | Attach to a session | Done |
| `ch show session <project> <branch>` | Show session details | Done |
| `ch delete session <project> <branch>` | Stop a session | Done |
| `ch template [dir]` | Process `.herd` template files | Done |
| `ch setup` | One-command project bootstrapping | Planned |

### tmux session architecture

Each agent session runs in its own tmux session. Shell sessions and the TUI also run as tmux sessions:

```
tmux sessions:
  codeherd         <- TUI / management session
  myapp-feature    <- agent session (Claude Code)
  myapp-fix-123    <- agent session (Aider)
  api-experiment   <- agent session (Claude Code)
  myapp-shell      <- permanent shell session
```

Navigation uses tmux's built-in session switching:
- `Ctrl+B s` — session picker (shows all sessions)
- `Ctrl+B ( / )` — previous / next session

---

## Key File Locations

| Purpose | Path |
|---|---|
| CLI config | `~/.config/codeherd/config.toml` |
| Binary | `~/.local/bin/ch` |

### `~/.config/codeherd/config.toml` structure

```toml
[defaults]
projects_dir = "~/projects"      # base directory for project clones and worktrees
agent = "claude"                 # default agent for new sessions

[agents.claude]
cmd = "claude"
args = ["--dangerously-skip-permissions"]

[agents.claude.env]
CLAUDE_CONFIG_DIR = "/custom"

[agents.aider]
cmd = "aider"
args = ["--model", "opus"]

[projects.myapp]
repo = "git@github.com:user/myapp.git"
default_branch = "main"
# cloned to: ~/projects/github.com/user/myapp/

[projects.api]
repo = "git@github.com:user/api.git"
default_branch = "develop"
```

### Project directory layout

Clone paths mirror the repo URL structure under `projects_dir` (like `ghq` / Go module paths):

```
~/projects/
  github.com/
    user/
      myapp/                       <- main clone
      myapp__worktrees/
        feature/                   <- worktree for branch "feature"
        fix-123/                   <- worktree for branch "fix-123"
      api/                         <- different project
  gitlab.com/
    corp/
      backend/
        api/                       <- clone from gitlab
        api__worktrees/
          develop/                 <- worktree
```

---

## Package Layout

```
codeherd/
├── main.go
├── go.mod / go.sum
├── Makefile
├── .golangci.yml
│
├── cmd/
│   ├── root.go          — cobra root, persistent flags, config load, TUI launch
│   ├── register.go      — wires verb commands (list, create, delete, show, clone, attach) to their subjects
│   ├── services.go      — shared service constructors
│   ├── errors.go        — shared error helpers
│   ├── project.go       — project subcommands (list, show, clone)
│   ├── worktree.go      — worktree subcommands (list, create, delete)
│   ├── session.go       — session subcommands (list, create, attach, show, delete)
│   ├── tui.go           — TUI launch helpers
│   ├── template.go      — template subcommand
│   └── plugin.go        — plugin subcommand
│
├── internal/
│   ├── config/          — TOML config: defaults, agents, projects; RepoPath() URL→path derivation
│   ├── session/         — tmux session lifecycle: start, stop, list, attach
│   ├── worktree/        — git worktree operations: new, delete, list
│   ├── project/         — project clone and directory management
│   ├── tmux/            — typed tmux command wrapper
│   ├── tui/             — Bubble Tea v2 dashboard
│   ├── herdtemplate/    — .herd template processing: deterministic ports, env var interpolation
│   └── semconv/         — semantic conventions (session naming, path conventions)
│
└── docs/
    ├── project.md
    └── landscape.md
```

---

## Development Roadmap

### What's done

- [x] Internal packages: config, project, worktree, session, tmux, herdtemplate, tui, semconv
- [x] `ch list/show/clone project` — project management
- [x] `ch list/create/delete worktree` — worktree management
- [x] `ch list/create/attach/show/delete session` — session management with named agent support and unified agent/shell sessions
- [x] `ch template` — ad-hoc `.herd` template processing
- [x] `ch` (no subcommand) — Bubble Tea v2 dashboard with session/worktree/project views
- [x] Named agent configurations (`[agents.<name>]`)
- [x] Session state via tmux user-defined options (no file-based state)
- [x] Automatic worktree creation on session start

### Next

- [ ] `ch setup` — one-command project bootstrapping: clone + worktree + env + session
- [ ] `.herd` template integration into session start (auto-render `.herd` files before launching agent)

### Future: Quality of life

- [ ] Desktop notifications when a session needs attention
- [ ] Session groups / batch operations (start/stop all sessions for a project)
- [ ] Agent health monitoring (detect stuck agents, auto-restart, resource usage alerts)

---

## Agent Session Context

When starting a new agent session to continue development on this project, provide this file as context. Key decisions already made:

1. **Go + Cobra** for the CLI
2. **Bubble Tea v2** for the TUI (not v1)
3. **XDG-compliant** file locations (config in `~/.config`)
4. **Go version**: 1.26 (managed via mise)
5. **Binary install location**: `~/.local/bin/ch`
6. **All session commands run locally** — no SSH indirection for project/worktree/session
7. **Each agent session = one tmux session** — management in `codeherd` session
8. **projects_dir is configurable** — default `~/projects`
9. **Clone paths mirror repo URL** — `~/projects/github.com/user/myapp/` (like `ghq` / Go modules)
10. **Named agents** — `[agents.<name>]` in config, selected via `--agent` flag or TUI picker
11. **Session state stored in tmux** — user-defined options on tmux sessions, no state files
12. **Agent-agnostic** — codeherd manages the session container, agents are pluggable
13. **`ch <verb> <subject>` command grammar** — verb is the action (list, create, delete, show, clone, attach), subject is the resource type (project, worktree, session)

---

## TODO / Deferred Ideas

Ideas and features that came up during development but were intentionally deferred.

| Idea | Context | Notes |
|---|---|---|
| Session groups / batch operations | Natural extension of project-awareness | Start/stop all sessions for a project; "refresh all worktrees" |
| Agent health monitoring | TUI enhancement | Detect stuck agents, auto-restart, resource usage alerts |
| Template-based project setup | Extension of `.herd` templates | Per-project setup scripts that run after worktree creation (install deps, build, etc.) |
