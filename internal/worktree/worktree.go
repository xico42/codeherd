package worktree

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// Sentinel errors returned by Service methods.
var (
	ErrNotCloned         = errors.New("project not cloned")
	ErrWorktreeExists    = errors.New("worktree already exists")
	ErrWorktreeNotFound  = errors.New("worktree not found")
	ErrSessionRunning    = errors.New("session is running")
	ErrLocalBranchExists = errors.New("local branch already exists")
)

// WorktreeInfo holds data from a single git worktree entry.
type WorktreeInfo struct {
	Path     string
	Branch   string // empty if detached HEAD
	Detached bool   // true when HEAD is detached (e.g. rebase in progress)
}

// ListEntry is one row in the worktree list output.
type ListEntry struct {
	Project  string
	Branch   string
	Path     string
	Session  string // "<name>-<branch> (running)" or ""
	Detached bool   // true when the worktree's HEAD is detached
}

// NewResult is the result of a successful worktree creation.
type NewResult struct {
	Path   string
	Branch string // resolved local branch name (derived for --track)
}

// RemoteBranch is one remote-tracking branch (e.g. origin/feature-x).
type RemoteBranch struct {
	Remote string
	Branch string
	Ref    string // "<remote>/<branch>"
}

// WorktreeRunner abstracts git worktree operations for testability.
type WorktreeRunner interface {
	Add(cloneDir, worktreePath, branch string) error
	AddNewBranch(cloneDir, worktreePath, branch string) error
	AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
	Remove(cloneDir, worktreePath string) error
	List(cloneDir string) ([]WorktreeInfo, error)

	Fetch(cloneDir, remote, branch string) error
	FetchAll(cloneDir string) error
	FastForward(cloneDir, remote, branch string) error
	Remotes(cloneDir string) ([]string, error)
	ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)
	AddTracking(cloneDir, worktreePath, branch, remoteRef string) error
	HasLocalBranch(cloneDir, branch string) (bool, error)
}

// RealWorktreeRunner runs git worktree commands via os/exec.
type RealWorktreeRunner struct{}

// NewRealWorktreeRunner returns a WorktreeRunner backed by the system git binary.
func NewRealWorktreeRunner() *RealWorktreeRunner { return &RealWorktreeRunner{} }

func (r *RealWorktreeRunner) Add(cloneDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", worktreePath, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) AddNewBranch(cloneDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add -b: %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, startPoint)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add -b (from): %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) Remove(cloneDir, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) List(cloneDir string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreePorcelain(string(out)), nil
}

func (r *RealWorktreeRunner) Fetch(cloneDir, remote, branch string) error {
	cmd := exec.Command("git", "fetch", remote, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch %s %s: %w\n%s", remote, branch, err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) FetchAll(cloneDir string) error {
	cmd := exec.Command("git", "fetch", "--all", "--prune")
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch --all: %w\n%s", err, out)
	}
	return nil
}

// FastForward advances the local branch to <remote>/<branch> without losing
// local commits. It first tries a non-checkout ref update (works when the
// branch is not checked out); if that fails it falls back to a fast-forward-only
// merge (for the clone's currently checked-out branch). Either failure is
// reported but treated as best-effort by callers.
func (r *RealWorktreeRunner) FastForward(cloneDir, remote, branch string) error {
	refspec := branch + ":" + branch
	cmd := exec.Command("git", "fetch", remote, refspec)
	cmd.Dir = cloneDir
	if _, err := cmd.CombinedOutput(); err == nil {
		return nil
	}
	remoteRef := remote + "/" + branch
	cmd = exec.Command("git", "merge", "--ff-only", remoteRef)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fast-forward %s: %w\n%s", branch, err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) Remotes(cloneDir string) ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote: %w", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

func (r *RealWorktreeRunner) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	return parseRemoteBranches(string(out)), nil
}

