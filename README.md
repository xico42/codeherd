# codeherd

A CLI for managing parallel agentic coding sessions. It organizes projects and git worktrees, configures per-agent environments with deterministic port allocation, and orchestrates tmux sessions where AI coding agents run independently.

It is like a shepherd, but for coding agents.

## Why

Running multiple AI coding agents in parallel requires isolated workspaces, separate environments, and session management. codeherd handles the infrastructure -- git worktrees for isolation, tmux sessions for persistence, deterministic ports to avoid conflicts -- so each agent gets a clean, independent workspace without manual setup.

## How It Works

codeherd manages a lifecycle that takes a project from configuration to a running agent session:

```
Clone -> Worktree -> File Copy -> Template Processing -> Session Start
```

Each step has pre/post hooks for custom automation (install dependencies, start services, notify external systems). See [docs/hooks.md](docs/hooks.md) for the full hook lifecycle reference.

## Configuration

All configuration lives in `~/.config/codeherd/config.toml`. Here is a full example:

```toml
[defaults]
projects_dir = "~/projects"    # base directory for clones and worktrees
agent = "claude"               # default agent for new sessions

# ── Agents ──────────────────────────────────────────────────────────────────

[agents.claude]
cmd = "claude"
args = ["--dangerously-skip-permissions"]

[agents.claude.env]
CLAUDE_CONFIG_DIR = "/home/user/.config/claude"

[agents.aider]
cmd = "aider"
args = ["--model", "opus"]

[agents.codex]
cmd = "codex"
args = ["--approval-mode", "full-auto"]

# ── Projects ────────────────────────────────────────────────────────────────

[projects.myapp]
repo = "git@github.com:user/myapp.git"
default_branch = "main"

# Files copied into every new worktree (see File Copy section)
files = [
  "CLAUDE.md",                                       # same path in worktree
  ".cursorrules",                                     # same path in worktree
  "~/.config/codeherd/prompts/safety.md:RULES.md",   # absolute source, custom destination
]

# Hooks run at each lifecycle step (see Hooks section)
[projects.myapp.hooks]
post-clone = "make deps"
post-worktree = "npm install"
pre-session = "docker compose up -d"
post-session = "curl -s https://hooks.example.com/started"

[projects.api]
repo = "git@github.com:user/api.git"
default_branch = "develop"
files = [".envrc"]

[projects.api.hooks]
post-worktree = "bundle install"
```

### Projects

Each project points to a git repository and a default branch. Clone paths mirror the repo URL under `projects_dir`:

```
~/projects/
  github.com/user/
    myapp/                    # main clone
    myapp__worktrees/
      feature/                # worktree for "feature" branch
      fix-123/                # worktree for "fix-123" branch
    api/                      # another project
```

### Agents

Agents are CLI tools configured once and selected at session start. Any command-line tool works -- Claude Code, Aider, Codex, or a custom script. Each agent defines a command, optional arguments, and optional environment variables.

### Sessions

Sessions are tmux sessions anchored to a project and branch. They run independently and persist across disconnects:

```
tmux sessions:
  codeherd         # TUI dashboard
  myapp-feature    # Claude Code working on feature
  myapp-fix-123    # Aider fixing a bug
  api-experiment   # another agent exploring an idea
```

## Lifecycle

When you create a worktree and start a session, codeherd runs through a five-step lifecycle. Each step has optional pre/post hooks.

```
 Clone ──> Worktree ──> File Copy ──> Template Processing ──> Session Start
  │  │      │  │         │  │          │  │                    │  │
 pre post  pre post     pre post      pre post                pre post
```

### File Copy

The `files` list in project config copies files into new worktrees. This is useful for shared configuration (editor rules, prompt files, env configs) that should exist in every worktree but isn't tracked in git.

| Entry format | Source | Destination |
|---|---|---|
| `"CLAUDE.md"` | Clone dir / `CLAUDE.md` | Worktree / `CLAUDE.md` |
| `"src/config.json"` | Clone dir / `src/config.json` | Worktree / `src/config.json` |
| `"~/.config/prompts/safety.md:RULES.md"` | `~/.config/prompts/safety.md` | Worktree / `RULES.md` |
| `"/absolute/path/file.txt:subdir/file.txt"` | `/absolute/path/file.txt` | Worktree / `subdir/file.txt` |

Relative paths resolve from the clone directory. Absolute paths and `~/` paths copy from the filesystem. Intermediate directories are created automatically.

### Template Processing

After files are copied, codeherd scans the worktree for `.herd` files and renders them using Go's `text/template` engine. The rendered output is written as a sibling file without the `.herd` suffix:

