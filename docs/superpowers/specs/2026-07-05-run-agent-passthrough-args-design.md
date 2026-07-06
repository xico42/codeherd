# `ch run <agent> -- <args>` pass-through arguments

## Goal

Let users pass extra arguments to a registered agent when running it in the
foreground:

```
ch run <agent> -- <args...>
```

`<args...>` are forwarded to the agent's command verbatim (no shell
re-parsing), appended after the agent's configured arguments.

## Scope

`ch run` only. The tmux session flow (`create session --agent`) and the TUI
agent picker are explicitly out of scope for this change.

## Behavior

`ch run` resolves a registered agent and replaces the current process with it
via `syscall.Exec`. Today it builds:

```
execArgs = [binaryName, cmdPrefixArgs…, agent.Args…]
```

where `cmdPrefixArgs` come from splitting `agent.Cmd` (e.g. `ai-jail -- claude`)
and `agent.Args` are the agent's configured args. This change appends the
pass-through args to the end:

```
execArgs = [binaryName, cmdPrefixArgs…, agent.Args…, extraArgs…]
```

Appending at the very end is deliberate: it places the pass-through args after
any `--` already embedded in `agent.Cmd`, so for a jailing wrapper like
`ai-jail -- claude`, the extra args reach the real agent (`claude`) rather than
the wrapper.

Each token is passed as a distinct `argv` element exactly as received — no
quoting, splitting, or shell interpretation.

## Argument parsing (`--` convention)

Verified against cobra v1.10.2 / pflag v1.0.10. When pflag encounters a bare
`--`, it records `ArgsLenAtDash()` = the number of positional args seen before
the dash, consumes the `--` (it does not appear in `args`), and appends every
following token to `args` verbatim without flag-parsing them.

Resulting cases:

| Command | `args` | `ArgsLenAtDash()` |
|---|---|---|
| `ch run claude -- --model opus` | `[claude, --model, opus]` | `1` |
| `ch run claude --` | `[claude]` | `1` |
| `ch run claude foo` | `[claude, foo]` | `-1` |
| `ch run claude --model x` | (parse error: unknown flag) | — |

Because tokens after `--` are not flag-parsed, `--model opus` passes through
cleanly. Without `--`, cobra treats `--model` as a flag on `ch run` and errors
with `unknown flag` — this is intended; users must use `--`.

## Validation (strict)

`cobra.ExactArgs(1)` is replaced with a custom validator that enforces exactly
one positional (the agent name) and requires the `--` convention for extras:

```go
Args: func(cmd *cobra.Command, args []string) error {
    dash := cmd.ArgsLenAtDash()
    if dash == -1 {
        if len(args) != 1 {
            return fmt.Errorf("accepts 1 arg (the agent name); pass extra args after --")
        }
        return nil
    }
    if dash != 1 {
        return fmt.Errorf("expected exactly one agent name before --")
    }
    return nil
}
```

Rejected (strict): `ch run` (none), `ch run claude foo` (dash=-1, len=2),
`ch run -- x` (dash=0), `ch run claude foo -- bar` (dash=2).
Accepted: `ch run claude`, `ch run claude --`, `ch run claude -- <args…>`.

## Code changes

All in `cmd/run.go`:

- `Use: "run <agent> [-- <args>]"`; document pass-through in `Long`.
- Replace `Args: cobra.ExactArgs(1)` with the strict validator above.
- In `Run`: `agentName := args[0]`; `extraArgs := args[1:]`; append `extraArgs`
  to `execArgs` after the existing `agent.Args` append.

No changes to `internal/config`, `internal/session`, or `internal/tui`.

## Documentation (required)

The `ch run <agent> -- <args>` usage must be documented in two places:

1. **Command help** (`cmd/run.go`): the `Use` string is
   `run <agent> [-- <args>]`, and the `Long` description explicitly explains
   that arguments after `--` are forwarded to the agent command verbatim,
   appended after the agent's configured args. This surfaces in
   `ch run --help` / `ch help run`.

2. **README** (`README.md`):
   - Add a `ch run <agent> [-- <args>]` row to the Commands table
     (`## Commands`) describing that it runs a registered agent in the current
     shell and forwards args after `--` to it.
   - Add a sentence to the Agents section (`### Agents`) noting that
     `ch run <agent> -- <args>` passes extra arguments straight through to the
     agent, e.g. `ch run claude -- --model opus`.

## Testing

`cmd/run_internal_test.go`:

- Extra args are appended after `agent.Args` in `execArgs`.
- Extra args land after an embedded `--` in `agent.Cmd`
  (`ai-jail -- claude … <args>`).
- Empty pass-through (`ch run claude` and `ch run claude --`) yields the same
  `execArgs` as today.
- Validator rejects a second bare positional (`ch run claude foo`).
- Validator rejects zero args and a positional before a stray `--` position.

Coverage must stay ≥ 80% (`make check`).
