package herd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// Handle is a live session.
type Handle struct {
	ID         string // tmux session_id ("$1") — stable across renames
	Canonical  string // @codeherd_canonical_name — the frozen identity, the match key
	Ref        Ref
	Type       SessionType
	TmuxName   string // current tmux name; may carry the ⚡ status prefix
	Status     Status
	Annotation string
	StartedAt  time.Time
}

// LaunchOpts configures a session start. The zero value starts the default
// agent, detached.
type LaunchOpts struct {
	Type   SessionType // zero value means SessionTypeAgent
	Agent  string      // agent name; "" means defaults.agent. Ignored for shell.
	Attach bool        // front ends read Handle.ID and attach themselves
}

// StopOpts selects which of a Ref's sessions to stop.
type StopOpts struct {
	Type SessionType // ignored when All is true
	All  bool        // stop every type for this Ref
}

// Launch starts a detached tmux session for ref and returns its handle.
//
// The session command runs with these env vars, which override conflicting
// keys in the agent's configured Env:
//
//   - CODEHERD_SESSION       canonical session name
//   - CODEHERD_PROJECT       project name
//   - CODEHERD_BRANCH        identity branch
//   - CODEHERD_CLONE_DIR     main git clone path
//   - CODEHERD_WORKTREE_PATH worktree root
//   - CODEHERD_PROFILE       profile name (only when a profile is active)
//
// Returns *SessionExistsError if a session for this ref and type is already
// running, and ErrPathNotFound if the worktree does not exist on disk.
func (h *Herd) Launch(ref Ref, opts LaunchOpts) (Handle, error) {
	if opts.Type == "" {
		opts.Type = SessionTypeAgent
	}

	// Scope the existence check to (ref, type) so agent and shell sessions coexist.
	switch _, err := h.Resolve(ref, opts.Type); {
	case err == nil:
		return Handle{}, &SessionExistsError{Ref: ref, Type: opts.Type}
	case !errors.Is(err, ErrSessionNotFound):
		return Handle{}, err
	}

	path, err := h.worktreePath(ref)
	if err != nil {
		return Handle{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Handle{}, fmt.Errorf("%w: %s", ErrPathNotFound, path)
		}
		return Handle{}, fmt.Errorf("checking worktree path: %w", err)
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return Handle{}, err
	}

	cmd, env, err := h.sessionCommand(opts)
	if err != nil {
		return Handle{}, err
	}

	canonical := ref.CanonicalName()
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrWorktreePath: path,
		semconv.HookAttrSessionName:  canonical,
	}
	if err := hook.Trigger(semconv.HookPreSession, attrs, path); err != nil {
		return Handle{}, fmt.Errorf("pre-session hook: %w", err)
	}

	sessionEnv := make(map[string]string, len(env)+6)
	for k, v := range env {
		sessionEnv[k] = v
	}
	// Codeherd-stamped vars win over user-supplied Env.
	sessionEnv[semconv.SessionEnvVar] = canonical
	sessionEnv[semconv.HookAttrProject] = ref.Project
	sessionEnv[semconv.HookAttrBranch] = ref.Branch
	sessionEnv[semconv.HookAttrWorktreePath] = path
	if cloneDir != "" {
		sessionEnv[semconv.HookAttrCloneDir] = cloneDir
	}
	if ref.Profile != "" {
		sessionEnv[semconv.EnvProfile] = ref.Profile
	}

	// Capture the session ID atomically at creation; a separate
	// display-message round-trip would race with short-lived commands.
	tmuxName := ref.tmuxName(opts.Type)
	id, err := h.tmux.NewSessionWithEnv(tmuxName, path, sessionEnv, cmd)
	if err != nil {
		return Handle{}, fmt.Errorf("creating tmux session: %w", err)
	}

	now := time.Now().UTC()
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionStatus, semconv.StatusRunning)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionStartedAt, now.Format(time.RFC3339))
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionCanonicalName, canonical)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionSessionType, string(opts.Type))
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionBranch, ref.Branch)
	_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionProject, ref.Project)
	if ref.Profile != "" {
		_ = h.tmux.SetOption(tmuxName, semconv.TmuxOptionProfile, ref.Profile)
	}

	if err := hook.Trigger(semconv.HookPostSession, attrs, path); err != nil {
		return Handle{}, fmt.Errorf("post-session hook: %w", err)
	}

	return Handle{
		ID:        id,
		Ref:       ref,
		Type:      opts.Type,
		TmuxName:  tmuxName,
		Status:    StatusRunning,
		StartedAt: now,
	}, nil
}

// sessionCommand resolves the command and env a session runs with. A shell
// session runs $SHELL; an agent session runs its configured command.
func (h *Herd) sessionCommand(opts LaunchOpts) (cmd string, env map[string]string, err error) {
	if opts.Type == SessionTypeShell {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		return shell, nil, nil
	}
	name := opts.Agent
	if name == "" {
		name = h.cfg.Defaults.Agent
	}
	if name == "" {
		return "", nil, fmt.Errorf("no agent specified; use --agent or set defaults.agent in config")
	}
	agent, err := h.cfg.AgentByName(name)
	if err != nil {
		return "", nil, fmt.Errorf("resolving agent: %w", err)
	}
	return agent.Command(), agent.Env, nil
}

