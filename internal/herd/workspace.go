package herd

import (
	"errors"
	"fmt"
	"os"

	"github.com/xico42/codeherd/internal/filecopy"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/semconv"
)

// Workspace is a worktree together with its sessions — the domain object the
// old split could not express.
type Workspace struct {
	// Ref is identity. Feed it back into any operation. It survives a
	// diverged HEAD, a profile switch, and a rename.
	Ref    Ref
	Path   string
	IsMain bool // true for the main clone dir

	// DisplayBranch is what a front end should render: the branch HEAD is
	// actually on. It is NOT identity and must never be fed back in — that
	// round-trip is what orphaned an agent against a deleted worktree.
	DisplayBranch string

	// HeadHint is "detached", "on <branch>", or "" when HEAD agrees with Ref.
	HeadHint string

	// Agent and Shell are nil when that session type is not running.
	Agent *Handle
	Shell *Handle
}

// EnsureOpts configures workspace creation. The zero value creates the
// worktree from the project's default branch and provisions nothing.
type EnsureOpts struct {
	AutoClone  bool   // clone the project first if it is not cloned
	Provision  bool   // run file copy + .herd templates after creating
	StartPoint string // --from: base the new branch on this ref
	Track      string // --track: "[<remote>/]<branch>"; derives the local name when Ref.Branch is ""
}

// TeardownOpts configures workspace deletion.
type TeardownOpts struct {
	Force bool // kill running sessions instead of refusing
}

