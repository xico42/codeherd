package cmd

import (
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/project"
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
