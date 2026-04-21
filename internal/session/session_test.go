package session_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
)

// mockRunner implements tmux.Runner for testing.
type mockRunner struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	calls    [][]string
}

func (m *mockRunner) Run(args ...string) (string, string, int, error) {
	m.calls = append(m.calls, args)
	return m.stdout, m.stderr, m.exitCode, m.err
}

type mockHook struct {
	calls  []hookCall
	failOn string
}
type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

func newService(t *testing.T, r *mockRunner) *session.Service {
	t.Helper()
	tc := tmux.NewClient(r)
	return session.NewService(tc, &mockHook{})
}

func TestStart_OK(t *testing.T) {
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1},                 // list-sessions → no sessions (exit 1 = empty)
		{exitCode: 0},                 // new-session → ok
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	wtDir := t.TempDir()
	sessionID, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    wtDir,
		Cmd:     "claude",
		Env:     map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if sessionID != "$1" {
		t.Errorf("Start() sessionID = %q, want $1", sessionID)
	}
	if len(r2.calls) != 7 {
		t.Errorf("expected 7 tmux calls, got %d: %v", len(r2.calls), r2.calls)
	}
}

func TestStart_DuplicateSession(t *testing.T) {
	// list-sessions returns a record with the same canonical name
	line := "$1\tmyapp-feature\tmyapp-feature\tagent\trunning\t\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    t.TempDir(),
		Cmd:     "claude",
	})
	if err == nil {
		t.Fatal("expected error for duplicate session")
	}
	if !errors.Is(err, session.ErrSessionExists) {
		t.Errorf("error = %v, want ErrSessionExists", err)
	}
}

func TestStart_DuplicateSession_Prefixed(t *testing.T) {
	// list-sessions returns a prefixed (waiting) session with the same canonical name
	line := "$1\t⚡ myapp-feature\tmyapp-feature\tagent\twaiting\t\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    t.TempDir(),
		Cmd:     "claude",
	})
	if !errors.Is(err, session.ErrSessionExists) {
		t.Errorf("error = %v, want ErrSessionExists", err)
	}
}

func TestStart_MissingPath(t *testing.T) {
	// list-sessions exits 1 (no sessions) — not an error
	r := &mockRunner{exitCode: 1}
	svc := newService(t, r)

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    "/nonexistent/path",
		Cmd:     "claude",
	})
	if !errors.Is(err, session.ErrPathNotFound) {
		t.Errorf("error = %v, want ErrPathNotFound", err)
	}
}

func TestSetStatus_Running(t *testing.T) {
	// SetStatus("running") with canonical name resolves the prefixed actual name.
	line := "$1\t⚡ myapp-feature\tmyapp-feature\tagent\twaiting\t\t\n"
	r := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line}, // list-sessions
		{exitCode: 0},               // set-option status
		{exitCode: 0},               // set-option annotation
		{exitCode: 0},               // rename-session (remove ⚡ prefix)
	}}
	tc := tmux.NewClient(r)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.SetStatus("myapp-feature", "running", ""); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d: %v", len(r.calls), r.calls)
	}
	// Verify rename targeted the prefixed name.
	renameArgs := r.calls[3]
	if renameArgs[len(renameArgs)-2] != "⚡ myapp-feature" {
		t.Errorf("rename source = %q, want ⚡ myapp-feature", renameArgs[len(renameArgs)-2])
	}
	if renameArgs[len(renameArgs)-1] != "myapp-feature" {
		t.Errorf("rename target = %q, want myapp-feature", renameArgs[len(renameArgs)-1])
	}
}

func TestSetStatus_Waiting(t *testing.T) {
	// SetStatus("waiting") with canonical name adds the prefix.
	line := "$1\tmyapp-feature\tmyapp-feature\tagent\trunning\t\t\n"
	r := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line}, // list-sessions
		{exitCode: 0},               // set-option status
		{exitCode: 0},               // set-option annotation
		{exitCode: 0},               // rename-session (add ⚡ prefix)
	}}
	tc := tmux.NewClient(r)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.SetStatus("myapp-feature", "waiting", "Claude needs input"); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(r.calls))
	}
}

