package herd

import (
	"fmt"
	"sort"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// repoPath returns the filesystem-relative path derived from a project's repo
// URL, e.g. github.com/user/myapp.
func (h *Herd) repoPath(project string) (string, error) {
	p, ok := h.cfg.Projects[project]
	if !ok {
		return "", fmt.Errorf("project %q is not configured", project)
	}
	rp, err := config.RepoPath(p.Repo)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL %q: %w", p.Repo, err)
	}
	return rp, nil
}

// cloneDir returns the main git clone directory for a project.
func (h *Herd) cloneDir(project string) (string, error) {
	rp, err := h.repoPath(project)
	if err != nil {
		return "", err
	}
	return semconv.CloneDir(h.cfg.Defaults.ProjectsDir, rp), nil
}

// worktreesRoot returns the directory holding a project's worktrees.
func (h *Herd) worktreesRoot(project string) (string, error) {
	rp, err := h.repoPath(project)
	if err != nil {
		return "", err
	}
	return semconv.WorktreesRoot(h.cfg.Defaults.ProjectsDir, rp), nil
}

// worktreePath returns the filesystem path for a ref's worktree. It derives
// from Ref.Branch — the identity branch — so it agrees with the session name
// by construction.
//
// The main worktree lives at the clone dir itself, not under __worktrees/, so
// the <clone>__worktrees/<branch> formula is wrong for the default branch. We
// resolve against the live worktree list using the same identity function
// List/workspaceFrom use, so the operate paths and the listing can never
// disagree about where the main worktree is — feeding a listed Ref back into
// Launch or Teardown must land on the same directory List reported. When no
// live worktree matches — the worktree does not exist yet, or git is
// unavailable — we fall back to the formula, which is correct for every
// non-main branch and yields a sensible not-found path for callers that stat.
func (h *Herd) worktreePath(ref Ref) (string, error) {
	rp, err := h.repoPath(ref.Project)
	if err != nil {
		return "", err
	}
	cloneDir := semconv.CloneDir(h.cfg.Defaults.ProjectsDir, rp)
	if h.git != nil {
		if infos, err := h.git.List(cloneDir); err == nil {
			defaultBranch := h.cfg.Projects[ref.Project].DefaultBranch
			for _, wt := range infos {
				identity := semconv.WorktreeIdentityBranch(wt.Path, cloneDir, defaultBranch, wt.Branch)
				if semconv.FlattenBranch(identity) == semconv.FlattenBranch(ref.Branch) {
					return wt.Path, nil
				}
			}
		}
	}
	return semconv.WorktreePath(h.cfg.Defaults.ProjectsDir, rp, ref.Branch), nil
}

// projectNames returns sorted project names, or just the named one after
// validating it exists.
func (h *Herd) projectNames(project string) ([]string, error) {
	if project != "" {
		if _, ok := h.cfg.Projects[project]; !ok {
			return nil, fmt.Errorf("project %q is not configured", project)
		}
		return []string{project}, nil
	}
	names := make([]string, 0, len(h.cfg.Projects))
	for name := range h.cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
