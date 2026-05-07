# Foreground Agent Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `ch run <agent>` command that executes a registered agent via `syscall.Exec` in the current shell, inheriting the current environment with agent-configured env vars overlaid.

**Architecture:** A new root-level `run` verb with a single `RunAgentCmd` struct. The command resolves the agent from config, merges `os.Environ()` with the agent's `Env` map, and replaces the current process with `syscall.Exec`. Env merging is a pure function. The `syscall.Exec` call is mockable via a package-level var for testing.

**Tech Stack:** Go, Cobra, standard library (`os`, `syscall`, `strings`).

---

## File Structure

| File | Responsibility |
|---|---|
| `cmd/run.go` | New. `RunAgentCmd` struct with `Cobra()` and `Run()`. Also contains `mergeEnv()` helper. |
| `cmd/run_internal_test.go` | New. Unit tests for `mergeEnv` and `RunAgentCmd.Run` with mocked `syscallExec`. |
| `cmd/run_test.go` | New. External integration test (package `cmd_test`) wiring the command through Cobra. |
| `cmd/register.go` | Modify. Wire `(&RunAgentCmd{}).Cobra()` into the root command tree. |

---

### Task 1: Implement `mergeEnv` pure function with TDD

**Files:**
- Create: `cmd/run.go`
- Test: `cmd/run_internal_test.go`

- [ ] **Step 1: Write the failing test**

```go
package cmd

import (
	"slices"
	"testing"
)

func TestMergeEnv_overridesExisting(t *testing.T) {
	current := []string{"FOO=old", "BAR=keep"}
	overrides := map[string]string{"FOO": "new"}
	got := mergeEnv(current, overrides)
	want := []string{"FOO=new", "BAR=keep"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %v, want %v", got, want)
	}
}

func TestMergeEnv_addsNewKeys(t *testing.T) {
	current := []string{"FOO=bar"}
	overrides := map[string]string{"BAZ": "qux"}
	got := mergeEnv(current, overrides)
	want := []string{"FOO=bar", "BAZ=qux"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %v, want %v", got, want)
	}
}

func TestMergeEnv_emptyOverrides(t *testing.T) {
	current := []string{"FOO=bar"}
	overrides := map[string]string{}
	got := mergeEnv(current, overrides)
	want := []string{"FOO=bar"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %v, want %v", got, want)
	}
}

func TestMergeEnv_emptyCurrent(t *testing.T) {
	current := []string{}
	overrides := map[string]string{"FOO": "bar"}
	got := mergeEnv(current, overrides)
	want := []string{"FOO=bar"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestMergeEnv -v`

Expected: FAIL with `mergeEnv not defined` or similar compile error.

- [ ] **Step 3: Write minimal implementation**

In `cmd/run.go`, add:

```go
package cmd

import "strings"

func mergeEnv(current []string, overrides map[string]string) []string {
	result := make([]string, len(current))
	copy(result, current)

	existing := make(map[string]int, len(result))
	for i, entry := range result {
		if idx := strings.Index(entry, "="); idx >= 0 {
			existing[entry[:idx]] = i
		}
	}

	for key, val := range overrides {
		if i, ok := existing[key]; ok {
			result[i] = key + "=" + val
		} else {
			result = append(result, key+"="+val)
		}
	}

	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestMergeEnv -v`

Expected: PASS for all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/run.go cmd/run_internal_test.go
git commit -m "feat: add mergeEnv helper for agent env overlay"
```

---

### Task 2: Implement `RunAgentCmd` with TDD

**Files:**
- Modify: `cmd/run.go`
- Test: `cmd/run_internal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/run_internal_test.go`:

```go
func TestRunAgentCmd_runKnownAgent_callsExec(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {
				Cmd:  "mybin",
				Args: []string{"--flag", "val"},
				Env:  map[string]string{"AGENT_VAR": "agent_value"},
			},
		},
	})

	var execCalled bool
	var execCmd string
	var execArgs []string
	var execEnv []string

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(cmd string, args []string, env []string) error {
		execCalled = true
		execCmd = cmd
		execArgs = args
		execEnv = env
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)

	err := c.Run(cobraCmd, []string{"my-agent"})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if !execCalled {
		t.Fatal("syscallExec not called")
	}
	if execCmd != "mybin" {
		t.Errorf("cmd = %q, want mybin", execCmd)
	}
	wantArgs := []string{"mybin", "--flag", "val"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Errorf("args = %v, want %v", execArgs, wantArgs)
	}

	// Verify env merging: AGENT_VAR should be present
	var foundAgentVar bool
	for _, e := range execEnv {
		if e == "AGENT_VAR=agent_value" {
			foundAgentVar = true
			break
		}
	}
	if !foundAgentVar {
		t.Errorf("env does not contain AGENT_VAR=agent_value: %v", execEnv)
	}
}

