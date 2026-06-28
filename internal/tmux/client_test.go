package tmux_test

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/tmux"
)

type mockRunner struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	lastArgs []string
}

func (m *mockRunner) Run(args ...string) (string, string, int, error) {
	m.lastArgs = args
	return m.stdout, m.stderr, m.exitCode, m.err
}

func TestClient_HasSession_found(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	got, err := c.HasSession("myapp-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true")
	}
	if r.lastArgs[0] != "has-session" || r.lastArgs[2] != "myapp-feature" {
		t.Errorf("unexpected args: %v", r.lastArgs)
	}
}

func TestClient_HasSession_notFound(t *testing.T) {
	r := &mockRunner{exitCode: 1}
	c := tmux.NewClient(r)
	got, err := c.HasSession("myapp-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for exit code 1")
	}
}

func TestClient_HasSession_execError(t *testing.T) {
	r := &mockRunner{exitCode: -1, err: fmt.Errorf("tmux not found")}
	c := tmux.NewClient(r)
	_, err := c.HasSession("myapp-feature")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_KillSession_ok(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	if err := c.KillSession("myapp-feature"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.lastArgs[0] != "kill-session" || r.lastArgs[2] != "myapp-feature" {
		t.Errorf("unexpected args: %v", r.lastArgs)
	}
}

func TestClient_KillSession_error(t *testing.T) {
	r := &mockRunner{exitCode: 1, stderr: "no such session"}
	c := tmux.NewClient(r)
	if err := c.KillSession("myapp-feature"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_ListSessions_ok(t *testing.T) {
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\tdoing stuff\t2026-01-01T00:00:00Z\tpersonal"
	r := &mockRunner{exitCode: 0, stdout: line + "\n"}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.ID != "$1" {
		t.Errorf("ID = %q, want $1", rec.ID)
	}
	if rec.Name != "myapp-feat" {
		t.Errorf("Name = %q, want myapp-feat", rec.Name)
	}
	if rec.CanonicalName != "myapp-feat" {
		t.Errorf("CanonicalName = %q, want myapp-feat", rec.CanonicalName)
	}
	if rec.SessionType != "agent" {
		t.Errorf("SessionType = %q, want agent", rec.SessionType)
	}
	if rec.Status != "running" {
		t.Errorf("Status = %q, want running", rec.Status)
	}
	if rec.Annotation != "doing stuff" {
		t.Errorf("Annotation = %q, want doing stuff", rec.Annotation)
	}
	if rec.StartedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("StartedAt = %q, want 2026-01-01T00:00:00Z", rec.StartedAt)
	}
	if rec.Profile != "personal" {
		t.Errorf("Profile = %q, want personal", rec.Profile)
	}
}

func TestClient_ListSessions_readsProfileOption(t *testing.T) {
	// 8-field line: profile populated.
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-04-23T00:00:00Z\twork\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Profile != "work" {
		t.Errorf("Profile = %q, want work", records[0].Profile)
	}
}

func TestClient_ListSessions_missingProfileIsEmpty(t *testing.T) {
	// Old 7-field line: profile must default to "" (backward-compatible
	// with existing runs after tmux reports no @codeherd_profile option).
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-04-23T00:00:00Z\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if records[0].Profile != "" {
		t.Errorf("Profile = %q, want empty", records[0].Profile)
	}
}

func TestClient_ListSessions_readsBranchOption(t *testing.T) {
	// 9-field line: branch populated as the final field.
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-01-01T00:00:00Z\twork\tfeature/login\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Branch != "feature/login" {
		t.Errorf("Branch = %q, want feature/login", records[0].Branch)
	}
}

func TestClient_ListSessions_missingBranchIsEmpty(t *testing.T) {
	// Old 8-field line (pre-upgrade session): Branch must default to "".
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-01-01T00:00:00Z\twork\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if records[0].Branch != "" {
		t.Errorf("Branch = %q, want empty", records[0].Branch)
	}
}

func TestClient_ListSessions_prefixedAndShell(t *testing.T) {
	lines := "$2\t⚡ myapp-feat\tmyapp-feat\tagent\twaiting\tneed input\t\n" +
		"$3\tmyapp-feat~sh\tmyapp-feat\tshell\t\t\t\n"
	r := &mockRunner{exitCode: 0, stdout: lines}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len = %d, want 2", len(records))
	}
	if records[0].Name != "⚡ myapp-feat" {
		t.Errorf("records[0].Name = %q", records[0].Name)
	}
	if records[0].CanonicalName != "myapp-feat" {
		t.Errorf("records[0].CanonicalName = %q", records[0].CanonicalName)
	}
	if records[1].SessionType != "shell" {
		t.Errorf("records[1].SessionType = %q, want shell", records[1].SessionType)
	}
}

func TestClient_ListSessions_nonCodeherd(t *testing.T) {
	// Non-codeherd sessions have empty option fields.
	r := &mockRunner{exitCode: 0, stdout: "$4\tother-session\t\t\t\t\t\n"}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len = %d, want 1", len(records))
	}
	if records[0].CanonicalName != "" {
		t.Errorf("CanonicalName = %q, want empty", records[0].CanonicalName)
	}
	if records[0].SessionType != "" {
		t.Errorf("SessionType = %q, want empty", records[0].SessionType)
	}
}

func TestClient_ListSessions_none(t *testing.T) {
	// tmux exits 1 when no sessions — not an error
	r := &mockRunner{exitCode: 1}
	c := tmux.NewClient(r)
	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty, got %v", sessions)
	}
}

