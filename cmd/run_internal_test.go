package cmd

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
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

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}

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
	if execCmd != "/usr/bin/mybin" {
		t.Errorf("cmd = %q, want /usr/bin/mybin", execCmd)
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

func TestRunAgentCmd_cmdWithSpaces_parsesCorrectly(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {
				Cmd:  "ai-jail -- claude",
				Args: []string{"--project", "foo"},
				Env:  map[string]string{"AGENT_VAR": "agent_value"},
			},
		},
	})

	var execArgs []string

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) {
		if file != "ai-jail" {
			t.Errorf("lookPath called with %q, want ai-jail", file)
		}
		return "/usr/bin/" + file, nil
	}

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(_ string, args []string, _ []string) error {
		execArgs = args
		return nil
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)

	if err := c.Run(cobraCmd, []string{"my-agent"}); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	wantArgs := []string{"ai-jail", "--", "claude", "--project", "foo"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Errorf("args = %v, want %v", execArgs, wantArgs)
	}
}

func TestRunAgentCmd_execError_returnsWrappedError(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {Cmd: "mybin"},
		},
	})

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(string) (string, error) {
		return "/usr/bin/mybin", nil
	}

	origExec := syscallExec
	t.Cleanup(func() { syscallExec = origExec })
	syscallExec = func(string, []string, []string) error {
		return fmt.Errorf("exec failed")
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)

	err := c.Run(cobraCmd, []string{"my-agent"})
	if err == nil {
		t.Fatal("expected error when exec fails")
	}
	if !strings.Contains(err.Error(), "exec agent") {
		t.Errorf("error = %q, want to contain 'exec agent'", err.Error())
	}
}

func TestRunAgentCmd_lookPathError_returnsWrappedError(t *testing.T) {
	setTestConfig(t, &config.Config{
		Agents: map[string]config.AgentConfig{
			"my-agent": {Cmd: "nonexistent-binary"},
		},
	})

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(string) (string, error) {
		return "", fmt.Errorf("executable not found")
	}

	c := &RunAgentCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(io.Discard)

	err := c.Run(cobraCmd, []string{"my-agent"})
	if err == nil {
		t.Fatal("expected error when lookPath fails")
	}
	if !strings.Contains(err.Error(), "agent command not found") {
		t.Errorf("error = %q, want to contain 'agent command not found'", err.Error())
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
