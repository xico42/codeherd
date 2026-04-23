package cmd

import (
	"fmt"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

// newWorktreeService returns a *worktree.Service for read-only paths
// (list, show). Write paths (create, delete) construct their own service
// inline because they need hooks bound to the project's config.
func newWorktreeService() *worktree.Service {
	return worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}

// newSessionService returns a *session.Service for read-only paths
// (list, show). Create/delete paths construct their own service inline
// with a project-bound hook. Declared as a var for test overriding.
var newSessionService = func() *session.Service {
	return session.NewService(tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}

// newProjectService returns a *project.Service for read-only paths
// (list, show). Clone constructs its own service inline with a
// project-bound hook.
func newProjectService() *project.Service {
	return project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})
}

// activeProfile returns the currently active profile name, or "" when
// profile mode is off. Safe when registry is nil.
func activeProfile() string {
	if registry == nil {
		return ""
	}
	return registry.Active
}

// showSessionForProfile dispatches Show/ShowByName based on whether a
// profile is active. Callers pass the logical (project, branch, type).
// ByName paths look up by canonical name (profile-qualified), which is
// always SessionName regardless of type — ShellSessionName differs only
// for the tmux display name. Errors are wrapped with %w so callers can
// still match against session sentinels via errors.Is.
func showSessionForProfile(svc *session.Service, project, branch, sessionType string) (*session.SessionInfo, error) {
	prof := activeProfile()
	if prof == "" {
		info, err := svc.Show(project, branch, sessionType)
		if err != nil {
			return nil, fmt.Errorf("showing session: %w", err)
		}
		return info, nil
	}
	info, err := svc.ShowByName(semconv.SessionName(prof, project, branch), sessionType)
	if err != nil {
		return nil, fmt.Errorf("showing session: %w", err)
	}
	return info, nil
}

// stopSessionForProfile dispatches Stop/StopByName based on the active
// profile.
func stopSessionForProfile(svc *session.Service, project, branch, sessionType string) error {
	prof := activeProfile()
	if prof == "" {
		if err := svc.Stop(project, branch, sessionType); err != nil {
			return fmt.Errorf("stopping session: %w", err)
		}
		return nil
	}
	if err := svc.StopByName(semconv.SessionName(prof, project, branch), sessionType); err != nil {
		return fmt.Errorf("stopping session: %w", err)
	}
	return nil
}

// listSessionsForProfile returns only sessions matching the active
// profile. With no active profile, all sessions are returned.
func listSessionsForProfile(svc *session.Service) ([]session.SessionInfo, error) {
	all, err := svc.List()
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	prof := activeProfile()
	if prof == "" {
		return all, nil
	}
	var out []session.SessionInfo
	for _, s := range all {
		if s.Profile == prof {
			out = append(out, s)
		}
	}
	return out, nil
}