func TestClient_NewSession_ok(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	if err := c.NewSession("myapp", "/tmp/myapp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// verify subcommand and key args
	if r.lastArgs[0] != "new-session" {
		t.Errorf("expected new-session, got %v", r.lastArgs)
	}
}

func TestClient_NewSession_error(t *testing.T) {
	r := &mockRunner{exitCode: 1, stderr: "duplicate session"}
	c := tmux.NewClient(r)
	if err := c.NewSession("myapp", "/tmp"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_KillSession_execError(t *testing.T) {
	r := &mockRunner{exitCode: -1, err: fmt.Errorf("tmux not found")}
	c := tmux.NewClient(r)
	if err := c.KillSession("myapp-feature"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_ListSessions_format(t *testing.T) {
	r := &mockRunner{exitCode: 0, stdout: "$0\ts\t\t\t\t\t\n"}
	c := tmux.NewClient(r)
	_, _ = c.ListSessions()
	argStr := fmt.Sprintf("%v", r.lastArgs)
	for _, want := range []string{"#{session_id}", "#{session_name}", "#{@codeherd_canonical_name}", "#{@codeherd_session_type}"} {
		if !strings.Contains(argStr, want) {
			t.Errorf("expected %q in args %s", want, argStr)
		}
	}
}

func TestClient_NewSession_execError(t *testing.T) {
	r := &mockRunner{exitCode: -1, err: fmt.Errorf("tmux not found")}
	c := tmux.NewClient(r)
	if err := c.NewSession("myapp", "/tmp"); err == nil {
		t.Fatal("expected error on exec failure")
	}
}

func TestClient_ListSessions_execError(t *testing.T) {
	r := &mockRunner{exitCode: -1, err: fmt.Errorf("tmux not found")}
	c := tmux.NewClient(r)
	_, err := c.ListSessions()
	if err == nil {
		t.Fatal("expected error on exec failure")
	}
}

func TestClient_ListSessions_unexpectedCode(t *testing.T) {
	r := &mockRunner{exitCode: 2, stderr: "unexpected error"}
	c := tmux.NewClient(r)
	_, err := c.ListSessions()
	if err == nil {
		t.Fatal("expected error for unexpected exit code")
	}
}

// TestNewRealRunner verifies the constructor returns a non-nil runner.
// This does not execute tmux — it only exercises the constructor.
func TestNewRealRunner(t *testing.T) {
	r := tmux.NewRealRunner()
	if r == nil {
		t.Fatal("NewRealRunner() returned nil")
	}
}

// TestRealRunner_Run exercises the RealRunner using "tmux -V" which prints the version.
func TestRealRunner_Run(t *testing.T) {
	r := tmux.NewRealRunner()
	stdout, _, exitCode, err := r.Run("-V")
	if err != nil {
		t.Fatalf("unexpected error running tmux -V: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("tmux -V exit code = %d, want 0", exitCode)
	}
	if stdout == "" {
		t.Error("expected non-empty stdout from tmux -V")
	}
}

// TestRealRunner_Run_nonZeroExit exercises the exit-code path by running
// "tmux has-session -t __nonexistent__session__" which exits 1 when not found.
func TestRealRunner_Run_nonZeroExit(t *testing.T) {
	r := tmux.NewRealRunner()
	_, _, exitCode, err := r.Run("has-session", "-t", "__nonexistent_codeherd_test_session__")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode == 0 {
		t.Error("expected non-zero exit code for nonexistent session")
	}
}

// TestRealRunner_Run_SocketEnvUnsetUsesDefault verifies that when SocketEnvVar
// is empty the runner targets the system default tmux server (no -S injected).
// We assert by running `tmux -V` which never touches a socket — the call must
// succeed regardless of the env var.
func TestRealRunner_Run_SocketEnvUnsetUsesDefault(t *testing.T) {
	t.Setenv(tmux.SocketEnvVar, "")
	r := tmux.NewRealRunner()
	_, _, code, err := r.Run("-V")
	if err != nil || code != 0 {
		t.Fatalf("tmux -V with empty %s: err=%v code=%d", tmux.SocketEnvVar, err, code)
	}
}

// TestRealRunner_Run_SocketEnvIsolatesServer verifies that setting
// SocketEnvVar to a per-test path produces an isolated tmux server: a session
// created on that socket appears in `tmux ls` on the same socket and is gone
// after `kill-server`. Skips if tmux cannot daemonize in the current env.
func TestRealRunner_Run_SocketEnvIsolatesServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	// Also clear $TMUX so tmux does not think we are nested inside another
	// session (would refuse new-session with an error).
	t.Setenv("TMUX", "")

	r := tmux.NewRealRunner()
	// Probe: create a short-lived session. If the server cannot daemonize
	// (sandboxed PID namespace etc.), skip — this isolates true regressions
	// from environment limitations.
	_, _, code, err := r.Run("new-session", "-d", "-s", "probe", "sleep", "30")
	if err != nil || code != 0 {
		t.Skipf("tmux daemonize unavailable in this environment (code=%d err=%v)", code, err)
	}
	t.Cleanup(func() { _, _, _, _ = r.Run("kill-server") })

	stdout, _, code, err := r.Run("ls", "-F", "#{session_name}")
	if err != nil || code != 0 {
		t.Fatalf("tmux ls on isolated socket: code=%d err=%v", code, err)
	}
	if !strings.Contains(stdout, "probe") {
		t.Errorf("isolated socket did not show probe session: %q", stdout)
	}

	// Outer system tmux server must not see this session. We deliberately do
	// not assert "session is absent from system tmux" because the developer's
	// outer server may or may not exist; the positive assertion above is
	// enough to demonstrate isolation.
}

func TestClient_NewSessionWithEnv_ok(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	env := map[string]string{"DEVENV_SESSION": "myapp-feature", "FOO": "bar"}
	_, err := c.NewSessionWithEnv("myapp-feature", "/tmp/wt", env, "claude --skip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.lastArgs[0] != "new-session" {
		t.Errorf("expected new-session, got %v", r.lastArgs)
	}
	// Verify -d, -s, -c flags are present
	argStr := fmt.Sprintf("%v", r.lastArgs)
	for _, want := range []string{"-d", "-s", "myapp-feature", "-c", "/tmp/wt"} {
		found := false
		for _, a := range r.lastArgs {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in args %s", want, argStr)
		}
	}
}

func TestClient_NewSessionWithEnv_error(t *testing.T) {
	r := &mockRunner{exitCode: 1, stderr: "duplicate session"}
	c := tmux.NewClient(r)
	_, err := c.NewSessionWithEnv("myapp", "/tmp", nil, "claude")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_NewSessionWithEnv_execError(t *testing.T) {
	r := &mockRunner{exitCode: -1, err: fmt.Errorf("tmux not found")}
	c := tmux.NewClient(r)
	_, err := c.NewSessionWithEnv("myapp", "/tmp", nil, "claude")
	if err == nil {
		t.Fatal("expected error on exec failure")
	}
}

func TestClient_NewSessionWithEnv_envFlags(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	env := map[string]string{"KEY": "val"}
	_, _ = c.NewSessionWithEnv("s", "/tmp", env, "cmd")
	foundE := false
	for i, a := range r.lastArgs {
		if a == "-e" && i+1 < len(r.lastArgs) && r.lastArgs[i+1] == "KEY=val" {
			foundE = true
			break
		}
	}
	if !foundE {
		t.Errorf("expected -e KEY=val in args, got %v", r.lastArgs)
	}
}

func TestClient_NewSessionWithEnv_cmdIsLastArg(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	_, _ = c.NewSessionWithEnv("s", "/tmp", nil, "claude --skip")
	last := r.lastArgs[len(r.lastArgs)-1]
	if last != "claude --skip" {
		t.Errorf("last arg = %q, want %q", last, "claude --skip")
	}
}

func TestGetOption(t *testing.T) {
	mr := &mockRunner{stdout: "running\n"}
	c := tmux.NewClient(mr)

	val, err := c.GetOption("myapp-feat", "@codeherd_status")
	if err != nil {
		t.Fatalf("GetOption() error = %v", err)
	}
	if val != "running" {
		t.Errorf("GetOption() = %q, want %q", val, "running")
	}
	wantArgs := []string{"show-option", "-t", "myapp-feat", "-v", "@codeherd_status"}
	if !slices.Equal(mr.lastArgs, wantArgs) {
		t.Errorf("args = %v, want %v", mr.lastArgs, wantArgs)
	}
}

func TestGetOption_notSet(t *testing.T) {
	mr := &mockRunner{stderr: "unknown option", exitCode: 1}
	c := tmux.NewClient(mr)

	val, err := c.GetOption("myapp-feat", "@codeherd_status")
	if err != nil {
		t.Fatalf("GetOption() error = %v", err)
	}
	if val != "" {
		t.Errorf("GetOption() = %q, want empty string for unset option", val)
	}
}

func TestGetOption_runnerError(t *testing.T) {
	mr := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(mr)

	_, err := c.GetOption("myapp-feat", "@codeherd_status")
	if err == nil {
		t.Error("GetOption() should return error when runner fails")
	}
}

func TestSetOption(t *testing.T) {
	mr := &mockRunner{}
	c := tmux.NewClient(mr)

	err := c.SetOption("myapp-feat", "@codeherd_status", "running")
	if err != nil {
		t.Fatalf("SetOption() error = %v", err)
	}
	wantArgs := []string{"set-option", "-t", "myapp-feat", "@codeherd_status", "running"}
	if !slices.Equal(mr.lastArgs, wantArgs) {
		t.Errorf("args = %v, want %v", mr.lastArgs, wantArgs)
	}
}

func TestSetOption_error(t *testing.T) {
	mr := &mockRunner{stderr: "no such session", exitCode: 1}
	c := tmux.NewClient(mr)

	err := c.SetOption("myapp-feat", "@codeherd_status", "running")
	if err == nil {
		t.Error("SetOption() should return error on non-zero exit")
	}
}

func TestSetOption_runnerError(t *testing.T) {
	mr := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(mr)

	err := c.SetOption("myapp-feat", "@codeherd_status", "running")
	if err == nil {
		t.Error("SetOption() should return error when runner fails")
	}
}

func TestNewSessionWithCmd(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	err := c.NewSessionWithCmd("codeherd", "/tmp", "ch --no-tmux")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"new-session", "-d", "-s", "codeherd", "-c", "/tmp", "ch --no-tmux"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestRenameSession(t *testing.T) {
	r := &mockRunner{}
	c := tmux.NewClient(r)

	err := c.RenameSession("old-name", "new-name")
	if err != nil {
		t.Fatalf("RenameSession() error = %v", err)
	}
	want := []string{"rename-session", "-t", "old-name", "new-name"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestRenameSession_Error(t *testing.T) {
	r := &mockRunner{exitCode: 1, stderr: "no such session"}
	c := tmux.NewClient(r)

	err := c.RenameSession("old", "new")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenameSession_runnerError(t *testing.T) {
	r := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(r)

	err := c.RenameSession("old", "new")
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestSwitchClient(t *testing.T) {
	r := &mockRunner{stdout: "", stderr: "", exitCode: 0}
	c := tmux.NewClient(r)
	err := c.SwitchClient("my-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"switch-client", "-t", "my-session"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestSwitchClientError(t *testing.T) {
	r := &mockRunner{stdout: "", stderr: "no current client", exitCode: 1}
	c := tmux.NewClient(r)
	err := c.SwitchClient("my-session")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no current client") {
		t.Errorf("error = %q, want it to contain 'no current client'", err)
	}
}

func TestCurrentSession(t *testing.T) {
	r := &mockRunner{
		stdout: "my-session\n", exitCode: 0,
	}
	c := tmux.NewClient(r)
	name, err := c.CurrentSession()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "my-session" {
		t.Errorf("name = %q, want %q", name, "my-session")
	}
	want := []string{"display-message", "-p", "#{session_name}"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestCurrentSessionNotInTmux(t *testing.T) {
	r := &mockRunner{
		stderr: "no current client", exitCode: 1,
	}
	c := tmux.NewClient(r)
	name, err := c.CurrentSession()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

func TestSessionID_ok(t *testing.T) {
	r := &mockRunner{stdout: "$3\n", exitCode: 0}
	c := tmux.NewClient(r)
	id, err := c.SessionID("myapp-feat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "$3" {
		t.Errorf("SessionID = %q, want $3", id)
	}
	want := []string{"display-message", "-t", "myapp-feat", "-p", "#{session_id}"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestSessionID_notFound(t *testing.T) {
	r := &mockRunner{exitCode: 1}
	c := tmux.NewClient(r)
	_, err := c.SessionID("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionID_runnerError(t *testing.T) {
	r := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(r)
	_, err := c.SessionID("myapp-feat")
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestIsPaneDead_dead(t *testing.T) {
	r := &mockRunner{stdout: "1\n", exitCode: 0}
	c := tmux.NewClient(r)
	dead, err := c.IsPaneDead("codeherd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dead {
		t.Error("expected dead=true")
	}
	if r.lastArgs[0] != "list-panes" {
		t.Errorf("expected list-panes, got %v", r.lastArgs)
	}
}

func TestIsPaneDead_alive(t *testing.T) {
	r := &mockRunner{stdout: "0\n", exitCode: 0}
	c := tmux.NewClient(r)
	dead, err := c.IsPaneDead("codeherd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dead {
		t.Error("expected dead=false")
	}
}

func TestIsPaneDead_noSession(t *testing.T) {
	r := &mockRunner{exitCode: 1}
	c := tmux.NewClient(r)
	dead, err := c.IsPaneDead("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dead {
		t.Error("expected dead=false when session doesn't exist")
	}
}

func TestIsPaneDead_runnerError(t *testing.T) {
	r := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(r)
	_, err := c.IsPaneDead("codeherd")
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestRespawnPane_ok(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	err := c.RespawnPane("codeherd:0", "ch --no-tmux")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"respawn-pane", "-k", "-t", "codeherd:0", "ch --no-tmux"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestRespawnPane_noCmd(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := tmux.NewClient(r)
	err := c.RespawnPane("codeherd:0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"respawn-pane", "-k", "-t", "codeherd:0"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}

func TestRespawnPane_error(t *testing.T) {
	r := &mockRunner{exitCode: 1, stderr: "no such pane"}
	c := tmux.NewClient(r)
	err := c.RespawnPane("codeherd:0", "ch")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRespawnPane_runnerError(t *testing.T) {
	r := &mockRunner{err: errors.New("boom")}
	c := tmux.NewClient(r)
	err := c.RespawnPane("codeherd:0", "ch")
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestSelectWindow(t *testing.T) {
	r := &mockRunner{stdout: "", stderr: "", exitCode: 0}
	c := tmux.NewClient(r)
	err := c.SelectWindow("codeherd:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"select-window", "-t", "codeherd:0"}
	if !slices.Equal(r.lastArgs, want) {
		t.Errorf("args = %v, want %v", r.lastArgs, want)
	}
}
