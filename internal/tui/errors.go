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
	var se *herd.SessionExistsError
	if errors.As(err, &se) {
		return fmt.Sprintf("Session %s/%s (%s) already exists.", se.Ref.Project, se.Ref.Branch, se.Type)
	}
	for _, s := range sentinelMessages {
		if errors.Is(err, s.err) {
			return s.msg
		}
	}
	return err.Error()
}

// sentinelMessages are the status lines for the herd sentinels, checked in
// order.
var sentinelMessages = []struct {
	err error
	msg string
}{
	{herd.ErrNotCloned, "Project is not cloned — clone it first."},
	{herd.ErrWorktreeExists, "Worktree already exists."},
	{herd.ErrWorktreeNotFound, "Worktree not found."},
	{herd.ErrSessionExists, "Session already exists."},
	{herd.ErrSessionNotFound, "No such session."},
	{herd.ErrSessionRunning, "Session is running — stop it first."},
	{herd.ErrPathNotFound, "Worktree path not found."},
	{herd.ErrMainWorktree, "Cannot delete the main worktree."},
}