func TestSetStatus_EmptyName(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	svc := newService(t, r)

	if err := svc.SetStatus("", "running", ""); err != nil {
		t.Fatalf("SetStatus() on empty name error = %v", err)
	}
	// No tmux calls should be made
	if len(r.calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(r.calls))
	}
}

func TestSetStatus_SuppressesError(t *testing.T) {
	// list-sessions fails — SetStatus suppresses the error and returns nil.
	r := &mockRunner{exitCode: 1, err: errors.New("tmux failed")}
	svc := newService(t, r)

	if err := svc.SetStatus("any-session", "running", ""); err != nil {
		t.Fatalf("SetStatus() should suppress errors: %v", err)
	}
}

func TestSetStatus_SessionNotFound(t *testing.T) {
	// Session with that canonical name does not exist — SetStatus is a no-op.
	r := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1}, // list-sessions exit 1 = no sessions
	}}
	tc := tmux.NewClient(r)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.SetStatus("myapp-feature", "running", ""); err != nil {
		t.Fatalf("SetStatus() should suppress not-found: %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("expected 1 call (list only), got %d", len(r.calls))
	}
}

func TestSetStatus_InvalidStatus(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	svc := newService(t, r)

	if err := svc.SetStatus("myapp-feature", "invalid", ""); err != nil {
		t.Fatalf("SetStatus() should suppress errors: %v", err)
	}
	// No tmux calls should be made for invalid status
	if len(r.calls) != 0 {
		t.Errorf("expected 0 calls for invalid status, got %d", len(r.calls))
	}
}

// mockResponse is a single canned response for mockRunnerSequence.
type mockResponse struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// mockRunnerSequence returns responses in order, repeating the last one.
type mockRunnerSequence struct {
	responses []mockResponse
	calls     [][]string
	idx       int
}

func (m *mockRunnerSequence) Run(args ...string) (string, string, int, error) {
	m.calls = append(m.calls, args)
	i := m.idx
	if i >= len(m.responses) {
		i = len(m.responses) - 1
	}
	m.idx++
	r := m.responses[i]
	return r.stdout, r.stderr, r.exitCode, r.err
}

func TestList_Empty(t *testing.T) {
	r := &mockRunner{exitCode: 1} // list-sessions exit 1 = no sessions
	svc := newService(t, r)

	sessions, err := svc.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("len = %d, want 0", len(sessions))
	}
}

func TestList_WithOptions(t *testing.T) {
	lines := "$1\t⚡ myapp-feature\tmyapp-feature\tagent\twaiting\tProceed?\t\n" +
		"$2\tapi-main\tapi-main\tagent\t\t\t\n" +
		"$3\tapi-main~sh\tapi-main\tshell\t\t\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: lines},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	sessions, err := svc.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("len = %d, want 3 (agent + shell)", len(sessions))
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Name < sessions[j].Name
	})

	if sessions[0].Name != "api-main" {
		t.Errorf("sessions[0].Name = %q, want api-main", sessions[0].Name)
	}
	if sessions[0].Type != semconv.SessionTypeAgent {
		t.Errorf("sessions[0].Type = %q, want agent", sessions[0].Type)
	}
	if sessions[0].Status != "" {
		t.Errorf("sessions[0].Status = %q, want empty", sessions[0].Status)
	}
	if sessions[1].Name != "api-main" {
		t.Errorf("sessions[1].Name = %q, want api-main", sessions[1].Name)
	}
	if sessions[1].Type != semconv.SessionTypeShell {
		t.Errorf("sessions[1].Type = %q, want shell", sessions[1].Type)
	}
	if sessions[2].Name != "myapp-feature" {
		t.Errorf("sessions[2].Name = %q, want myapp-feature", sessions[2].Name)
	}
	if sessions[2].Type != semconv.SessionTypeAgent {
		t.Errorf("sessions[2].Type = %q, want agent", sessions[2].Type)
	}
	if sessions[2].Status != semconv.StatusWaiting {
		t.Errorf("sessions[2].Status = %q, want waiting", sessions[2].Status)
	}
	if sessions[2].Annotation != "Proceed?" {
		t.Errorf("sessions[2].Annotation = %q, want Proceed?", sessions[2].Annotation)
	}
}