// Resolve returns the live handle for a ref and type.
// Returns ErrSessionNotFound if no such session is running.
func (h *Herd) Resolve(ref Ref, t SessionType) (Handle, error) {
	if t == "" {
		t = SessionTypeAgent
	}
	all, err := h.handles()
	if err != nil {
		return Handle{}, err
	}
	canonical := ref.CanonicalName()
	for _, hd := range all {
		if hd.Canonical == canonical && hd.Type == t {
			return hd, nil
		}
	}
	return Handle{}, fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, canonical, t)
}

// Sessions returns every live session belonging to the active profile. With
// no active profile, that is every session codeherd started.
func (h *Herd) Sessions() ([]Handle, error) {
	all, err := h.handles()
	if err != nil {
		return nil, err
	}
	var out []Handle
	for _, hd := range all {
		if hd.Ref.Profile == h.profile {
			out = append(out, hd)
		}
	}
	return out, nil
}

// StopSessions kills the sessions matching ref and returns the handles it
// stopped. Sessions are killed by tmux session ID, never by a rebuilt name.
// Stopping nothing is not an error — Teardown calls this unconditionally.
func (h *Herd) StopSessions(ref Ref, opts StopOpts) ([]Handle, error) {
	all, err := h.handles()
	if err != nil {
		return nil, err
	}
	if opts.Type == "" && !opts.All {
		opts.Type = SessionTypeAgent
	}

	canonical := ref.CanonicalName()
	var stopped []Handle
	for _, hd := range all {
		if hd.Canonical != canonical {
			continue
		}
		if !opts.All && hd.Type != opts.Type {
			continue
		}
		if err := h.tmux.KillSession(hd.ID); err != nil {
			return stopped, fmt.Errorf("killing session %s: %w", hd.Canonical, err)
		}
		stopped = append(stopped, hd)
	}
	return stopped, nil
}

// SetStatus transitions an agent session's status and annotation, addressing
// it by canonical name.
//
// This is the one operation that does not take a Ref, and it is deliberate:
// `ch plugin handle-claude` receives a bare name from $CODEHERD_SESSION and
// cannot recover a Ref from it — the profile prefix is ambiguous, since
// work-myapp-feat could be profile "work" + project "myapp", or a project
// literally named "work-myapp". One narrow escape hatch beats re-exporting
// name resolution.
//
// Errors are suppressed: a hook must never fail the agent it is reporting on.
func (h *Herd) SetStatus(canonicalName string, status Status, annotation string) error {
	if canonicalName == "" {
		return nil
	}
	if status != StatusRunning && status != StatusWaiting {
		return nil
	}

	records, _ := h.tmux.ListSessions()
	actualName := ""
	for _, r := range records {
		if r.CanonicalName == canonicalName && SessionType(r.SessionType) == SessionTypeAgent {
			actualName = r.Name
			break
		}
	}
	if actualName == "" {
		return nil // session not found — suppress
	}

	_ = h.tmux.SetOption(actualName, semconv.TmuxOptionStatus, string(status))
	_ = h.tmux.SetOption(actualName, semconv.TmuxOptionAnnotation, annotation)

	hasPrefix := strings.HasPrefix(actualName, semconv.StatusPrefix)
	if status == StatusRunning && hasPrefix {
		_ = h.tmux.RenameSession(actualName, strings.TrimPrefix(actualName, semconv.StatusPrefix))
	} else if status != StatusRunning && !hasPrefix {
		_ = h.tmux.RenameSession(actualName, semconv.StatusPrefix+actualName)
	}
	return nil
}

// handles lists every codeherd session tmux knows about, across all profiles.
// It is the single place a tmux record becomes a Handle, which is why the
// backward-compat project recovery and self-heal live in handleFrom: every
// read path funnels through here, so none of them can disagree about identity.
func (h *Herd) handles() ([]Handle, error) {
	records, err := h.tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}
	out := make([]Handle, 0, len(records))
	for _, r := range records {
		if r.CanonicalName == "" {
			continue // not a codeherd session
		}
		out = append(out, h.handleFrom(r))
	}
	return out, nil
}

// resolveProject finds the configured project whose canonical session name
// matches the stored one, given the (stored) profile and branch. Profile and
// branch are known exactly, so the project is the only unknown and the match
// is unambiguous. It validates against real config rather than string-
// splitting the name, so a project no longer in config yields "", false.
func resolveProject(cfg *config.Config, profile, branch, canonical string) (string, bool) {
	for name := range cfg.Projects {
		if semconv.SessionName(profile, name, branch) == canonical {
			return name, true
		}
	}
	return "", false
}

func (h *Herd) handleFrom(r tmux.SessionRecord) Handle {
	hd := Handle{
		ID:         r.ID,
		Canonical:  r.CanonicalName,
		Ref:        Ref{Profile: r.Profile, Project: r.Project, Branch: r.Branch},
		Type:       SessionType(r.SessionType),
		TmuxName:   r.Name,
		Status:     Status(r.Status),
		Annotation: r.Annotation,
	}
	if r.StartedAt != "" {
		hd.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}

	// Backward compatibility: sessions created before @codeherd_project existed
	// carry no project stamp. Recover it from the frozen canonical name and
	// stamp it, so the session heals to first-class on first observation.
	// Idempotent — once stamped, future reads take r.Project directly and skip
	// this path.
	if r.Project == "" && r.CanonicalName != "" {
		if project, ok := resolveProject(h.cfg, r.Profile, r.Branch, r.CanonicalName); ok {
			hd.Ref.Project = project
			_ = h.tmux.SetOption(r.Name, semconv.TmuxOptionProject, project)
		}
	}
	return hd
}
