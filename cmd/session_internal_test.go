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
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
)

// setTestConfig sets the package-level cfg for the duration of the test.
func setTestConfig(t *testing.T, c *config.Config) {
	t.Helper()
	orig := cfg
	cfg = c
	t.Cleanup(func() { cfg = orig })
}

func TestResolveAgentName_flagTakesPrecedence(t *testing.T) {
	setTestConfig(t, &config.Config{
		Defaults: config.DefaultsConfig{Agent: "default-agent"},
		Agents: map[string]config.AgentConfig{
			"default-agent": {Cmd: "default"},
			"flag-agent":    {Cmd: "flag"},
		},
	})
	name, err := resolveAgentName("flag-agent")
	if err != nil {
		t.Fatal(err)
	}
	if name != "flag-agent" {
		t.Errorf("resolveAgentName = %q, want flag-agent", name)
	}
}

func TestResolveAgentName_fallsBackToDefault(t *testing.T) {
	setTestConfig(t, &config.Config{
		Defaults: config.DefaultsConfig{Agent: "my-default"},
		Agents: map[string]config.AgentConfig{
			"my-default": {Cmd: "claude"},
		},
	})
	name, err := resolveAgentName("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-default" {
		t.Errorf("resolveAgentName = %q, want my-default", name)
	}
}

func TestResolveAgentName_errorWhenNoneSet(t *testing.T) {
	setTestConfig(t, &config.Config{})
	_, err := resolveAgentName("")
	if err == nil {
		t.Error("resolveAgentName should error when no agent specified and no default")
	}
}

func TestSessionTypeFromFlag(t *testing.T) {
	if got := sessionTypeFromFlag(true); got != semconv.SessionTypeShell {
		t.Errorf("sessionTypeFromFlag(true) = %q, want %q", got, semconv.SessionTypeShell)
	}
	if got := sessionTypeFromFlag(false); got != semconv.SessionTypeAgent {
		t.Errorf("sessionTypeFromFlag(false) = %q, want %q", got, semconv.SessionTypeAgent)
	}
}

// listSessionsResponse formats a fake tmux list-sessions output that the session
// service can parse. Fields: session_id, name, canonical_name, session_type,
// status, annotation, started_at.
func listSessionsResponse(id, name, canonical, stype, status string) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t\t\n", id, name, canonical, stype, status)
}

// newTestSessionService creates a session.Service backed by the given mock runner.
func newTestSessionService(r tmux.Runner) *session.Service {
	return session.NewService(tmux.NewClient(r), &hooks.NoOp{})
}

// overrideSessionService replaces newSessionService for the duration of the test.
func overrideSessionService(t *testing.T, svc *session.Service) {
	t.Helper()
	orig := newSessionService
	newSessionService = func() *session.Service { return svc }
	t.Cleanup(func() { newSessionService = orig })
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
			{stdout: listSessionsResponse("$1", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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
			{stdout: listSessionsResponse("$1", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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
			// list-sessions response for svc.Show
			{stdout: listSessionsResponse("$42", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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
			// list-sessions for svc.Show (inside !Force branch)
			{stdout: listSessionsResponse("$10", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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

// TestDeleteSession_promptConfirm_stop verifies that DeleteSessionCmd.Run
// proceeds to svc.Stop when the user confirms with "y". It expects Stop to
// return ErrSessionNotFound (because list-sessions is called again in Stop),
// so we supply a second list-sessions response.
func TestDeleteSession_promptConfirm_callsStop(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			// list-sessions for svc.Show
			{stdout: listSessionsResponse("$10", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
			// list-sessions for svc.Stop
			{stdout: listSessionsResponse("$10", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
			// kill-session
			{stdout: "", exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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

	// Verify kill-session was called.
	var killCalled bool
	for _, call := range r.calls {
		if call[0] == "kill-session" {
			killCalled = true
		}
	}
	if !killCalled {
		t.Error("expected kill-session call, not found")
	}
}

// TestAttachSession_callsExecTmuxAttach verifies that AttachSessionCmd.Run
// calls execTmuxAttach with the session ID from Show.
func TestAttachSession_callsExecTmuxAttach(t *testing.T) {
	r := &multiMockRunner{
		responses: []mockResponse{
			// list-sessions for svc.Show — returns session with ID $42
			{stdout: listSessionsResponse("$42", "myapp-feat", "myapp-feat", "agent", "running"), exitCode: 0},
		},
	}
	svc := newTestSessionService(r)
	overrideSessionService(t, svc)

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
// prints "Worktree ... not found, creating..." and calls wtSvc.New when the
// worktree directory does not exist. The real git runner will fail (not a
// git repo), which produces a non-sentinel error that flows through
// worktreeErr's default branch and is returned (no os.Exit).
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
	// The error should NOT be nil since the worktree creation fails.
	if err == nil {
		t.Fatal("expected error from git worktree creation, got nil")
	}

	// The "creating..." message should appear in output.
	if !strings.Contains(out.String(), "not found, creating") {
		t.Errorf("output %q does not contain 'not found, creating'", out.String())
	}
}

// TestCreateSession_agentPath_missingWorktree exercises the non-shell branch
// to verify agent resolution errors are surfaced cleanly.
func TestCreateSession_agent_noDefaultAgent(t *testing.T) {
	setTestConfig(t, &config.Config{
		Defaults: config.DefaultsConfig{},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	})

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
// past the worktree check and reaches the session start phase. The real tmux
// runner is used; we expect a tmux-level error (session creation) rather than
// a worktree error.
func TestCreateSession_shell_existingWorktree(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	worktreePath := cloneDir + "__worktrees" + string(os.PathSeparator) + "feat"
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	setTestConfig(t, &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	})

	origExec := execTmuxAttach
	t.Cleanup(func() { execTmuxAttach = origExec })
	execTmuxAttach = func(string) error { return nil }

	c := &CreateSessionCmd{}
	cobraCmd := c.Cobra()
	// Set Shell=true AFTER Cobra() to avoid BoolVar overwriting the field with its default.
	c.Shell = true
	var out bytes.Buffer
	cobraCmd.SetOut(&out)

	// The tmux call may succeed (creates a real session) or fail with a
	// non-sentinel error. Either way, we should reach the "Starting session..."
	// print, confirming the worktree and agent-resolution paths were traversed.
	runErr := c.Run(cobraCmd, []string{"myapp", "feat"})
	// Clean up any created tmux session.
	_ = exec.Command("tmux", "kill-session", "-t", "myapp-feat~sh").Run()

	if !strings.Contains(out.String(), "Starting session myapp-feat~sh") {
		t.Errorf("output %q does not show shell session name (runErr=%v)", out.String(), runErr)
	}
}
