# Foreground Agent Runner Design

## Date
2026-05-06

## Status
Approved

## Summary
Add a `ch run <agent>` command that executes a registered agent directly in the current shell as a foreground process, replacing the `ch` process via `syscall.Exec`. No tmux session is created, no worktree context is required, and the current shell environment is preserved with the agent's configured env vars overlaid.

## Motivation
Currently, running an agent in codeherd requires the full session lifecycle: `ch create session <project> <branch> --agent <name>`, which creates a detached tmux session in a specific worktree with codeherd-specific environment variables. Users who want to run an agent quickly in an existing tmux panel or window must go through this ceremony. A lightweight foreground runner lets users spawn agents directly in any shell context they have already prepared.

## Requirements

1. `ch run <agent-name>` resolves the agent from `[agents.<name>]` in the config.
2. The agent runs in the current shell process (no tmux session, no worktree).
3. The agent inherits the current shell environment (`os.Environ()`), with agent-configured env vars overlaid (agent wins on conflicts).
4. No `CODEHERD_*` env vars are injected. The agent gets exactly the user's current context plus its own config.
5. The `ch` process is replaced by `syscall.Exec`; signals and exit codes pass through natively.
6. If the agent name is unknown, print an error to stderr and exit without exec.
7. If the agent command binary cannot be found on `$PATH`, `syscall.Exec` fails naturally and the shell reports it.

## Non-Requirements

- No session tracking, state, or tmux integration.
- No automatic tmux panel/window creation. The user is responsible for their own layout.
- No project, branch, worktree, or profile context.
- No hooks are triggered (no `pre-session` / `post-session`).

## Design

### Command

```
ch run <agent-name>
```

`run` is a root-level verb (not nested under `create`), since it does not create a tmux session.

### Data Flow

```
User runs: ch run <agent-name>
        |
        v
    cmd/run.go
        |
        v
    cfg.AgentByName(name)
        |
        +-- not found -> error, exit non-zero
        |
        +-- found -> AgentConfig {Cmd, Args, Env}
        |
        v
    Merge(os.Environ(), agent.Env)
        (agent.Env keys override current env)
        |
        v
    syscall.Exec(agent.Cmd, agent.Args, mergedEnv)
        |
        v
    Agent process replaces ch process
```

### Env Merging

Extracted to a pure function for testability:

```go
func mergeEnv(current []string, overrides map[string]string) []string
```

- Start with `current` (from `os.Environ()`).
- Parse each entry into `key=value`.
- Overlay each key from `overrides`. If a key exists in `current`, replace its value. If not, append it.
- Return the merged slice.

### `syscall.Exec` Wrapper

A package-level `var syscallExec` function (same pattern as `execTmuxAttach` in `cmd/session.go`):

```go
var syscallExec = syscall.Exec
```

This allows unit tests to inject a mock that records the call instead of actually replacing the process.

### Error Handling

| Scenario | Behavior |
|---|---|
| Agent not found | Print `agent "<name>" not found` to stderr, exit code 1 |
| Config not loaded | Same error path as other commands (handled by `PersistentPreRunE`) |
| Binary not found | `syscall.Exec` returns error; shell prints its own error message |

## Testing

- **Unit tests for `mergeEnv`:** pure function, no dependencies.
- **Unit tests for `RunAgentCmd.Run`:** mock `syscallExec` to assert the correct cmd, args, and env are passed. Mock `lookPath` if we decide to validate the binary exists before exec (not required for MVP).
- **No integration tests needed** — the command is a thin wrapper over config lookup + exec.

## Files Changed

- `cmd/register.go` — wire `run` command into root.
- `cmd/run.go` — new file: `RunAgentCmd` struct with `Cobra()` and `Run()` methods.
- `cmd/run_test.go` — new file: unit tests.

## Future Considerations

- `--env` flag to pass additional env vars on the fly: `ch run claude --env KEY=val`.
- `--` separator to pass extra args to the agent: `ch run claude -- --some-flag`.
- Both are out of scope for the initial implementation.