- `.env.herd` renders to `.env`
- `docker-compose.yml.herd` renders to `docker-compose.yml`
- `nginx.conf.herd` renders to `nginx.conf`

This is how you generate per-worktree configuration with unique ports and branch-specific values.

#### Template Variables

Templates receive a context object with these fields:

| Variable | Type | Description | Example value |
|---|---|---|---|
| `.Project` | string | Project name from config | `myapp` |
| `.Branch` | string | Branch name | `feature` |
| `.WorktreePath` | string | Absolute path to the worktree | `/home/user/projects/github.com/user/myapp__worktrees/feature` |
| `.SessionName` | string | Derived session name | `myapp-feature` |

#### Template Functions

| Function | Description | Example |
|---|---|---|
| `port "name"` | Deterministic port (10000-59999) derived from project + branch + name. Same inputs always produce the same port, different branches get different ports. | `{{ port "http" }}` |
| `env "VAR" "default"` | Read an environment variable, with an optional fallback value. | `{{ env "API_KEY" "dev-key" }}` |

#### Example: `.env.herd`

```
# .env.herd — place this in your repo or copy it via the files list
APP_PORT={{ port "http" }}
GRPC_PORT={{ port "grpc" }}
DEBUG_PORT={{ port "debug" }}
DATABASE_URL=postgres://localhost:5432/{{ .Project }}_{{ .Branch }}
API_KEY={{ env "API_KEY" "dev-key-for-local" }}
SESSION={{ .SessionName }}
```

Running `ch create worktree myapp feature` renders this to `.env`:

```
# .env (generated)
APP_PORT=34521
GRPC_PORT=18973
DEBUG_PORT=42810
DATABASE_URL=postgres://localhost:5432/myapp_feature
API_KEY=dev-key-for-local
SESSION=myapp-feature
```

Every worktree gets unique ports. The `feature` branch and `fix-123` branch will never collide.

#### Example: `docker-compose.yml.herd`

```yaml
# docker-compose.yml.herd
services:
  postgres:
    image: postgres:16
    ports:
      - "{{ port "postgres" }}:5432"
    environment:
      POSTGRES_DB: {{ .Project }}_{{ .Branch }}
      POSTGRES_PASSWORD: {{ env "PG_PASSWORD" "devpass" }}

  redis:
    image: redis:7
    ports:
      - "{{ port "redis" }}:6379"
```

### Hooks

Hooks are shell commands that run at each lifecycle step. Configure them per-project:

```toml
[projects.myapp.hooks]
pre-clone = "echo preparing to clone"
post-clone = "make deps"
pre-worktree = "echo creating worktree"
post-worktree = "npm install"
pre-copy = "echo copying files"
post-copy = "chmod 600 .env"
pre-template = "vault read secret/myapp > .secrets"
post-template = "echo templates rendered"
pre-session = "docker compose up -d"
post-session = "curl -s https://hooks.example.com/started"
```

Hooks receive context as environment variables (`CODEHERD_PROJECT`, `CODEHERD_BRANCH`, `CODEHERD_WORKTREE_PATH`, etc.). A non-zero exit code stops the workflow. Omit a hook to skip it.

See [docs/hooks.md](docs/hooks.md) for the full reference including environment variables per step, working directory rules, and error handling.

## Install

Requires Go 1.22+, git, and tmux.

```bash
make install    # builds and installs to ~/.local/bin/ch
```

## Quick Start

```bash
# Configure a project (edit ~/.config/codeherd/config.toml)
# See the Configuration section above for the full config format

# Clone the project
ch clone project myapp

# Create a worktree and start a session
ch create session myapp feature --agent claude --attach
```

Or use the interactive TUI:

```bash
ch
```

## Commands

| Command | Description |
|---|---|
| `ch` | Launch the TUI dashboard |
| `ch list project` | List configured projects |
| `ch show project <name>` | Show project details |
| `ch clone project <name>` | Clone a project |
| `ch list worktree` | List all worktrees |
| `ch create worktree <project> <branch>` | Create a worktree |
| `ch delete worktree <project> <branch>` | Delete a worktree |
| `ch list session` | List active sessions |
| `ch create session <project> <branch>` | Start an agent session (use `--shell` for a plain shell) |
| `ch attach session <project> <branch>` | Attach to a session |
| `ch show session <project> <branch>` | Show session details |
| `ch delete session <project> <branch>` | Stop a session |

## Development

```bash
make build          # build ./ch binary
make test           # run unit tests
make lint           # run linter
make check          # coverage (80%+) + integration tests + lint + build
```

## Documentation

- [Project overview](docs/project.md) -- architecture, design philosophy, and roadmap
- [Hooks lifecycle](docs/hooks.md) -- hook configuration, file copy, template processing
