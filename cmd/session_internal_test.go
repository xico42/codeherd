package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// setTestConfig sets the package-level cfg for the duration of the test.
func setTestConfig(t *testing.T, c *config.Config) {
	t.Helper()
	orig := cfg
	cfg = c
	t.Cleanup(func() { cfg = orig })
}

// setHerdTmux points the package-level Herd at the given tmux runner and keeps
// the package cfg in sync (production builds both from one config load).
// Session commands go through h; there is no service seam to override any more.
func setHerdTmux(t *testing.T, c *config.Config, r tmux.Runner) {
	t.Helper()
	setTestConfig(t, c)
	orig := h
	h = herd.New(c, registry, herd.Deps{Tmux: r})
	t.Cleanup(func() { h = orig })
}

// sessionLine builds a tmux list-sessions record in the 10-field tab format
// ListSessions parses: id, name, canonical, type, status, annotation,
// started_at, profile, branch, project. The branch and project fields are what
// let h.Resolve rebuild a matching Ref.
func sessionLine(id, project, branch, stype, status string) string {
	canonical := semconv.SessionName("", project, branch)
	return strings.Join([]string{
		id, canonical, canonical, stype, status, "", "", "", branch, project,
	}, "\t") + "\n"
}

func TestSessionTypeFromFlag(t *testing.T) {
	if got := sessionTypeFromFlag(true); got != herd.SessionTypeShell {
		t.Errorf("sessionTypeFromFlag(true) = %q, want %q", got, herd.SessionTypeShell)
	}
	if got := sessionTypeFromFlag(false); got != herd.SessionTypeAgent {
		t.Errorf("sessionTypeFromFlag(false) = %q, want %q", got, herd.SessionTypeAgent)
	}
}

// failWriter is an io.Writer that always returns an error, used to test
// flush-error paths in tabwriter.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write failed")
}

// TestListSession_flushError verifies that ListSessionCmd.Run surfaces a
// tabwriter flush error when the output writer fails.
func TestListSession_flushError(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: sessionLine("$1", "myapp", "feat", "agent", "running"), exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	c := &ListSessionCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(failWriter{})

	err := c.Run(cobraCmd, nil)
	if err == nil {
		t.Error("expected flush error, got nil")
	}
}

// TestShowSession_flushError verifies that ShowSessionCmd.Run surfaces a
// tabwriter flush error when the output writer fails.
func TestShowSession_flushError(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: sessionLine("$1", "myapp", "feat", "agent", "running"), exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	c := &ShowSessionCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(failWriter{})

	err := c.Run(cobraCmd, []string{"myapp", "feat"})
	if err == nil {
		t.Error("expected flush error, got nil")
	}
}

// TestShowSession_outputsSessionInfo verifies that ShowSessionCmd.Run prints
// session details to stdout when the session exists.
func TestShowSession_outputsSessionInfo(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: sessionLine("$42", "myapp", "feat", "agent", "running"), exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	c := &ShowSessionCmd{}
	cobraCmd := c.Cobra()
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	if err := c.Run(cobraCmd, []string{"myapp", "feat"}); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	got := out.String()
	if !strings.Contains(got, "myapp-feat") {
		t.Errorf("output %q does not contain session name", got)
	}
	if !strings.Contains(got, "agent") {
		t.Errorf("output %q does not contain session type", got)
	}
	if !strings.Contains(got, "running") {
		t.Errorf("output %q does not contain status", got)
	}
}

// TestDeleteSession_promptAbort verifies that DeleteSessionCmd.Run aborts
// when the user answers "n" to the running-session confirmation prompt.
func TestDeleteSession_promptAbort(t *testing.T) {
	// Return a running session so the prompt fires.
	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: sessionLine("$10", "myapp", "feat", "agent", "running"), exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	c := &DeleteSessionCmd{}
	cobraCmd := c.Cobra()
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	// Pipe "n\n" to stdin so the prompt receives a "no" answer.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(pw, "n")
	pw.Close()
	cobraCmd.SetIn(pr)

	if err := c.Run(cobraCmd, []string{"myapp", "feat"}); err != nil {
		t.Fatalf("Run() = %v, want nil (aborted)", err)
	}

	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("output %q does not contain Aborted", out.String())
	}
}

