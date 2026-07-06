# `ch run <agent> -- <args>` Pass-Through Arguments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `ch run <agent> -- <args>` forward extra arguments verbatim to the resolved agent command.

**Architecture:** `ch run` resolves a registered agent and `syscall.Exec`s it, building `execArgs = [binary, cmdPrefixArgs…, agent.Args…]`. This change appends the tokens after `--` to the end of that slice and replaces the `cobra.ExactArgs(1)` validator with a strict validator that uses cobra's `ArgsLenAtDash()` to enforce the `--` convention. Everything lives in `cmd/run.go`; docs are updated in the command help and README.

**Tech Stack:** Go, cobra v1.10.2 / pflag v1.0.10.

## Global Constraints

- New code must keep aggregate test coverage ≥ 80% (`make check`).
- Scope is `ch run` only — do not touch `internal/config`, `internal/session`, or `internal/tui`.
- Pass-through args are forwarded verbatim: no quoting, splitting, or shell interpretation. Each token is one `argv` element.
- Pass-through args are appended *after* `agent.Args` (so they land after any `--` embedded in `agent.Cmd`, e.g. `ai-jail -- claude … <args>`).
- Strict validation: exactly one positional (the agent name) before any `--`; reject a second bare positional without `--`.

---

### Task 1: Pass-through args + strict validator in `ch run`

**Files:**
- Modify: `cmd/run.go` (the `Cobra()` method and `Run` method)
- Test: `cmd/run_internal_test.go`

**Interfaces:**
- Consumes: existing `RunAgentCmd`, package globals `lookPath`, `syscallExec`, `cfg`, and test helper `setTestConfig(t, *config.Config)`.
- Produces: no new exported symbols. Observable behavior — `ch run <agent> -- <args>` appends `<args>` to the exec `argv`; strict arg validation rejects extra bare positionals.

**Cobra `--` semantics (verified against source):** when pflag hits a bare `--`, it sets `argsLenAtDash = len(positionals so far)`, consumes the `--` (it is not in `args`), and appends every following token to `args` without flag-parsing them. `cmd.ArgsLenAtDash()` returns that count, or `-1` if no `--` was present. So for `ch run claude -- --model opus`, RunE receives `args = ["claude","--model","opus"]` and `ArgsLenAtDash() == 1`.

- [ ] **Step 1: Write the failing tests**

Add these tests to `cmd/run_internal_test.go` (imports `io`, `slices`, `strings`, `config` are already present):

```go
func TestRunAgentCmd_passThroughArgs_appendedAfterAgentArgs(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {Cmd: "mybin", Args: []string{"--flag", "val"}},
		},
	})

	var execArgs []string
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(_ string, args []string, _ []string) error {
		execArgs = args
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{"my-agent", "--", "--model", "opus"})

	if err := cobraCmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	wantArgs := []string{"mybin", "--flag", "val", "--model", "opus"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Errorf("args = %v, want %v", execArgs, wantArgs)
	}
}

func TestRunAgentCmd_passThroughArgs_landAfterEmbeddedDash(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {Cmd: "ai-jail -- claude", Args: []string{"--project", "foo"}},
		},
	})

	var execArgs []string
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(_ string, args []string, _ []string) error {
		execArgs = args
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{"my-agent", "--", "-p", "trust"})

	if err := cobraCmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	wantArgs := []string{"ai-jail", "--", "claude", "--project", "foo", "-p", "trust"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Errorf("args = %v, want %v", execArgs, wantArgs)
	}
}

func TestRunAgentCmd_emptyPassThrough_unchanged(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {Cmd: "mybin", Args: []string{"--flag", "val"}},
		},
	})

	var execArgs []string
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(_ string, args []string, _ []string) error {
		execArgs = args
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{"my-agent", "--"})

	if err := cobraCmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	wantArgs := []string{"mybin", "--flag", "val"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Errorf("args = %v, want %v", execArgs, wantArgs)
	}
}

func TestRunAgentCmd_rejectsBarePositionalWithoutDash(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{"my-agent": {Cmd: "mybin"}},
	})

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(string, []string, []string) error {
		t.Fatal("syscallExec should not be called when validation fails")
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{"my-agent", "foo"})

	if err := cobraCmd.Execute(); err == nil {
		t.Fatal("expected error for a second bare positional without --")
	}
}

func TestRunAgentCmd_rejectsZeroArgs(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{"my-agent": {Cmd: "mybin"}},
	})

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(string, []string, []string) error {
		t.Fatal("syscallExec should not be called when validation fails")
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{})

	if err := cobraCmd.Execute(); err == nil {
		t.Fatal("expected error when no agent name is given")
	}
}

func TestRunAgentCmd_rejectsTwoPositionalsBeforeDash(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{"my-agent": {Cmd: "mybin"}},
	})

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(string, []string, []string) error {
		t.Fatal("syscallExec should not be called when validation fails")
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)
	cobraCmd.SetErr(io.Discard)
	cobraCmd.SetArgs([]string{"my-agent", "extra", "--", "x"})

	if err := cobraCmd.Execute(); err == nil {
		t.Fatal("expected error for two positionals before --")
	}
}
```

- [ ] **Step 2: Run the tests to verify the new behavior fails**

Run: `go test ./cmd/ -run 'TestRunAgentCmd_(passThroughArgs|emptyPassThrough|rejects)' -v`

