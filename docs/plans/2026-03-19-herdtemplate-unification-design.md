# Herdtemplate Unification Design

Replace `envtemplate` with `herdtemplate` as the single template engine and add standalone commands for running template processing on any directory.

## Context

Two template packages exist: `envtemplate` processes a single `.env.template` into `.env`; `herdtemplate` processes any `*.herd` file into its unsuffixed counterpart. Both share the same template functions (`port`, `env`) and context shape. `herdtemplate` already imports `envtemplate.DeterministicPort`. The duplication is unnecessary — `herdtemplate` is the strict superset.

## Decisions

- **Delete `internal/envtemplate`** — no migration path for `.env.template` files; users rename to `.env.herd`.
- **Move `DeterministicPort`** into `herdtemplate`.
- **Remove `ProjectConfig.EnvTemplate`** config field and its `expandPaths()` handling in `config.Load()`. File copying already handles external file placement.
- **Remove `resolveTemplate()`**, `worktree.Service.Env()`, `EnvResult`, and `EnvWritten` from `NewResult`.
- **Remove `.env.template` processing** from `worktree.Service.New()` and `NewFrom()`.
- **Remove `ch worktree env`** command — clean break, no deprecation alias.
- **Add dry-run** to `herdtemplate.Process()`.
- **Add `ch worktree template`** — project-aware replacement for `worktree env`.
- **Add `ch template`** — top-level command for ad-hoc use on any directory.

## Package Changes

### `internal/herdtemplate`

Move `DeterministicPort()` in from `envtemplate` (same FNV-1a logic, range 10000–59999). Delete `ParseEnvFile` — it has no callers outside its own tests.

Add dry-run support and structured results:

```go
type RenderedFile struct {
    Source string // e.g. "docker-compose.yml.herd"
    Target string // e.g. "docker-compose.yml"
    Output string // rendered content
}

type ProcessResult struct {
    Files []RenderedFile
}
```

Add `DryRun` field to `ProcessContext`. When true:
- `renderFile()` skips `os.WriteFile`
- Pre/post-template hooks are **not** triggered (dry-run is purely read-only)

When false, behavior is unchanged from today — hooks fire, files are written.

### `internal/config`

Remove `EnvTemplate string` field from `ProjectConfig`. Remove the `expandPaths()` logic that expanded `~/` in that field.

### `internal/worktree`

Remove:
- `resolveTemplate()` helper
- `Service.Env()` method and `EnvResult` struct
- `.env.template` processing from `Service.New()` and `Service.NewFrom()`
- `EnvWritten` field from `NewResult`

### Callers

`cmd/session.go` and `internal/tui/actions.go` already call `herdtemplate.Process()` directly — no changes needed beyond removing any `envtemplate` imports.

## New Commands

### `ch template [dir]`

Top-level command in `cmd/template.go`.

| Flag | Description |
|---|---|
| `--project` | Override project name |
| `--branch` | Override branch name |
| `--dry-run` | Print rendered output without writing |

**Argument:** directory path, defaults to `.` if omitted.

**Project resolution:**
1. Walk configured projects, compute each project's clone directory via `semconv.CloneDir(projectsDir, config.RepoPath(repo))`.
2. Check if `dir` is inside (or equal to) any project's clone dir or worktrees root — first match wins.
3. If no match, fall back to `--project` flag.
4. Error if still unresolved.

**Branch resolution:**
1. Run `git rev-parse --abbrev-ref HEAD` in `dir`.
2. Fall back to `--branch` flag.
3. Error if still unresolved.

Calls `herdtemplate.Service.Process()` with the resolved context.

### `ch worktree template <project> <branch>`

Replaces `worktree env` in `cmd/worktree.go`.

| Flag | Description |
|---|---|
| `--dry-run` | Print rendered output without writing |

Resolves the worktree path from project + branch using existing `resolvePaths()`. Calls the same `herdtemplate.Service.Process()`.