func (r *RealWorktreeRunner) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	cmd := exec.Command("git", "worktree", "add", "--track", "-b", branch, worktreePath, remoteRef)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add --track: %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) HasLocalBranch(cloneDir, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = cloneDir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, fmt.Errorf("git show-ref %s: %w", ref, err)
}

// parseWorktreePorcelain parses the output of `git worktree list --porcelain`.
// Blocks are separated by blank lines.
func parseWorktreePorcelain(output string) []WorktreeInfo {
	var result []WorktreeInfo
	var current WorktreeInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			current.Detached = true
		case line == "":
			if current.Path != "" {
				result = append(result, current)
				current = WorktreeInfo{}
			}
		}
	}
	if current.Path != "" {
		result = append(result, current)
	}
	return result
}

// parseRemoteBranches parses `git for-each-ref --format=%(refname:short) refs/remotes`.
// The remote name is the segment before the first slash; the rest is the branch
// (which may contain slashes). Symbolic */HEAD entries are skipped.
func parseRemoteBranches(output string) []RemoteBranch {
	var result []RemoteBranch
	for _, line := range strings.Split(output, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		idx := strings.Index(s, "/")
		if idx <= 0 {
			continue
		}
		remote, branch := s[:idx], s[idx+1:]
		if branch == "" || branch == "HEAD" {
			continue
		}
		result = append(result, RemoteBranch{Remote: remote, Branch: branch, Ref: s})
	}
	return result
}

// parseRef splits a user-supplied ref into a remote and branch. When ref is
// "<remote>/<rest>" and <remote> matches a configured remote, it returns
// (remote, rest, true). Otherwise it defaults to ("origin", ref, false), which
// keeps branch names containing slashes (e.g. feature/login) intact.
func parseRef(remotes []string, ref string) (remote, branch string, explicit bool) {
	if idx := strings.Index(ref, "/"); idx > 0 {
		candidate := ref[:idx]
		for _, r := range remotes {
			if r == candidate {
				return candidate, ref[idx+1:], true
			}
		}
	}
	return "origin", ref, false
}

// Service provides worktree management operations.
type Service struct {
	cfg  *config.Config
	git  WorktreeRunner
	tmux *tmux.Client
	hook hooks.Hook
}

// NewService creates a Service.
func NewService(cfg *config.Config, git WorktreeRunner, tmux *tmux.Client, hook hooks.Hook) *Service {
	return &Service{cfg: cfg, git: git, tmux: tmux, hook: hook}
}

// resolvePaths returns cloneDir, worktreesRoot, and worktreePath for a project+branch.
func (s *Service) resolvePaths(project, branch string) (cloneDir, worktreesRoot, worktreePath string, err error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return "", "", "", fmt.Errorf("project %q is not configured", project)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return "", "", "", fmt.Errorf("parsing repo URL: %w", err)
	}
	cloneDir = semconv.CloneDir(s.cfg.Defaults.ProjectsDir, repoPath)
	worktreesRoot = semconv.WorktreesRoot(s.cfg.Defaults.ProjectsDir, repoPath)
	worktreePath = semconv.WorktreePath(s.cfg.Defaults.ProjectsDir, repoPath, branch)
	return cloneDir, worktreesRoot, worktreePath, nil
}

// freshenStartPoint fetches updates for the source ref and returns the start
// point a new worktree branch should be based on. It prefers a fast-forwarded
// local branch (to preserve un-pushed commits), falling back to the
// remote-tracking ref, or the raw ref when the source is not on a remote
// (tags, SHAs, local-only branches). All git failures here are best-effort.
func (s *Service) freshenStartPoint(cloneDir, src string) string {
	remotes, _ := s.git.Remotes(cloneDir)
	remote, branch, explicit := parseRef(remotes, src)
	if explicit {
		_ = s.git.Fetch(cloneDir, remote, branch)
		return src
	}
	if err := s.git.Fetch(cloneDir, "origin", src); err != nil {
		return src
	}
	if has, _ := s.git.HasLocalBranch(cloneDir, src); has {
		_ = s.git.FastForward(cloneDir, "origin", src)
		return src
	}
	return "origin/" + src
}