func TestRunAgentCmd_unknownAgent_returnsError(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{},
	})

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(string, []string, []string) error {
		t.Fatal("syscallExec should not be called for unknown agent")
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)

	err := c.Run(cobraCmd, []string{"unknown-agent"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}
```

Add imports at top of `cmd/run_internal_test.go`:
```go
import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestRunAgentCmd -v`

Expected: FAIL with `RunAgentCmd not defined` or `syscallExec not defined` compile error.

- [ ] **Step 3: Write minimal implementation**

Append to `cmd/run.go`:

```go
import (
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
)

var syscallExec = syscall.Exec

type RunAgentCmd struct{}

func (c *RunAgentCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:   "run <agent>",
		Short: "Run a registered agent in the current shell",
		Args:  cobra.ExactArgs(1),
		RunE:  c.Run,
	}
}

func (c *RunAgentCmd) Run(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	agent, err := cfg.AgentByName(agentName)
	if err != nil {
		return err
	}

	execArgs := append([]string{agent.Cmd}, agent.Args...)
	mergedEnv := mergeEnv(os.Environ(), agent.Env)

	if err := syscallExec(agent.Cmd, execArgs, mergedEnv); err != nil {
		return fmt.Errorf("exec agent: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestRunAgentCmd -v`

Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/run.go cmd/run_internal_test.go
git commit -m "feat: add ch run command for foreground agent execution"
```

---

### Task 3: Wire `run` into command tree + external test

**Files:**
- Modify: `cmd/register.go`
- Create: `cmd/run_test.go`

- [ ] **Step 1: Add run command to register.go**

In `cmd/register.go`, add after the existing command groups:

```go
	runCmd := &RunAgentCmd{}
	root.AddCommand(runCmd.Cobra())
```

Place it after `attachCmd` addition, before `root.AddCommand((&TemplateCmd{}).Cobra())`.

- [ ] **Step 2: Write external integration test**

Create `cmd/run_test.go`:

```go
package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRunConfig(t *testing.T) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[agents.my-agent]
cmd = "echo"
args = ["hello"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestRun_tooFewArgs(t *testing.T) {
	cfgPath := writeRunConfig(t)
	if err := runCmd(t, "--config", cfgPath, "run"); err == nil {
		t.Error("expected error for missing agent argument")
	}
}

func TestRun_tooManyArgs(t *testing.T) {
	cfgPath := writeRunConfig(t)
	if err := runCmd(t, "--config", cfgPath, "run", "a", "b"); err == nil {
		t.Error("expected error for too many arguments")
	}
}

func TestRun_unknownAgent(t *testing.T) {
	cfgPath := writeRunConfig(t)
	err := runCmd(t, "--config", cfgPath, "run", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}
```

- [ ] **Step 3: Run all new tests**

Run: `go test ./cmd/ -run TestRun -v`

Expected: PASS for all tests.

- [ ] **Step 4: Run full unit test suite**

Run: `go test ./...`

Expected: PASS (no regressions).

- [ ] **Step 5: Commit**

```bash
git add cmd/register.go cmd/run_test.go
git commit -m "feat: wire run command into CLI and add integration tests"
```

---

### Task 4: Verify coverage and build

**Files:** None (verification only).

- [ ] **Step 1: Run coverage check**

Run: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1`

Expected: Coverage at or above 80%.

- [ ] **Step 2: Run lint**

Run: `make lint` (or `golangci-lint run ./...`)

Expected: No new lint errors.

- [ ] **Step 3: Run build**

Run: `make build`

Expected: Binary builds successfully.

- [ ] **Step 4: Final commit (if any fixes needed)**

If coverage, lint, or build required fixes, commit them. Otherwise, no additional commit needed.

---

## Self-Review

**1. Spec coverage:**

| Requirement | Task |
|---|---|
| `ch run <agent-name>` resolves agent from config | Task 2 |
| Runs in current shell (no tmux, no worktree) | Task 2 (syscall.Exec) |
| Inherits `os.Environ()` with agent env overlaid | Task 1 (mergeEnv), Task 2 |
| No `CODEHERD_*` env vars injected | Task 2 (only merges agent.Env) |
| `syscall.Exec` replaces ch process | Task 2 |
| Unknown agent → error, no exec | Task 2 (TestRunAgentCmd_unknownAgent) |
| Binary not found → exec fails naturally | Handled by syscall.Exec error return |

All requirements covered. No gaps.

**2. Placeholder scan:** No TBD, TODO, "implement later", or vague descriptions found. Every step contains complete code or exact commands.

**3. Type consistency:** `mergeEnv` signature `([]string, map[string]string) []string` is used consistently. `syscallExec` is defined as `var syscallExec = syscall.Exec` and mocked with matching signature. `RunAgentCmd` follows the established struct-per-command pattern with `Cobra()` and `Run()` methods.
