package session

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

var (
	ErrSessionExists   = errors.New("session already exists")
	ErrSessionNotFound = errors.New("session not found")
	ErrPathNotFound    = errors.New("worktree path not found")
)

// SessionExistsError is returned by Start when a tmux session with the same
// name already exists. It wraps ErrSessionExists for errors.Is compatibility
// and carries the session Name for structured access by callers.
type SessionExistsError struct {
	Name string
}

func (e *SessionExistsError) Error() string {
	return ErrSessionExists.Error() + ": " + e.Name
}

func (e *SessionExistsError) Unwrap() error {
	return ErrSessionExists
}

// Service manages codeherd tmux sessions and their persisted state.
type Service struct {
	tmux *tmux.Client
}

// NewService creates a Service using the given tmux client.
func NewService(tmux *tmux.Client) *Service {
	return &Service{tmux: tmux}
}

// StartRequest holds parameters for starting a new session.
type StartRequest struct {
	Project string
	Branch  string
	Path    string
	Cmd     string
	Env     map[string]string
	Attach  bool
}

// Start creates a new detached tmux session for the given project/branch,
// sets the CODEHERD_SESSION env var, and sets @codeherd_status and
// @codeherd_started_at tmux options on the new session.
// Returns ErrSessionExists if a session with the derived name already exists.
// Returns ErrPathNotFound if Path does not exist on disk.
func (s *Service) Start(req StartRequest) (string, error) {
	name := semconv.SessionName(req.Project, req.Branch)

	// Check for existing session by canonical name (handles prefixed names too).
	records, err := s.tmux.ListSessions()
	if err != nil {
		return "", fmt.Errorf("checking session: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == name {
			return "", &SessionExistsError{Name: name}
		}
	}

	if _, err := os.Stat(req.Path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrPathNotFound, req.Path)
		}
		return "", fmt.Errorf("checking path: %w", err)
	}

	env := make(map[string]string)
	for k, v := range req.Env {
		env[k] = v
	}
	env[semconv.SessionEnvVar] = name

	if err := s.tmux.NewSessionWithEnv(name, req.Path, env, req.Cmd); err != nil {
		return "", fmt.Errorf("creating tmux session: %w", err)
	}

	now := time.Now().UTC()
	_ = s.tmux.SetOption(name, semconv.TmuxOptionStatus, semconv.StatusRunning)
	_ = s.tmux.SetOption(name, semconv.TmuxOptionStartedAt, now.Format(time.RFC3339))
	_ = s.tmux.SetOption(name, semconv.TmuxOptionCanonicalName, name)
	_ = s.tmux.SetOption(name, semconv.TmuxOptionSessionType, semconv.SessionTypeAgent)

	// Get the stable session ID (unaffected by renames).
	id, err := s.tmux.SessionID(name)
	if err != nil {
		return "", fmt.Errorf("getting session id: %w", err)
	}
	return id, nil
}

// SessionInfo holds display information about a tmux session.
type SessionInfo struct {
	Name       string
	TmuxName   string // actual tmux session name (may have status prefix)
	SessionID  string // tmux session_id — stable target for attach/switch
	Project    string
	Branch     string
	Status     string
	Annotation string
	StartedAt  time.Time
	UpdatedAt  time.Time
}

// List returns a SessionInfo for every active agent tmux session.
func (s *Service) List() ([]SessionInfo, error) {
	records, err := s.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}

	var result []SessionInfo
	for _, r := range records {
		if r.SessionType != semconv.SessionTypeAgent {
			continue
		}
		info := SessionInfo{
			Name:       r.CanonicalName,
			Status:     r.Status,
			Annotation: r.Annotation,
		}
		if r.StartedAt != "" {
			info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
		}
		result = append(result, info)
	}
	return result, nil
}

// Show returns the SessionInfo for a single named tmux session, looked up by canonical name.
// Returns ErrSessionNotFound if no agent session with that canonical name exists.
func (s *Service) Show(name string) (*SessionInfo, error) {
	records, err := s.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == name && r.SessionType == semconv.SessionTypeAgent {
			info := &SessionInfo{
				Name:       r.CanonicalName,
				TmuxName:   r.Name,
				SessionID:  r.ID,
				Status:     r.Status,
				Annotation: r.Annotation,
			}
			if r.StartedAt != "" {
				info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
			}
			return info, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, name)
}

// Stop kills the named tmux session, resolving the actual session name by canonical name.
// Returns ErrSessionNotFound if no agent session with that canonical name exists.
func (s *Service) Stop(name string) error {
	records, err := s.tmux.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == name && r.SessionType == semconv.SessionTypeAgent {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
	}
	if err := s.tmux.KillSession(actualName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}
	return nil
}

// SetStatus transitions a session's status and updates the annotation.
// It resolves the actual tmux session name by canonical name.
// Errors are suppressed — this method always returns nil.
func (s *Service) SetStatus(name, status, annotation string) error {
	if name == "" {
		return nil
	}
	if status != semconv.StatusRunning && status != semconv.StatusWaiting {
		return nil
	}

	records, _ := s.tmux.ListSessions()
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == name && r.SessionType == semconv.SessionTypeAgent {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return nil // session not found, suppress
	}

	_ = s.tmux.SetOption(actualName, semconv.TmuxOptionStatus, status)
	_ = s.tmux.SetOption(actualName, semconv.TmuxOptionAnnotation, annotation)

	hasPrefix := strings.HasPrefix(actualName, semconv.StatusPrefix)
	if status == semconv.StatusRunning && hasPrefix {
		_ = s.tmux.RenameSession(actualName, strings.TrimPrefix(actualName, semconv.StatusPrefix))
	} else if status != semconv.StatusRunning && !hasPrefix {
		_ = s.tmux.RenameSession(actualName, semconv.StatusPrefix+actualName)
	}

	return nil
}
