package session

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

var (
	ErrSessionExists   = errors.New("session already exists")
	ErrSessionNotFound = errors.New("session not found")
	ErrPathNotFound    = errors.New("worktree path not found")
)

// SessionExistsError is returned by Start when a tmux session for the same
// (project, branch, type) already exists. It wraps ErrSessionExists for
// errors.Is compatibility and carries Project/Branch/Type for structured
// access by callers (e.g. to print an attach hint).
type SessionExistsError struct {
	Project string
	Branch  string
	Type    string
}

func (e *SessionExistsError) Error() string {
	return fmt.Sprintf("%s: %s/%s (%s)", ErrSessionExists.Error(), e.Project, e.Branch, e.Type)
}

func (e *SessionExistsError) Unwrap() error {
	return ErrSessionExists
}

// Service manages codeherd tmux sessions and their persisted state.
type Service struct {
	tmux *tmux.Client
	hook hooks.Hook
}

// NewService creates a Service using the given tmux client.
func NewService(tmux *tmux.Client, hook hooks.Hook) *Service {
	return &Service{tmux: tmux, hook: hook}
}

// StartRequest holds parameters for starting a new session.
type StartRequest struct {
	Project  string
	Branch   string
	Path     string
	CloneDir string // main git clone for the project (exposed as CODEHERD_CLONE_DIR)
	Type     string // semconv.SessionTypeAgent or SessionTypeShell; defaults to SessionTypeAgent
	Cmd      string
	Env      map[string]string
	Attach   bool
	Profile  string // "" when profiles are disabled
}

// Start creates a new detached tmux session for the given project/branch and
// sets @codeherd_status and @codeherd_started_at tmux options on the new session.
// The session command runs with these env vars, which override any conflicting
// keys in req.Env:
//
//   - CODEHERD_SESSION       canonical session name
//   - CODEHERD_PROJECT       project name
//   - CODEHERD_BRANCH        branch name
//   - CODEHERD_CLONE_DIR     main git clone path (when req.CloneDir is set)
//   - CODEHERD_WORKTREE_PATH worktree root
//   - CODEHERD_PROFILE       profile name (only when req.Profile is non-empty)
//
// Returns ErrSessionExists if a session with the same canonical name and type already exists.
// Returns ErrPathNotFound if Path does not exist on disk.
func (s *Service) Start(req StartRequest) (string, error) {
	if req.Type == "" {
		req.Type = semconv.SessionTypeAgent
	}

	// Canonical name is <[profile-]project-branch>; tmux name differs by type.
	canonicalName := semconv.SessionName(req.Profile, req.Project, req.Branch)
	var tmuxName string
	if req.Type == semconv.SessionTypeShell {
		tmuxName = semconv.ShellSessionName(req.Profile, req.Project, req.Branch)
	} else {
		tmuxName = canonicalName
	}

	// Scope existence check to (canonical name, type) pair so agent and shell sessions coexist.
	records, err := s.tmux.ListSessions()
	if err != nil {
		return "", fmt.Errorf("checking session: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == canonicalName && r.SessionType == req.Type {
			return "", &SessionExistsError{Project: req.Project, Branch: req.Branch, Type: req.Type}
		}
	}

	if _, err := os.Stat(req.Path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrPathNotFound, req.Path)
		}
		return "", fmt.Errorf("checking path: %w", err)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      req.Project,
		semconv.HookAttrBranch:       req.Branch,
		semconv.HookAttrWorktreePath: req.Path,
		semconv.HookAttrSessionName:  canonicalName,
	}

	if err := s.hook.Trigger(semconv.HookPreSession, attrs, req.Path); err != nil {
		return "", fmt.Errorf("pre-session hook: %w", err)
	}

	env := make(map[string]string)
	for k, v := range req.Env {
		env[k] = v
	}
	// Codeherd-stamped vars win over user-supplied Env.
	env[semconv.SessionEnvVar] = canonicalName
	env[semconv.HookAttrProject] = req.Project
	env[semconv.HookAttrBranch] = req.Branch
	env[semconv.HookAttrWorktreePath] = req.Path
	if req.CloneDir != "" {
		env[semconv.HookAttrCloneDir] = req.CloneDir
	}
	if req.Profile != "" {
		env[semconv.EnvProfile] = req.Profile
	}

	// Capture the stable session ID atomically at creation; a separate
	// display-message round-trip would race with short-lived commands (e.g. "true").
	id, err := s.tmux.NewSessionWithEnv(tmuxName, req.Path, env, req.Cmd)
	if err != nil {
		return "", fmt.Errorf("creating tmux session: %w", err)
	}

	now := time.Now().UTC()
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionStatus, semconv.StatusRunning)
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionStartedAt, now.Format(time.RFC3339))
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionCanonicalName, canonicalName)
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionSessionType, req.Type)
	if req.Profile != "" {
		_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionProfile, req.Profile)
	}

	if err := s.hook.Trigger(semconv.HookPostSession, attrs, req.Path); err != nil {
		return "", fmt.Errorf("post-session hook: %w", err)
	}

	return id, nil
}