func TestShow_OK(t *testing.T) {
	line := "$1\tmyapp-feature\tmyapp-feature\tagent\trunning\t\t2024-01-01T00:00:00Z\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	info, err := svc.Show("myapp", "feature", semconv.SessionTypeAgent)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if info.Name != "myapp-feature" {
		t.Errorf("Name = %q, want myapp-feature", info.Name)
	}
	if info.TmuxName != "myapp-feature" {
		t.Errorf("TmuxName = %q, want myapp-feature", info.TmuxName)
	}
	if info.SessionID != "$1" {
		t.Errorf("SessionID = %q, want $1", info.SessionID)
	}
	if info.Status != semconv.StatusRunning {
		t.Errorf("Status = %q, want running", info.Status)
	}
	if info.Type != semconv.SessionTypeAgent {
		t.Errorf("Type = %q, want agent", info.Type)
	}
	if info.StartedAt.IsZero() {
		t.Error("StartedAt should be non-zero")
	}
}

func TestShow_WaitingSession(t *testing.T) {
	// Session has prefix in tmux but canonical name is used for lookup.
	line := "$2\t⚡ myapp-feature\tmyapp-feature\tagent\twaiting\tneed input\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	info, err := svc.Show("myapp", "feature", semconv.SessionTypeAgent)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if info.Name != "myapp-feature" {
		t.Errorf("Name = %q, want myapp-feature (canonical)", info.Name)
	}
	if info.TmuxName != "⚡ myapp-feature" {
		t.Errorf("TmuxName = %q, want ⚡ myapp-feature", info.TmuxName)
	}
	if info.SessionID != "$2" {
		t.Errorf("SessionID = %q, want $2", info.SessionID)
	}
}

