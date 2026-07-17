// Package herd is codeherd's domain. It owns projects, worktrees, and the
// tmux sessions running in them — three things that used to be three
// packages that could not see each other.
//
// The split cost us a class of defects. The session package had no config, so
// it could not know the active profile, so every profile decision moved up
// into its callers, and one of them rebuilt a session name without the
// profile and killed nothing. Here, identity lives in one place: a Ref
// obtained from Herd.Ref always carries the profile, and every session
// lookup is keyed on a Ref.
package herd

import (
	"fmt"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// SessionType distinguishes the two kinds of session codeherd runs. Both are
// first-class: they coexist for the same Ref and are addressed the same way.
type SessionType string

const (
	SessionTypeAgent SessionType = semconv.SessionTypeAgent
	SessionTypeShell SessionType = semconv.SessionTypeShell
)

// Status is an agent session's lifecycle state, stored on the tmux session.
type Status string

const (
	StatusRunning Status = semconv.StatusRunning
	StatusWaiting Status = semconv.StatusWaiting
)

// RemoteBranch is one remote-tracking branch. Aliased rather than redeclared:
// git.Runner returns these, and a conversion loop at the exec boundary would
// buy nothing.
type RemoteBranch = git.RemoteBranch

// Ref identifies a workspace — a project and branch, scoped to a profile.
//
// Branch is ALWAYS the identity branch: the branch the worktree was created
// for, which is what its sessions were named after. It is never the branch
// HEAD currently points at. Use Workspace.DisplayBranch for rendering.
//
// Obtain a Ref from Herd.Ref or from Workspace.Ref. Never build one by hand.
// Herd.Ref takes no profile argument, so the shortest path is the correct
// one; a hand-built herd.Ref{Project: p, Branch: b} is visibly missing a
// field under review, and that missing field is the bug this package exists
// to prevent.
type Ref struct {
	Profile string
	Project string
	Branch  string
}

// CanonicalName is the session name frozen at creation: the identity both
// session types share, and the key every tmux lookup matches on.
func (r Ref) CanonicalName() string {
	return semconv.SessionName(r.Profile, r.Project, r.Branch)
}

// tmuxName is the actual tmux session name for a type. It differs from
// CanonicalName only for shell sessions, which carry a ~sh suffix so the two
// types can coexist.
func (r Ref) tmuxName(t SessionType) string {
	if t == SessionTypeShell {
		return semconv.ShellSessionName(r.Profile, r.Project, r.Branch)
	}
	return semconv.SessionName(r.Profile, r.Project, r.Branch)
}

// Deps holds the exec-boundary runners. Two fields; revisit options at three.
type Deps struct {
	Tmux tmux.Runner
	Git  git.Runner
}

// Herd is the domain: config, the active profile, and the runners.
type Herd struct {
	cfg         *config.Config
	profile     string
	profilesDir string
	profiles    []string
	tmux        *tmux.Client
	git         git.Runner

	// newHook builds the hook dispatcher for one project's hook config.
	//
	// It is a defaulted field, not a constructor parameter, and that is
	// deliberate. Binding hooks at construction is what killed dependency
	// injection in the TUI: the actions needed a project-bound hook, so
	// every one of them rebuilt its own service and Model.sesSvc became a
	// field that was assigned and never read. Herd holds cfg, so it can
	// resolve hooks per operation instead. Tests override this field.
	newHook func(config.HooksConfig) hooks.Hook
}

// New builds a Herd for the given config and profile registry. A nil
// registry means profile mode is off — that is what config.Load returns in
// the common case, so New must accept it.
func New(cfg *config.Config, registry *config.ProfileRegistry, deps Deps) *Herd {
	h := &Herd{
		cfg:     cfg,
		tmux:    tmux.NewClient(deps.Tmux),
		git:     deps.Git,
		newHook: func(hc config.HooksConfig) hooks.Hook { return hooks.New(hc) },
	}
	if registry != nil {
		h.profile = registry.Active
		h.profilesDir = registry.ProfilesDir
		h.profiles = registry.Names
	}
	return h
}

// Ref supplies the active profile. This is the only sanctioned way to mint a
// Ref from a (project, branch) pair.
func (h *Herd) Ref(project, branch string) Ref {
	return Ref{Profile: h.profile, Project: project, Branch: branch}
}

// Config exposes the config this Herd was built for. Front ends need it for
// agent lookup and project enumeration.
func (h *Herd) Config() *config.Config { return h.cfg }

// Profile returns the active profile name, or "" when profile mode is off.
func (h *Herd) Profile() string { return h.profile }

// Profiles returns every discovered profile name, nil when profile mode is off.
func (h *Herd) Profiles() []string { return h.profiles }

// WithProfile returns a new Herd scoped to a different profile, sharing this
// one's runners. The receiver is unchanged.
func (h *Herd) WithProfile(name string) (*Herd, error) {
	if h.profilesDir == "" {
		return nil, fmt.Errorf("cannot switch to profile %q: profiles are not enabled", name)
	}
	cfg, err := config.LoadProfile(h.profilesDir, name)
	if err != nil {
		return nil, fmt.Errorf("loading profile %s: %w", name, err)
	}
	next := *h
	next.cfg = cfg
	next.profile = name
	return &next, nil
}

// hookFor returns the hook dispatcher for a project. It is total: an
// unconfigured project yields a dispatcher with no hooks, which fires nothing.
func (h *Herd) hookFor(project string) hooks.Hook {
	return h.newHook(h.cfg.Projects[project].Hooks)
}