// SessionInfo holds display information about a tmux session.
type SessionInfo struct {
	Name       string
	TmuxName   string // actual tmux session name (may have status prefix)
	SessionID  string // tmux session_id — stable target for attach/switch
	Type       string // semconv.SessionTypeAgent or SessionTypeShell
	Project    string
	Branch     string
	Status     string
	Annotation string
	StartedAt  time.Time
	UpdatedAt  time.Time
	Profile    string
}

// List returns a SessionInfo for every active tmux session, including both agent and shell types.
func (s *Service) List() ([]SessionInfo, error) {
	records, err := s.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}

	var result []SessionInfo
	for _, r := range records {
		info := SessionInfo{
			Name:       r.CanonicalName,
			Type:       r.SessionType,
			Status:     r.Status,
			Annotation: r.Annotation,
			Profile:    r.Profile,
		}
		if r.StartedAt != "" {
			info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
		}
		result = append(result, info)
	}
	return result, nil
}

// Show returns the SessionInfo for a session identified by project, branch, and type.
// Empty sessionType defaults to semconv.SessionTypeAgent.
// Returns ErrSessionNotFound if no matching session exists.
func (s *Service) Show(project, branch, sessionType string) (*SessionInfo, error) {
	if sessionType == "" {
		sessionType = semconv.SessionTypeAgent
	}
	canonicalName := semconv.SessionName("", project, branch)
	records, err := s.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == canonicalName && r.SessionType == sessionType {
			info := &SessionInfo{
				Name:       r.CanonicalName,
				TmuxName:   r.Name,
				SessionID:  r.ID,
				Type:       r.SessionType,
				Status:     r.Status,
				Annotation: r.Annotation,
				Profile:    r.Profile,
			}
			if r.StartedAt != "" {
				info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
			}
			return info, nil
		}
	}
	return nil, fmt.Errorf("%w: %s/%s (%s)", ErrSessionNotFound, project, branch, sessionType)
}

// ShowByName returns the SessionInfo for the session whose canonical
// name + type match exactly. Returns ErrSessionNotFound otherwise.
func (s *Service) ShowByName(name, sessionType string) (*SessionInfo, error) {
	if sessionType == "" {
		sessionType = semconv.SessionTypeAgent
	}
	records, err := s.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == name && r.SessionType == sessionType {
			info := &SessionInfo{
				Name:       r.CanonicalName,
				TmuxName:   r.Name,
				SessionID:  r.ID,
				Type:       r.SessionType,
				Status:     r.Status,
				Annotation: r.Annotation,
				Profile:    r.Profile,
			}
			if r.StartedAt != "" {
				info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
			}
			return info, nil
		}
	}
	return nil, fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, name, sessionType)
}

// StopByName kills the session whose canonical name + type match exactly.
// Empty sessionType defaults to semconv.SessionTypeAgent.
// Returns ErrSessionNotFound if no matching session exists.
func (s *Service) StopByName(name, sessionType string) error {
	if sessionType == "" {
		sessionType = semconv.SessionTypeAgent
	}
	records, err := s.tmux.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == name && r.SessionType == sessionType {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, name, sessionType)
	}
	if err := s.tmux.KillSession(actualName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}
	return nil
}

// Stop kills the tmux session identified by project, branch, and type.
// Empty sessionType defaults to semconv.SessionTypeAgent.
// Returns ErrSessionNotFound if no matching session exists.
func (s *Service) Stop(project, branch, sessionType string) error {
	if sessionType == "" {
		sessionType = semconv.SessionTypeAgent
	}
	canonicalName := semconv.SessionName("", project, branch)
	records, err := s.tmux.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == canonicalName && r.SessionType == sessionType {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return fmt.Errorf("%w: %s/%s (%s)", ErrSessionNotFound, project, branch, sessionType)
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