func TestShow_NotFound(t *testing.T) {
	r := &mockRunner{exitCode: 1} // list-sessions exit 1 = no sessions
	svc := newService(t, r)

	_, err := svc.Show("nonexistent", "branch", semconv.SessionTypeAgent)
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestStop_OK(t *testing.T) {
	line := "$1\tmyapp-feature\tmyapp-feature\tagent\trunning\t\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line}, // list-sessions
		{exitCode: 0},               // kill-session
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.Stop("myapp", "feature", semconv.SessionTypeAgent); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestStop_WaitingSession(t *testing.T) {
	// Stop must kill the prefixed session name.
	line := "$1\t⚡ myapp-feature\tmyapp-feature\tagent\twaiting\t\t\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line}, // list-sessions
		{exitCode: 0},               // kill-session
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.Stop("myapp", "feature", semconv.SessionTypeAgent); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	// Verify kill-session targeted the prefixed name.
	killArgs := r2.calls[1]
	if killArgs[len(killArgs)-1] != "⚡ myapp-feature" {
		t.Errorf("kill-session target = %q, want ⚡ myapp-feature", killArgs[len(killArgs)-1])
	}
}

func TestStop_NotFound(t *testing.T) {
	r := &mockRunner{exitCode: 1} // list-sessions exit 1 = no sessions
	svc := newService(t, r)

	err := svc.Stop("nonexistent", "branch", semconv.SessionTypeAgent)
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestStart_RunnerError(t *testing.T) {
	r := &mockRunner{exitCode: 1, err: errors.New("tmux exec failed")}
	svc := newService(t, r)

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    t.TempDir(),
		Cmd:     "claude",
	})
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

// TestList_WithStartedAt verifies that StartedAt is parsed when it is present
// in the tmux session record.
func TestList_WithStartedAt(t *testing.T) {
	startedAt := "2024-01-15T10:00:00Z"
	line := "$1\tmyapp-main\tmyapp-main\tagent\trunning\t\t" + startedAt + "\n"
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	sessions, err := svc.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len = %d, want 1", len(sessions))
	}
	if sessions[0].StartedAt.IsZero() {
		t.Error("StartedAt should be non-zero when timestamp is provided")
	}
}

func TestList_RunnerError(t *testing.T) {
	// list-sessions returns a runner error
	r := &mockRunner{exitCode: 1, err: errors.New("tmux exec failed")}
	svc := newService(t, r)

	_, err := svc.List()
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestService_List_IncludesShellSessions(t *testing.T) {
	path := t.TempDir()
	r2 := &mockRunnerSequence{responses: []mockResponse{
		// --- First Start (agent) ---
		{exitCode: 1},                 // list-sessions → no sessions
		{exitCode: 0},                 // new-session
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
		// --- Second Start (shell) ---
		{exitCode: 0, stdout: "$1\tapp-main\tapp-main\tagent\trunning\t\t\n"},
		{exitCode: 0},                 // new-session
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$2\n"}, // display-message → session_id
		// --- List() ---
		{exitCode: 0, stdout: "$1\tapp-main\tapp-main\tagent\trunning\t\t\n" +
			"$2\tapp-main~sh\tapp-main\tshell\trunning\t\t\n"},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeAgent, Cmd: "agent",
	})
	if err != nil {
		t.Fatalf("agent Start: %v", err)
	}

	_, err = svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
	})
	if err != nil {
		t.Fatalf("shell Start: %v", err)
	}

	infos, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("List returned %d sessions, want 2", len(infos))
	}

	var sawAgent, sawShell bool
	for _, i := range infos {
		switch i.Type {
		case semconv.SessionTypeAgent:
			sawAgent = true
		case semconv.SessionTypeShell:
			sawShell = true
		}
	}
	if !sawAgent || !sawShell {
		t.Fatalf("List missing a type: agent=%v shell=%v", sawAgent, sawShell)
	}
}

func TestShow_RunnerError(t *testing.T) {
	// has-session returns a runner error
	r := &mockRunner{exitCode: 1, err: errors.New("tmux exec failed")}
	svc := newService(t, r)

	_, err := svc.Show("myapp", "feature", semconv.SessionTypeAgent)
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestStop_KillError(t *testing.T) {
	line := "$1\tmyapp-feature\tmyapp-feature\tagent\trunning\t\t\n"
	r := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 0, stdout: line},                   // list-sessions
		{exitCode: 1, err: errors.New("kill failed")}, // kill-session
	}}
	tc := tmux.NewClient(r)
	svc := session.NewService(tc, &mockHook{})

	if err := svc.Stop("myapp", "feature", semconv.SessionTypeAgent); err == nil {
		t.Fatal("expected error when kill fails")
	}
}

func TestSessionExistsError(t *testing.T) {
	err := &session.SessionExistsError{
		Project: "myapp",
		Branch:  "feature",
		Type:    semconv.SessionTypeAgent,
	}
	want := "session already exists: myapp/feature (agent)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, session.ErrSessionExists) {
		t.Error("errors.Is(err, ErrSessionExists) = false, want true")
	}
}

func TestStart_StatError(t *testing.T) {
	// list-sessions exits 1 (no sessions)
	r := &mockRunner{exitCode: 1}
	svc := newService(t, r)

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    "/tmp/\x00invalid",
		Cmd:     "claude",
	})
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
	if errors.Is(err, session.ErrPathNotFound) {
		t.Error("got ErrPathNotFound, expected a different error for invalid path")
	}
}

