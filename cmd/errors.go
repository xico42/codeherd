package cmd

import (
	"errors"
	"fmt"

	"github.com/xico42/codeherd/internal/herd"
)

// herdErr is the CLI's single error translator. Every command funnels its
// domain errors through here, matching herd sentinels and nothing else
// (the one error vocabulary — herd owns the sentinels, the front end owns
// the presentation).
//
// It RETURNS the error rather than printing and calling os.Exit. Execute
// prints it once (prefixed "Error: ") and main exits non-zero. The previous
// shape printed and called os.Exit(1) inside RunE, which made the trailing
// `return nil` unreachable and bypassed Execute's error path.
//
// project and branch supply context for the friendly messages; pass the
// identity values in scope at the call site (or ws.Ref.Project /
// ws.Ref.Branch). For ErrSessionExists the context is read from the typed
// *herd.SessionExistsError instead, so the two positional args are ignored
// on that branch.
func herdErr(project, branch string, err error) error {
	switch {
	case errors.Is(err, herd.ErrNotCloned):
		return fmt.Errorf("%s is not cloned. Run 'ch clone project %s' first", project, project)
	case errors.Is(err, herd.ErrWorktreeExists):
		return fmt.Errorf("worktree %s/%s already exists", project, branch)
	case errors.Is(err, herd.ErrWorktreeNotFound):
		return fmt.Errorf("worktree %s/%s not found. Run 'ch create worktree %s %s' first", project, branch, project, branch)
	case errors.Is(err, herd.ErrSessionRunning):
		return fmt.Errorf("session %s-%s is running. Stop it first or use --force", project, branch)
	case errors.Is(err, herd.ErrSessionExists):
		var sesErr *herd.SessionExistsError
		if errors.As(err, &sesErr) {
			return fmt.Errorf("session %s/%s (%s) already exists. Attach with 'ch attach session %s %s'",
				sesErr.Ref.Project, sesErr.Ref.Branch, sesErr.Type, sesErr.Ref.Project, sesErr.Ref.Branch)
		}
		return err
	default:
		return err
	}
}
