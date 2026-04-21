package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/worktree"
)

func worktreeErr(cmd *cobra.Command, project, branch string, err error) error {
	switch {
	case errors.Is(err, worktree.ErrNotCloned):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s is not cloned. Run 'ch clone project %s' first.\n", project, project)
	case errors.Is(err, worktree.ErrWorktreeExists):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: worktree %s/%s already exists.\n", project, branch)
	case errors.Is(err, worktree.ErrWorktreeNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: worktree %s/%s not found. Run 'ch create worktree %s %s' first.\n", project, branch, project, branch)
	case errors.Is(err, worktree.ErrSessionRunning):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: session %s-%s is running. Stop it first or use --force.\n", project, branch)
	default:
		return err
	}
	os.Exit(1)
	return nil
}

func sessionErr(cmd *cobra.Command, err error) error {
	switch {
	case errors.Is(err, session.ErrSessionExists):
		var sesErr *session.SessionExistsError
		if errors.As(err, &sesErr) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: session %s/%s (%s) already exists. Attach with 'ch attach session %s %s'.\n", sesErr.Project, sesErr.Branch, sesErr.Type, sesErr.Project, sesErr.Branch)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
		}
	case errors.Is(err, session.ErrSessionNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, session.ErrPathNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, worktree.ErrNotCloned):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, worktree.ErrWorktreeNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	default:
		return err
	}
	os.Exit(1)
	return nil
}
