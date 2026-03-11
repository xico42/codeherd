package cmd

import (
	"fmt"
	"testing"

	"github.com/xico42/codeherd/internal/tmux"
)

// mockResponse holds a single response for multiMockRunner.
type mockResponse struct {
	stdout   string
	stderr   string
	exitCode int
}

// multiMockRunner returns different responses for sequential calls.
type multiMockRunner struct {
	calls     [][]string
	responses []mockResponse
	callIdx   int
}

func (m *multiMockRunner) Run(args ...string) (string, string, int, error) {
	m.calls = append(m.calls, args)
	if m.callIdx < len(m.responses) {
		r := m.responses[m.callIdx]
		m.callIdx++
		return r.stdout, r.stderr, r.exitCode, nil
	}
	return "", "", 0, nil
}

func TestRunTUIInTmux_AlreadyInCodeherd(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "codeherd\n", exitCode: 0}, // display-message (CurrentSession)
			{stdout: "0\n", exitCode: 0},        // list-panes (pane alive)
			{stdout: "", exitCode: 0},           // select-window
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[1][0] != "list-panes" {
		t.Errorf("expected list-panes, got %v", r.calls[1])
	}
	if r.calls[2][0] != "select-window" {
		t.Errorf("expected select-window, got %v", r.calls[2])
	}
}

func TestRunTUIInTmux_AlreadyInCodeherd_DeadPane(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "codeherd\n", exitCode: 0}, // display-message (CurrentSession)
			{stdout: "1\n", exitCode: 0},        // list-panes (pane DEAD)
			{stdout: "", exitCode: 0},           // respawn-pane
			{stdout: "", exitCode: 0},           // select-window
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[2][0] != "respawn-pane" {
		t.Errorf("expected respawn-pane, got %v", r.calls[2])
	}
	if r.calls[3][0] != "select-window" {
		t.Errorf("expected select-window, got %v", r.calls[3])
	}
}

func TestRunTUIInTmux_InDifferentSession(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "other-session\n", exitCode: 0}, // display-message (CurrentSession)
			{stdout: "", exitCode: 0},                // has-session (exists)
			{stdout: "0\n", exitCode: 0},             // list-panes (pane alive)
			{stdout: "", exitCode: 0},                // switch-client
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[3][0] != "switch-client" {
		t.Errorf("expected switch-client, got %v", r.calls[3])
	}
}

func TestRunTUIInTmux_InDifferentSession_DeadPane(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "other-session\n", exitCode: 0}, // display-message (CurrentSession)
			{stdout: "", exitCode: 0},                // has-session (exists)
			{stdout: "1\n", exitCode: 0},             // list-panes (pane DEAD)
			{stdout: "", exitCode: 0},                // respawn-pane
			{stdout: "", exitCode: 0},                // switch-client
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[2][0] != "list-panes" {
		t.Errorf("expected list-panes, got %v", r.calls[2])
	}
	if r.calls[3][0] != "respawn-pane" {
		t.Errorf("expected respawn-pane, got %v", r.calls[3])
	}
}

func TestRunTUIInTmux_NotInTmux_SessionExists(t *testing.T) {
	t.Setenv("TMUX", "")

	origExec := execTmuxAttach
	defer func() { execTmuxAttach = origExec }()
	var attachedTo string
	execTmuxAttach = func(name string) error {
		attachedTo = name
		return nil
	}

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "", exitCode: 0},    // has-session (exists)
			{stdout: "0\n", exitCode: 0}, // list-panes (pane alive)
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[0][0] != "has-session" {
		t.Errorf("expected has-session, got %v", r.calls[0])
	}
	if r.calls[1][0] != "list-panes" {
		t.Errorf("expected list-panes, got %v", r.calls[1])
	}
	if attachedTo != "codeherd" {
		t.Errorf("expected attach to codeherd, got %q", attachedTo)
	}
}

func TestRunTUIInTmux_NotInTmux_SessionExists_DeadPane(t *testing.T) {
	t.Setenv("TMUX", "")

	origExec := execTmuxAttach
	defer func() { execTmuxAttach = origExec }()
	var attachedTo string
	execTmuxAttach = func(name string) error {
		attachedTo = name
		return nil
	}

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "", exitCode: 0},    // has-session (exists)
			{stdout: "1\n", exitCode: 0}, // list-panes (pane DEAD)
			{stdout: "", exitCode: 0},    // respawn-pane
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[2][0] != "respawn-pane" {
		t.Errorf("expected respawn-pane, got %v", r.calls[2])
	}
	if attachedTo != "codeherd" {
		t.Errorf("expected attach to codeherd, got %q", attachedTo)
	}
}

func TestRunTUIInTmux_NotInTmux_CreateSession(t *testing.T) {
	t.Setenv("TMUX", "")

	origExec := execTmuxAttach
	defer func() { execTmuxAttach = origExec }()
	var attachedTo string
	execTmuxAttach = func(name string) error {
		attachedTo = name
		return nil
	}

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "", exitCode: 1}, // has-session (doesn't exist)
			{stdout: "", exitCode: 0}, // new-session
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %v", len(r.calls), r.calls)
	}
	if r.calls[1][0] != "new-session" {
		t.Errorf("expected new-session, got %v", r.calls[1])
	}
	if attachedTo != "codeherd" {
		t.Errorf("expected attach to codeherd, got %q", attachedTo)
	}
}

func TestRunTUIInTmux_HasSessionError(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	// display-message returns non-codeherd, then has-session errors.
	tc := tmux.NewClient(&hasSessionErrRunner{})
	err := runTUIInTmux(tc)
	if err == nil {
		t.Error("expected error when HasSession fails")
	}
}

// hasSessionErrRunner returns an error on the second call (has-session).
type hasSessionErrRunner struct {
	callIdx int
}

func (r *hasSessionErrRunner) Run(args ...string) (string, string, int, error) {
	r.callIdx++
	if r.callIdx == 1 {
		// display-message → returns non-codeherd session
		return "other-session\n", "", 0, nil
	}
	// has-session → return actual error
	return "", "", 0, fmt.Errorf("connection refused")
}

func TestRunTUIInTmux_SelectWindowError(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "codeherd\n", exitCode: 0},                   // display-message
			{stdout: "0\n", exitCode: 0},                          // list-panes (alive)
			{stdout: "", stderr: "window not found", exitCode: 1}, // select-window error
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err == nil {
		t.Error("expected error when SelectWindow fails")
	}
}

func TestRunTUIInTmux_SwitchClientError(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "other\n", exitCode: 0},                       // display-message
			{stdout: "", exitCode: 0},                              // has-session (exists)
			{stdout: "0\n", exitCode: 0},                           // list-panes (alive)
			{stdout: "", stderr: "no current client", exitCode: 1}, // switch-client error
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err == nil {
		t.Error("expected error when SwitchClient fails")
	}
}

func TestRunTUIInTmux_NewSessionError(t *testing.T) {
	t.Setenv("TMUX", "")

	r := &multiMockRunner{
		responses: []mockResponse{
			{stdout: "", exitCode: 1},                              // has-session (doesn't exist)
			{stdout: "", stderr: "duplicate session", exitCode: 1}, // new-session error
		},
	}
	tc := tmux.NewClient(r)
	err := runTUIInTmux(tc)
	if err == nil {
		t.Error("expected error when NewSessionWithCmd fails")
	}
}
