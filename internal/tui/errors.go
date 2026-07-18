package tui

import (
	"errors"
	"fmt"

	"github.com/xico42/codeherd/internal/herd"
)

// humanize maps a herd domain error to a concise, user-facing status line for
// the TUI. It matches the same herd sentinels the CLI translator does (one
// error vocabulary) so the dashboard stops surfacing raw internal error
// strings. Unknown errors fall through to their own message.
//
// The TUI errMsg carries no project/branch, so most messages are
// context-free; ErrSessionExists is the exception — its typed error carries
// the Ref, so the message names the session.
func humanize(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, herd.ErrNotCloned):
		return "Project is not cloned — clone it first."
	case errors.Is(err, herd.ErrWorktreeExists):
		return "Worktree already exists."
	case errors.Is(err, herd.ErrWorktreeNotFound):
		return "Worktree not found."
	case errors.Is(err, herd.ErrSessionExists):
		var se *herd.SessionExistsError
		if errors.As(err, &se) {
			return fmt.Sprintf("Session %s/%s (%s) already exists.", se.Ref.Project, se.Ref.Branch, se.Type)
		}
		return "Session already exists."
	case errors.Is(err, herd.ErrSessionNotFound):
		return "No such session."
	case errors.Is(err, herd.ErrSessionRunning):
		return "Session is running — stop it first."
	case errors.Is(err, herd.ErrPathNotFound):
		return "Worktree path not found."
	case errors.Is(err, herd.ErrMainWorktree):
		return "Cannot delete the main worktree."
	default:
		return err.Error()
	}
}