Expected: FAIL on `TestRunAgentCmd_passThroughArgs_appendedAfterAgentArgs` and `TestRunAgentCmd_passThroughArgs_landAfterEmbeddedDash`. Under the current `cobra.ExactArgs(1)`, running `my-agent -- --model opus` leaves a 3-element post-dash `args` slice, so `ExactArgs(1)` makes `Execute()` return an error and the tests hit their `t.Fatalf("Execute() = ...")`. That is the failure proving the feature is missing.

Note: the `rejects*` tests and `emptyPassThrough` may already pass under `ExactArgs(1)` (it also rejects zero/extra args, and the empty-pass-through case exec's identically today). They are regression guards that must keep passing after the change — they are not expected to fail here. Do not proceed until you see the two `passThroughArgs` tests failing.

- [ ] **Step 3: Replace the validator in `cmd/run.go`**

In the `Cobra()` method, change the `Use` string and replace `Args: cobra.ExactArgs(1)` with the strict validator. Update `Long` to document pass-through.

Replace this block:

```go
	return &cobra.Command{
		Use:   "run <agent>",
		Short: "Run a registered agent in the current shell",
		Long:  "Resolves a registered agent from config and replaces the current process with it. The agent inherits the current environment with its configured env vars overlaid.",
		Args:  cobra.ExactArgs(1),
		RunE:  c.Run,
```

with:

```go
	return &cobra.Command{
		Use:   "run <agent> [-- <args>]",
		Short: "Run a registered agent in the current shell",
		Long: "Resolves a registered agent from config and replaces the current process with it. " +
			"The agent inherits the current environment with its configured env vars overlaid.\n\n" +
			"Arguments after -- are forwarded to the agent command verbatim, appended after the " +
			"agent's configured args. Example: ch run claude -- --model opus",
		Args: func(cmd *cobra.Command, args []string) error {
			if dash := cmd.ArgsLenAtDash(); dash == -1 {
				if len(args) != 1 {
					return fmt.Errorf("accepts exactly one agent name; pass extra args after --")
				}
			} else if dash != 1 {
				return fmt.Errorf("accepts exactly one agent name before --; pass extra args after --")
			}
			return nil
		},
		RunE: c.Run,
```

- [ ] **Step 4: Append pass-through args in `Run`**

In the `Run` method of `cmd/run.go`, find:

```go
	execArgs := append([]string{binaryName}, cmdPrefixArgs...)
	execArgs = append(execArgs, agent.Args...)
	mergedEnv := mergeEnv(os.Environ(), agent.Env)
```

Change it to append the tokens after the agent name (`args[1:]`):

```go
	execArgs := append([]string{binaryName}, cmdPrefixArgs...)
	execArgs = append(execArgs, agent.Args...)
	execArgs = append(execArgs, args[1:]...)
	mergedEnv := mergeEnv(os.Environ(), agent.Env)
```

(`args[0]` is `agentName`, already read at the top of `Run`; `args[1:]` is the pass-through slice, empty when no `--` args were given.)

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./cmd/ -run 'TestRunAgentCmd_(passThroughArgs|emptyPassThrough|rejects)' -v`
Expected: PASS (all six new tests).

- [ ] **Step 6: Run the full cmd package tests to check nothing regressed**

Run: `go test ./cmd/ -run TestRunAgentCmd -v`
Expected: PASS, including the pre-existing `TestRunAgentCmd_runKnownAgent_callsExec`, `TestRunAgentCmd_cmdWithSpaces_parsesCorrectly`, etc. (those call `c.Run` directly with a single-element `args`, so `args[1:]` is empty and their expectations are unchanged).

- [ ] **Step 7: Commit**

```bash
git add cmd/run.go cmd/run_internal_test.go
git commit -m "feat: forward args after -- to the agent in ch run

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Document pass-through in the README

**Files:**
- Modify: `README.md` (the `### Agents` section and the `## Commands` table)

**Interfaces:**
- Consumes: nothing (docs only).
- Produces: user-facing documentation of `ch run <agent> -- <args>`.

- [ ] **Step 1: Add a note to the Agents section**

In `README.md`, find the Agents paragraph:

```markdown
### Agents

Agents are CLI tools configured once and selected at session start. Any command-line tool works -- Claude Code, Aider, Codex, or a custom script. Each agent defines a command, optional arguments, and optional environment variables.
```

Add a second paragraph directly after it:

```markdown
Run an agent in the current shell with `ch run <agent>`. Arguments after `--` are forwarded to the agent's command verbatim, appended after its configured args -- for example `ch run claude -- --model opus`.
```

- [ ] **Step 2: Add a Commands table row**

In `README.md`, in the `## Commands` table, add a row immediately after the `ch delete session` row and before the `ch version` row:

```markdown
| `ch run <agent> [-- <args>]` | Run a registered agent in the current shell; args after `--` are forwarded to it |
```

- [ ] **Step 3: Verify the README renders as intended**

Run: `git diff README.md`
Expected: the new Agents paragraph and the new table row appear, with no unintended reflow of surrounding lines.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document ch run pass-through args in README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Full verification gate

**Files:** none (verification only).

- [ ] **Step 1: Run the project's full check**

Run: `make check`
Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds.

- [ ] **Step 2: Manual smoke check of the help text**

Run: `make build && ./ch run --help`
Expected: usage line shows `ch run <agent> [-- <args>]` and the long description mentions forwarding args after `--` (e.g. the `ch run claude -- --model opus` example).

- [ ] **Step 3: Manual smoke check of validation (optional, requires a configured agent)**

Run: `./ch run some-agent extra` (no `--`)
Expected: an error mentioning that extra args must be passed after `--`; the agent is not exec'd.
