# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

<!-- Add Unreleased entries here. -->

## [0.2.0] - 2026-07-06

### Added

- `create worktree --track [<remote>/]<branch>` fetches a remote branch and checks it out into a worktree that tracks it, deriving the local name from the remote branch when the branch argument is omitted. Every creation path now freshens its source first, fast-forwarding a matching local branch with `--ff-only` before branching, and the TUI rebinds `r` to a filterable remote-branch picker.
- The TUI shows an animated busy spinner during slow git operations — creating a worktree, cloning a project, and fetching remote branches — and ignores input other than quit until the operation finishes.
- `ch run <agent> -- <args>` forwards every token after `--` to the agent verbatim, appended after its configured arguments, so per-invocation flags reach the agent without editing config.

### Fixed

- The dashboard and `ch list worktree` keep a worktree's tmux session visible after its HEAD diverges from the branch it was created for, so a rebase or a branch checkout no longer makes a running agent disappear; the listing annotates a diverged worktree with its live state (`detached` or `on <branch>`).

## [0.1.0] - 2026-05-28

### Added

- `project` command (`list`, `show`, `clone`) manages repo aliases and clone directories.
- `worktree` command (`list`, `create`, `delete`) checks out parallel branches under each project, with `--from <branch>`, `--attach`, and `--agent` flags.
- `session` command (`list`, `create`, `delete`, `show`, `attach`) runs tmux-backed agent and shell sessions, storing session state in tmux user-defined options.
- `ch create session` creates the worktree automatically when the target branch has no checkout.
- TUI dashboard launches when you run `ch` with no subcommand, covers project, worktree, and session views, offers a contextual delete flow and a navigable agent picker, and runs inside a dedicated `codeherd` tmux session unless you pass `--no-tmux`.
- Named agents live under `[agents.<name>]` in `config.toml`, selected at session start via `--agent` or the TUI picker; `[defaults].agent` sets the default.
- Hooks fire during the clone, worktree, file-copy, template, and session stages, configured per project.
- File-copy step copies external files (editor config, prompt files, `.env`) into new worktrees through `src:dst` entries.
- `.herd` template engine ships `port` (deterministic FNV-1a hash per project, branch, and name, in the range 10000–59999) and `env` helpers, and supports dry-run.
- `ch template [dir]` renders `.herd` files outside the worktree lifecycle.
- Opt-in profiles, enabled by `[defaults].profiles_enabled`, isolate personal, work, and client contexts under one install; the active profile resolves from `-p/--profile`, then `CODEHERD_PROFILE`, then `[defaults].main_profile`, and each session carries a `<profile>-` tmux name prefix so two profiles never collide on the same `(project, branch)` pair.
- Every agent and shell session receives `CODEHERD_SESSION`, `CODEHERD_PROJECT`, `CODEHERD_BRANCH`, `CODEHERD_WORKTREE_PATH`, `CODEHERD_CLONE_DIR`, and `CODEHERD_PROFILE`.
- `ch run <agent>` replaces the current process with a registered agent in the foreground, inherits the shell environment, and skips the tmux and worktree lifecycle.
- Shell completion suggests project, branch, agent, and profile names dynamically.

[Unreleased]: https://github.com/xico42/codeherd/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/xico42/codeherd/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/xico42/codeherd/releases/tag/v0.1.0
