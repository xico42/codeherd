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
	ErrNotCloned        = errors.New("project not cloned")
	ErrWorktreeExists   = errors.New("worktree already exists")
	ErrWorktreeNotFound = errors.New("worktree not found")
	ErrSessionRunning   = errors.New("session is running")
)

// WorktreeInfo holds data from a single git worktree entry.
type WorktreeInfo struct {
	Path   string
	Branch string // empty if detached HEAD
}

// ListEntry is one row in the worktree list output.
type ListEntry struct {
	Project string
	Branch  string
	Path    string
	Session string // "<name>-<branch> (running)" or ""
}

// NewResult is the result of a successful worktree creation.
type NewResult struct {
	Path string
}

// WorktreeRunner abstracts git worktree operations for testability.
type WorktreeRunner interface {
	Add(cloneDir, worktreePath, branch string) error
	AddNewBranch(cloneDir, worktreePath, branch string) error
	AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
	Remove(cloneDir, worktreePath string) error
	List(cloneDir string) ([]WorktreeInfo, error)
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
		if err := s.git.AddNewBranch(cloneDir, worktreePath, branch); err != nil {
			return NewResult{}, fmt.Errorf("failed to create worktree (add: %v; add -b: %w)", addErr, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath}, nil
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

	if err := s.git.AddNewBranchFrom(cloneDir, worktreePath, branch, fromBranch); err != nil {
		return NewResult{}, fmt.Errorf("creating worktree from %s: %w", fromBranch, err)
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath}, nil
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
			session := ""
			if wt.Branch != "" {
				candidate := semconv.SessionName("", name, wt.Branch)
				if running, _ := s.tmux.HasSession(candidate); running {
					session = candidate + " (running)"
				}
			}
			entries = append(entries, ListEntry{
				Project: name,
				Branch:  wt.Branch,
				Path:    wt.Path,
				Session: session,
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