// EnsureWorkspace makes the workspace for ref exist: clone if asked, create
// the worktree if missing, provision if asked. It is idempotent on the clone
// but not on the worktree — an existing worktree returns ErrWorktreeExists.
//
// The returned Workspace.Ref is authoritative: with Track, the local branch
// is derived from the remote ref and may differ from the ref passed in.
func (h *Herd) EnsureWorkspace(ref Ref, opts EnsureOpts) (Workspace, error) {
	if opts.StartPoint != "" && opts.Track != "" {
		return Workspace{}, errors.New("cannot combine a start point with a tracking ref")
	}

	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return Workspace{}, err
	}
	if opts.AutoClone {
		// Already cloned is the normal case, not a failure.
		if err := h.Clone(ref.Project); err != nil && !errors.Is(err, ErrAlreadyCloned) {
			return Workspace{}, err
		}
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return Workspace{}, fmt.Errorf("%w: %s", ErrNotCloned, ref.Project)
	}

	// A tracking ref decides the local branch name, so resolve it before the
	// ref is used for anything path-shaped.
	remoteRef := ""
	if opts.Track != "" {
		remotes, _ := h.git.Remotes(cloneDir)
		remote, remoteBranch, _ := git.ParseRef(remotes, opts.Track)
		if ref.Branch == "" {
			ref.Branch = remoteBranch
		}
		remoteRef = remote + "/" + remoteBranch
		if has, _ := h.git.HasLocalBranch(cloneDir, ref.Branch); has {
			return Workspace{}, fmt.Errorf("%w: %s", ErrLocalBranchExists, ref.Branch)
		}
		if err := h.git.Fetch(cloneDir, remote, remoteBranch); err != nil {
			return Workspace{}, fmt.Errorf("fetching %s: %w", remoteRef, err)
		}
	}

	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return Workspace{}, err
	}
	if _, err := os.Stat(wtPath); err == nil {
		return Workspace{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, ref.Project, ref.Branch)
	}

	p := h.cfg.Projects[ref.Project]
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: wtPath,
	}
	if err := hook.Trigger(semconv.HookPreWorktree, attrs, wtPath); err != nil {
		return Workspace{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	root, err := h.worktreesRoot(ref.Project)
	if err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	if err := h.addWorktree(ref, cloneDir, wtPath, remoteRef, opts); err != nil {
		return Workspace{}, err
	}

	if err := hook.Trigger(semconv.HookPostWorktree, attrs, wtPath); err != nil {
		return Workspace{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	if opts.Provision {
		if err := h.Provision(ref); err != nil {
			return Workspace{}, err
		}
	}

	return Workspace{
		Ref:           ref,
		Path:          wtPath,
		IsMain:        wtPath == cloneDir,
		DisplayBranch: ref.Branch,
	}, nil
}

// addWorktree runs the git call that actually creates the worktree. The three
// shapes were three near-identical 50-line methods; only this switch differed.
func (h *Herd) addWorktree(ref Ref, cloneDir, wtPath, remoteRef string, opts EnsureOpts) error {
	switch {
	case opts.Track != "":
		if err := h.git.AddTracking(cloneDir, wtPath, ref.Branch, remoteRef); err != nil {
			return fmt.Errorf("creating tracking worktree for %s: %w", remoteRef, err)
		}
		return nil

	case opts.StartPoint != "":
		startPoint := h.freshenStartPoint(cloneDir, opts.StartPoint)
		if err := h.git.AddNewBranchFrom(cloneDir, wtPath, ref.Branch, startPoint); err != nil {
			return fmt.Errorf("creating worktree from %s: %w", startPoint, err)
		}
		return nil

	default:
		// Try checking out an existing branch; fall back to branching from
		// the project's default.
		addErr := h.git.Add(cloneDir, wtPath, ref.Branch)
		if addErr == nil {
			return nil
		}
		src := h.cfg.Projects[ref.Project].DefaultBranch
		if src == "" {
			src = "main"
		}
		startPoint := h.freshenStartPoint(cloneDir, src)
		if err := h.git.AddNewBranchFrom(cloneDir, wtPath, ref.Branch, startPoint); err != nil {
			return fmt.Errorf("failed to create worktree (add: %v; add -b from %s: %w)", addErr, startPoint, err)
		}
		return nil
	}
}

// freshenStartPoint fetches updates for the source ref and returns the start
// point a new branch should be based on. It prefers a fast-forwarded local
// branch (to preserve un-pushed commits), falling back to the remote-tracking
// ref, or the raw ref when the source is not on a remote (tags, SHAs,
// local-only branches). All git failures here are best-effort.
func (h *Herd) freshenStartPoint(cloneDir, src string) string {
	remotes, _ := h.git.Remotes(cloneDir)
	remote, branch, explicit := git.ParseRef(remotes, src)
	if explicit {
		_ = h.git.Fetch(cloneDir, remote, branch)
		return src
	}
	if err := h.git.Fetch(cloneDir, "origin", src); err != nil {
		return src
	}
	if has, _ := h.git.HasLocalBranch(cloneDir, src); has {
		_ = h.git.FastForward(cloneDir, "origin", src)
		return src
	}
	return "origin/" + src
}

// Provision runs file copy and .herd template processing for a workspace.
//
// The template context is built from ref in one place, which is what kills
// the divergence where `ch create session` rendered a profile-qualified
// SessionName into a .herd file while `ch create worktree`, `ch template`,
// and the TUI rendered a profile-blind one — for the same worktree.
func (h *Herd) Provision(ref Ref) error {
	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return err
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return err
	}

	p := h.cfg.Projects[ref.Project]
	hook := h.hookFor(ref.Project)
	attrs := map[string]string{
		semconv.HookAttrProject:      ref.Project,
		semconv.HookAttrBranch:       ref.Branch,
		semconv.HookAttrWorktreePath: wtPath,
	}

	if len(p.Files) > 0 {
		if err := filecopy.New(hook).Copy(p.Files, cloneDir, wtPath, attrs); err != nil {
			return fmt.Errorf("copying files: %w", err)
		}
	}

	if _, err := herdtemplate.New(hook).Process(herdtemplate.ProcessContext{
		Project:      ref.Project,
		Branch:       ref.Branch,
		WorktreePath: wtPath,
		SessionName:  ref.CanonicalName(),
	}, attrs); err != nil {
		return fmt.Errorf("processing templates: %w", err)
	}
	return nil
}

// List returns every workspace for a project, or for all projects when
// project is "". Projects that are not cloned, and projects whose git calls
// fail, are skipped rather than failing the whole listing.
//
// This is the one place worktrees and sessions are joined, and the join is on
// the Ref — which carries the profile. The old split computed identity in
// worktree.Service.List, threw it away into a display string, and made the
// TUI recompute it.
func (h *Herd) List(project string) ([]Workspace, error) {
	names, err := h.projectNames(project)
	if err != nil {
		return nil, err
	}
	sessions, err := h.Sessions()
	if err != nil {
		return nil, err
	}
	byName := make(map[string][]Handle, len(sessions))
	for _, hd := range sessions {
		key := hd.Canonical
		byName[key] = append(byName[key], hd)
	}

	var out []Workspace
	for _, name := range names {
		cloneDir, err := h.cloneDir(name)
		if err != nil {
			continue
		}
		if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
			continue
		}
		infos, err := h.git.List(cloneDir)
		if err != nil {
			continue
		}
		defaultBranch := h.cfg.Projects[name].DefaultBranch
		for _, wt := range infos {
			ws := h.workspaceFrom(name, cloneDir, defaultBranch, wt)
			for i := range byName[ws.Ref.CanonicalName()] {
				hd := byName[ws.Ref.CanonicalName()][i]
				switch hd.Type {
				case SessionTypeAgent:
					ws.Agent = &hd
				case SessionTypeShell:
					ws.Shell = &hd
				}
			}
			out = append(out, ws)
		}
	}
	return out, nil
}

// workspaceFrom derives identity and display from one git worktree entry.
func (h *Herd) workspaceFrom(project, cloneDir, defaultBranch string, wt git.WorktreeInfo) Workspace {
	identity := semconv.WorktreeIdentityBranch(wt.Path, cloneDir, defaultBranch, wt.Branch)
	ws := Workspace{
		Ref:           h.Ref(project, identity),
		Path:          wt.Path,
		IsMain:        wt.Path == cloneDir,
		DisplayBranch: wt.Branch,
	}
	switch {
	case wt.Detached:
		ws.HeadHint = "detached"
	case wt.Branch != "" && semconv.FlattenBranch(wt.Branch) != semconv.FlattenBranch(identity):
		ws.HeadHint = "on " + wt.Branch
	}
	return ws
}

// Teardown stops a workspace's sessions and deletes its worktree.
//
// The order is not incidental. The TUI killed sessions by ID and then called
// worktree.Delete, which ran a second, profile-blind kill loop that either
// missed or no-opped — and force-deleted the worktree either way, orphaning
// the agent process. One loop, keyed on a Ref that carries the profile.
func (h *Herd) Teardown(ref Ref, opts TeardownOpts) error {
	wtPath, err := h.worktreePath(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s/%s", ErrWorktreeNotFound, ref.Project, ref.Branch)
	}
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return err
	}

	// The main worktree is the clone dir itself; removing it makes no sense.
	// Refuse before stopping any session or touching git.
	if wtPath == cloneDir {
		return fmt.Errorf("%w: %s/%s", ErrMainWorktree, ref.Project, ref.Branch)
	}

	if !opts.Force {
		running, err := h.handles()
		if err != nil {
			return err
		}
		canonical := ref.CanonicalName()
		for _, hd := range running {
			if hd.Canonical == canonical {
				return fmt.Errorf("%w: %s (%s)", ErrSessionRunning, canonical, hd.Type)
			}
		}
	}

	if _, err := h.StopSessions(ref, StopOpts{All: true}); err != nil {
		return err
	}
	if err := h.git.Remove(cloneDir, wtPath); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	return nil
}

// RemoteBranches returns a project's remote-tracking branches. When fetch is
// true it refreshes all remotes first (best-effort) so the list reflects
// current remote state; completion passes false to stay fast.
func (h *Herd) RemoteBranches(project string, fetch bool) ([]RemoteBranch, error) {
	cloneDir, err := h.cloneDir(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	if fetch {
		_ = h.git.FetchAll(cloneDir)
	}
	branches, err := h.git.ListRemoteBranches(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("listing remote branches: %w", err)
	}
	return branches, nil
}