// New creates a new git worktree for the given project and branch.
func (s *Service) New(project, branch string) (NewResult, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return NewResult{}, fmt.Errorf("project %q is not configured", project)
	}

	cloneDir, worktreesRoot, worktreePath, err := s.resolvePaths(project, branch)
	if err != nil {
		return NewResult{}, err
	}

	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return NewResult{}, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}

	if _, err := os.Stat(worktreePath); err == nil {
		return NewResult{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, project, branch)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       branch,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: worktreePath,
	}

	if err := s.hook.Trigger(semconv.HookPreWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return NewResult{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	addErr := s.git.Add(cloneDir, worktreePath, branch)
	if addErr != nil {
		src := p.DefaultBranch
		if src == "" {
			src = "main"
		}
		startPoint := s.freshenStartPoint(cloneDir, src)
		if err := s.git.AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint); err != nil {
			return NewResult{}, fmt.Errorf("failed to create worktree (add: %v; add -b from %s: %w)", addErr, startPoint, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: branch}, nil
}

// NewFrom creates a new git worktree branching from the given start point.
func (s *Service) NewFrom(project, branch, fromBranch string) (NewResult, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return NewResult{}, fmt.Errorf("project %q is not configured", project)
	}

	cloneDir, worktreesRoot, worktreePath, err := s.resolvePaths(project, branch)
	if err != nil {
		return NewResult{}, err
	}

	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return NewResult{}, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}

	if _, err := os.Stat(worktreePath); err == nil {
		return NewResult{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, project, branch)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       branch,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: worktreePath,
	}

	if err := s.hook.Trigger(semconv.HookPreWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return NewResult{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	startPoint := s.freshenStartPoint(cloneDir, fromBranch)
	if err := s.git.AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint); err != nil {
		return NewResult{}, fmt.Errorf("creating worktree from %s: %w", startPoint, err)
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: branch}, nil
}

// cloneDirFor returns the clone directory for a project without needing a
// branch (unlike resolvePaths, which also computes a worktree path).
func (s *Service) cloneDirFor(project string) (string, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return "", fmt.Errorf("project %q is not configured", project)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL: %w", err)
	}
	return semconv.CloneDir(s.cfg.Defaults.ProjectsDir, repoPath), nil
}