// TestDeleteSession_promptConfirm_callsStop verifies that DeleteSessionCmd.Run
// proceeds to StopSessions when the user confirms with "y", killing the session
// by its stable tmux ID.
func TestDeleteSession_promptConfirm_callsStop(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			// list-sessions for h.Resolve (confirmation probe)
			{stdout: sessionLine("$10", "myapp", "feat", "agent", "running"), exitCode: 0},
			// list-sessions for h.StopSessions
			{stdout: sessionLine("$10", "myapp", "feat", "agent", "running"), exitCode: 0},
			// kill-session
			{stdout: "", exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	c := &DeleteSessionCmd{}
	cobraCmd := c.Cobra()
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(pw, "y")
	pw.Close()
	cobraCmd.SetIn(pr)

	if err := c.Run(cobraCmd, []string{"myapp", "feat"}); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if !strings.Contains(out.String(), "done") {
		t.Errorf("output %q does not contain done", out.String())
	}

	// Verify kill-session was called by stable ID ($10).
	var killedByID bool
	for _, call := range r.calls {
		if call[0] == "kill-session" && call[len(call)-1] == "$10" {
			killedByID = true
		}
	}
	if !killedByID {
		t.Error("expected kill-session -t $10 (by stable ID), not found")
	}
}

// TestAttachSession_callsExecTmuxAttach verifies that AttachSessionCmd.Run
// calls execTmuxAttach with the session ID from Resolve.
func TestAttachSession_callsExecTmuxAttach(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: sessionLine("$42", "myapp", "feat", "agent", "running"), exitCode: 0},
		},
	}
	setHerdTmux(t, &config.Config{}, r)

	var attachedTo string
	origExec := execTmuxAttach
	t.Cleanup(func() { execTmuxAttach = origExec })
	execTmuxAttach = func(name string) error {
		attachedTo = name
		return nil
	}

	c := &AttachSessionCmd{}
	cobraCmd := c.Cobra()
	cobraCmd.SetOut(os.Stdout)

	if err := c.Run(cobraCmd, []string{"myapp", "feat"}); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if attachedTo != "$42" {
		t.Errorf("execTmuxAttach called with %q, want $42", attachedTo)
	}
}

// TestCreateSession_autoCreate_worktreeNotFound verifies that CreateSessionCmd
// tries to create the worktree via EnsureWorkspace when it does not exist. The
// real git runner will fail (not a git repo), which produces a non-sentinel
// error that flows through worktreeErr's default branch and is returned (no
// os.Exit). The "not found, creating...  done" banner now prints only on the
// successful create path, so this failure case surfaces the git error instead.
func TestCreateSession_autoCreate_worktreeNotFound(t *testing.T) {
	// cloneDir exists (project is "cloned") but worktreePath does not.
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// worktreePath intentionally not created

	setTestConfig(t, &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir, Agent: "echo-agent"},
		Agents:   map[string]config.AgentConfig{"echo-agent": {Cmd: "echo"}},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	})

	c := &CreateSessionCmd{}
	cobraCmd := c.Cobra()
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	err := c.Run(cobraCmd, []string{"myapp", "feat"})
	// Expect an error from git (not a real git repo), not from os.Exit.
	if err == nil {
		t.Fatal("expected error from git worktree creation, got nil")
	}
}

// TestCreateSession_agent_noDefaultAgent exercises the agent branch to verify
// agent-resolution errors surface. With the worktree already present, the
// command reaches h.Launch, whose agent resolution fails when neither --agent
// nor defaults.agent is set.
func TestCreateSession_agent_noDefaultAgent(t *testing.T) {
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	worktreePath := filepath.Join(cloneDir+"__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir}, // no default agent
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	setHerdTmux(t, cfg, &multiMockRunner{}) // empty runner: list-sessions returns no sessions

	c := &CreateSessionCmd{} // Shell=false, no agent set
	cobraCmd := c.Cobra()
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	err := c.Run(cobraCmd, []string{"myapp", "feat"})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	if !strings.Contains(err.Error(), "no agent specified") {
		t.Errorf("error = %q, want 'no agent specified'", err.Error())
	}
}

// TestCreateSession_shell_existingWorktree verifies the shell path proceeds
// past the worktree check and reaches the session start phase against a real
// (isolated) tmux server.
func TestCreateSession_shell_existingWorktree(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	// Isolate tmux to a per-test socket so the test's transient session does
	// not appear on the developer's outer tmux server.
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	t.Setenv("TMUX", "")
	probe := exec.Command("tmux", "-S", socket, "new-session", "-d", "-s", "__probe__", "sleep", "30")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux daemonize unavailable: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
	})
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	worktreePath := cloneDir + "__worktrees" + string(os.PathSeparator) + "feat"
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	setHerdTmux(t, cfg, tmux.NewRealRunner())

	origExec := execTmuxAttach
	t.Cleanup(func() { execTmuxAttach = origExec })
	execTmuxAttach = func(string) error { return nil }

	c := &CreateSessionCmd{}
	cobraCmd := c.Cobra()
	// Set Shell=true AFTER Cobra() to avoid BoolVar overwriting the field with its default.
	c.Shell = true
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	runErr := c.Run(cobraCmd, []string{"myapp", "feat"})

	if !strings.Contains(out.String(), "Starting session myapp-feat~sh") {
		t.Errorf("output %q does not show shell session name (runErr=%v)", out.String(), runErr)
	}
}