func TestStart_SessionIDError(t *testing.T) {
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1}, // list-sessions → no sessions
		{exitCode: 0}, // new-session → ok
		{exitCode: 0}, // set-option status
		{exitCode: 0}, // set-option started_at
		{exitCode: 0}, // set-option canonical_name
		{exitCode: 0}, // set-option session_type
		{exitCode: 1}, // display-message → fails
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    t.TempDir(),
		Cmd:     "claude",
	})
	if err == nil {
		t.Fatal("expected error when SessionID fails")
	}
}

func TestService_Start_ShellAndAgentCoexist(t *testing.T) {
	path := t.TempDir()

	// Each Start calls: list-sessions, new-session, set-option x4, display-message (session_id).
	// Two Starts = 14 calls total. Third Start (duplicate agent) only calls list-sessions.
	r2 := &mockRunnerSequence{responses: []mockResponse{
		// --- First Start (agent) ---
		{exitCode: 1},                 // list-sessions → no sessions
		{exitCode: 0},                 // new-session
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
		// --- Second Start (shell) ---
		// list-sessions returns the existing agent session; shell has different type so no conflict
		{exitCode: 0, stdout: "$1\tapp-main\tapp-main\tagent\trunning\t\t\n"},
		{exitCode: 0},                 // new-session
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$2\n"}, // display-message → session_id
		// --- Third Start (duplicate agent) ---
		// list-sessions returns both sessions; agent canonical name matches → ErrSessionExists
		{exitCode: 0, stdout: "$1\tapp-main\tapp-main\tagent\trunning\t\t\n" +
			"$2\tapp-main~sh\tapp-main\tshell\trunning\t\t\n"},
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeAgent, Cmd: "agent",
	})
	if err != nil {
		t.Fatalf("agent Start: %v", err)
	}

	_, err = svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
	})
	if err != nil {
		t.Fatalf("shell Start coexisting with agent: %v", err)
	}

	_, err = svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeAgent, Cmd: "agent",
	})
	if !errors.Is(err, session.ErrSessionExists) {
		t.Fatalf("duplicate agent Start: want ErrSessionExists, got %v", err)
	}
}

func TestStart_TriggersHooks(t *testing.T) {
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1},                 // list-sessions → no sessions
		{exitCode: 0},                 // new-session → ok
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
	}}
	tc := tmux.NewClient(r2)
	hookMock := &mockHook{}
	svc := session.NewService(tc, hookMock)

	wtDir := t.TempDir()
	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    wtDir,
		Cmd:     "claude",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if len(hookMock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookMock.calls))
	}
	if hookMock.calls[0].name != semconv.HookPreSession {
		t.Errorf("first hook = %q, want %q", hookMock.calls[0].name, semconv.HookPreSession)
	}
	if hookMock.calls[1].name != semconv.HookPostSession {
		t.Errorf("second hook = %q, want %q", hookMock.calls[1].name, semconv.HookPostSession)
	}
}

func TestService_Show_ShellType(t *testing.T) {
	path := t.TempDir()
	r2 := &mockRunnerSequence{responses: []mockResponse{
		// --- Start (shell) ---
		{exitCode: 1},                 // list-sessions → no sessions
		{exitCode: 0},                 // new-session
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
		// --- Show(shell) ---
		{exitCode: 0, stdout: "$1\tapp-main~sh\tapp-main\tshell\trunning\t\t\n"},
		// --- Show(agent) ---
		{exitCode: 1}, // list-sessions → no sessions (exit 1)
	}}
	tc := tmux.NewClient(r2)
	svc := session.NewService(tc, &mockHook{})

	_, err := svc.Start(session.StartRequest{
		Project: "app", Branch: "main", Path: path,
		Type: semconv.SessionTypeShell, Cmd: "/bin/sh",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	info, err := svc.Show("app", "main", semconv.SessionTypeShell)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != semconv.SessionTypeShell {
		t.Fatalf("Show().Type = %q, want shell", info.Type)
	}

	// Agent-type Show must return ErrSessionNotFound for shell-only session.
	if _, err := svc.Show("app", "main", semconv.SessionTypeAgent); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("agent Show for shell-only session: want ErrSessionNotFound, got %v", err)
	}
}