// NewTracking fetches a remote branch and creates a worktree whose local branch
// tracks it. ref is "[<remote>/]<branch>" (default remote origin). When branch
// is empty the local name is derived from the remote branch; otherwise it
// overrides the derived name.
func (s *Service) NewTracking(project, branch, ref string) (NewResult, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return NewResult{}, fmt.Errorf("project %q is not configured", project)
	}

	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return NewResult{}, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return NewResult{}, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}

	remotes, _ := s.git.Remotes(cloneDir)
	remote, remoteBranch, _ := parseRef(remotes, ref)
	localName := branch
	if localName == "" {
		localName = remoteBranch
	}
	remoteRef := remote + "/" + remoteBranch

	_, worktreesRoot, worktreePath, err := s.resolvePaths(project, localName)
	if err != nil {
		return NewResult{}, err
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return NewResult{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, project, localName)
	}

	if has, _ := s.git.HasLocalBranch(cloneDir, localName); has {
		return NewResult{}, fmt.Errorf("%w: %s", ErrLocalBranchExists, localName)
	}

	if err := s.git.Fetch(cloneDir, remote, remoteBranch); err != nil {
		return NewResult{}, fmt.Errorf("fetching %s: %w", remoteRef, err)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       localName,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: worktreePath,
	}

	if err := s.hook.Trigger(semconv.HookPreWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return NewResult{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	if err := s.git.AddTracking(cloneDir, worktreePath, localName, remoteRef); err != nil {
		return NewResult{}, fmt.Errorf("creating tracking worktree for %s: %w", remoteRef, err)
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: localName}, nil
}

// ListRemoteBranches returns the remote-tracking branches for a project without
// fetching (suitable for shell completion).
func (s *Service) ListRemoteBranches(project string) ([]RemoteBranch, error) {
	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	branches, err := s.git.ListRemoteBranches(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("listing remote branches: %w", err)
	}
	return branches, nil
}

// RemoteBranches fetches all remotes (best-effort) then lists remote-tracking
// branches — used by the TUI picker so the list reflects current remote state.
func (s *Service) RemoteBranches(project string) ([]RemoteBranch, error) {
	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	_ = s.git.FetchAll(cloneDir)
	branches, err := s.git.ListRemoteBranches(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("listing remote branches: %w", err)
	}
	return branches, nil
}

// List returns worktree entries for all configured projects, or just the named one.
// Skips projects that are not cloned. Never returns an error for individual project
// failures — those are silently skipped.
func (s *Service) List(project string) ([]ListEntry, error) {
	names, err := s.projectNames(project)
	if err != nil {
		return nil, err
	}

	var entries []ListEntry
	for _, name := range names {
		p := s.cfg.Projects[name]
		repoPath, err := config.RepoPath(p.Repo)
		if err != nil {
			continue
		}
		cd := filepath.Join(s.cfg.Defaults.ProjectsDir, repoPath)
		if _, err := os.Stat(cd); os.IsNotExist(err) {
			continue
		}

		worktrees, err := s.git.List(cd)
		if err != nil {
			continue
		}

		for _, wt := range worktrees {
			identity := semconv.WorktreeIdentityBranch(wt.Path, cd, p.DefaultBranch, wt.Branch)
			session := ""
			if identity != "" {
				candidate := semconv.SessionName("", name, identity)
				if running, _ := s.tmux.HasSession(candidate); running {
					session = candidate + " (running)"
				}
			}
			entries = append(entries, ListEntry{
				Project:  name,
				Branch:   wt.Branch,
				Path:     wt.Path,
				Session:  session,
				Detached: wt.Detached,
			})
		}
	}
	return entries, nil
}

// DeleteRequest holds parameters for a worktree deletion.
type DeleteRequest struct {
	Project string
	Branch  string
	Force   bool
}

// Delete removes a git worktree. Returns ErrWorktreeNotFound if the worktree
// directory does not exist, ErrSessionRunning if any tmux session (agent or
// shell) is active and Force is false.
func (s *Service) Delete(req DeleteRequest) error {
	cloneDir, _, worktreePath, err := s.resolvePaths(req.Project, req.Branch)
	if err != nil {
		return err
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s/%s", ErrWorktreeNotFound, req.Project, req.Branch)
	}

	sessionNames := []string{
		semconv.SessionName("", req.Project, req.Branch),
		semconv.ShellSessionName("", req.Project, req.Branch),
	}

	for _, name := range sessionNames {
		running, err := s.tmux.HasSession(name)
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}
		if running && !req.Force {
			return fmt.Errorf("%w: %s", ErrSessionRunning, name)
		}
		if running && req.Force {
			if err := s.tmux.KillSession(name); err != nil {
				return fmt.Errorf("killing session: %w", err)
			}
		}
	}

	if err := s.git.Remove(cloneDir, worktreePath); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	return nil
}

// WorktreePath resolves the filesystem path for the given project+branch worktree,
// checking that both the clone and the worktree exist.
func (s *Service) WorktreePath(project, branch string) (string, error) {
	cloneDir, _, worktreePath, err := s.resolvePaths(project, branch)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s/%s", ErrWorktreeNotFound, project, branch)
	}
	return worktreePath, nil
}

// projectNames returns sorted project names. If project is non-empty, validates and
// returns just that one.
func (s *Service) projectNames(project string) ([]string, error) {
	if project != "" {
		if _, ok := s.cfg.Projects[project]; !ok {
			return nil, fmt.Errorf("project %q is not configured", project)
		}
		return []string{project}, nil
	}
	names := make([]string, 0, len(s.cfg.Projects))
	for name := range s.cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
