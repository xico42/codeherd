package tmux

import (
	"fmt"
	"sort"
	"strings"
)

// SessionRecord holds the structured data returned by ListSessions.
type SessionRecord struct {
	ID            string // tmux session_id (e.g. "$1") — stable, never changes
	Name          string // current tmux session name (may have status prefix)
	CanonicalName string // @codeherd_canonical_name — original name, never changes
	SessionType   string // @codeherd_session_type — "agent" or "shell"
	Status        string // @codeherd_status
	Annotation    string // @codeherd_annotation
	StartedAt     string // @codeherd_started_at (raw RFC3339 string)
}

// Client provides typed tmux operations built on a Runner.
type Client struct {
	runner Runner
}

// NewClient creates a Client using the given Runner.
func NewClient(r Runner) *Client {
	return &Client{runner: r}
}

// HasSession reports whether a tmux session with the given name exists.
// tmux exits with code 1 to signal "no session" — this is not an error.
func (c *Client) HasSession(name string) (bool, error) {
	_, _, code, err := c.runner.Run("has-session", "-t", name)
	if err != nil {
		return false, fmt.Errorf("tmux has-session: %w", err)
	}
	return code == 0, nil
}

// KillSession terminates the named tmux session.
func (c *Client) KillSession(name string) error {
	_, stderr, code, err := c.runner.Run("kill-session", "-t", name)
	if err != nil {
		return fmt.Errorf("tmux kill-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux kill-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// NewSession creates a detached tmux session with the given name and start directory.
func (c *Client) NewSession(name, dir string) error {
	_, stderr, code, err := c.runner.Run("new-session", "-d", "-s", name, "-c", dir)
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// NewSessionWithCmd creates a detached tmux session with an initial command.
func (c *Client) NewSessionWithCmd(name, dir, cmd string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	if cmd != "" {
		args = append(args, cmd)
	}
	_, stderr, code, err := c.runner.Run(args...)
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// NewSessionWithEnv creates a detached tmux session with environment variables
// and an initial command.
func (c *Client) NewSessionWithEnv(name, dir string, env map[string]string, cmd string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	// Sort keys for deterministic arg order (testability).
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}
	args = append(args, cmd)
	_, stderr, code, err := c.runner.Run(args...)
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// GetOption reads a tmux user-defined option from a session.
// Returns empty string (no error) when the option is not set or the session does not exist (tmux exits 1).
func (c *Client) GetOption(session, option string) (string, error) {
	stdout, _, code, err := c.runner.Run("show-option", "-t", session, "-v", option)
	if err != nil {
		return "", fmt.Errorf("tmux show-option: %w", err)
	}
	if code != 0 {
		return "", nil // option not set
	}
	return strings.TrimSpace(stdout), nil
}

// SetOption sets a tmux user-defined option on a session.
func (c *Client) SetOption(session, option, value string) error {
	_, stderr, code, err := c.runner.Run("set-option", "-t", session, option, value)
	if err != nil {
		return fmt.Errorf("tmux set-option: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux set-option: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// RenameSession renames a tmux session.
func (c *Client) RenameSession(oldName, newName string) error {
	_, stderr, code, err := c.runner.Run("rename-session", "-t", oldName, newName)
	if err != nil {
		return fmt.Errorf("tmux rename-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux rename-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// SwitchClient switches the current tmux client to the named session.
func (c *Client) SwitchClient(name string) error {
	_, stderr, code, err := c.runner.Run("switch-client", "-t", name)
	if err != nil {
		return fmt.Errorf("tmux switch-client: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux switch-client: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// SelectWindow selects a window in the current session.
func (c *Client) SelectWindow(target string) error {
	_, stderr, code, err := c.runner.Run("select-window", "-t", target)
	if err != nil {
		return fmt.Errorf("tmux select-window: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux select-window: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// SessionID returns the stable tmux session_id (e.g. "$1") for a given target.
// The target can be a session name, ID, or any tmux target accepted by -t.
func (c *Client) SessionID(target string) (string, error) {
	stdout, _, code, err := c.runner.Run("display-message", "-t", target, "-p", "#{session_id}")
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("tmux session not found: %s", target)
	}
	return strings.TrimSpace(stdout), nil
}

// CurrentSession returns the name of the tmux session the current client is
// attached to. Returns empty string (no error) if not inside tmux.
func (c *Client) CurrentSession() (string, error) {
	stdout, _, code, err := c.runner.Run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	if code != 0 {
		return "", nil // not in tmux
	}
	return strings.TrimSpace(stdout), nil
}

// ListSessions returns a SessionRecord for every active tmux session.
// Returns nil (no error) when no sessions exist (tmux exits 1 in that case).
func (c *Client) ListSessions() ([]SessionRecord, error) {
	format := "#{session_id}\t#{session_name}\t#{@codeherd_canonical_name}\t#{@codeherd_session_type}\t#{@codeherd_status}\t#{@codeherd_annotation}\t#{@codeherd_started_at}"
	stdout, stderr, code, err := c.runner.Run("list-sessions", "-F", format)
	if err != nil {
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}
	if code == 1 {
		return nil, nil // no sessions — not an error
	}
	if code != 0 {
		return nil, fmt.Errorf("tmux list-sessions: %s", strings.TrimSpace(stderr))
	}
	var records []SessionRecord
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		for len(fields) < 7 {
			fields = append(fields, "")
		}
		records = append(records, SessionRecord{
			ID:            fields[0],
			Name:          fields[1],
			CanonicalName: fields[2],
			SessionType:   fields[3],
			Status:        fields[4],
			Annotation:    fields[5],
			StartedAt:     fields[6],
		})
	}
	return records, nil
}
