package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

// ErrAlreadyCloned is returned by Clone when the target path already exists.
var ErrAlreadyCloned = errors.New("already cloned")

// AlreadyClonedError carries the path that already exists.
type AlreadyClonedError struct{ Path string }

func (e *AlreadyClonedError) Error() string { return e.Path + " already exists, skipping" }
func (e *AlreadyClonedError) Unwrap() error { return ErrAlreadyCloned }

// GitRunner abstracts git clone execution to enable testing.
type GitRunner interface {
	Clone(repo, path, branch string) error
}

// RealGitRunner runs git commands via os/exec.
type RealGitRunner struct{}

// NewRealGitRunner returns a GitRunner backed by the system git binary.
func NewRealGitRunner() *RealGitRunner { return &RealGitRunner{} }

// Clone runs git clone. If branch is non-empty, passes --branch <branch>.
func (r *RealGitRunner) Clone(repo, path, branch string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repo, path)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
	}
	return nil
}

// ProjectEntry is a project with its derived filesystem path and clone status.
type ProjectEntry struct {
	Name   string
	Config config.ProjectConfig
	Path   string // absolute path derived from repo URL + projects_dir
	Cloned bool   // true if Path exists on the filesystem
}

// CloneResult captures the outcome of a single clone attempt.
type CloneResult struct {
	Name string
	Err  error // nil=success, ErrAlreadyCloned=skipped, other=failure
}

// Service provides project management operations.
type Service struct {
	cfg  *config.Config
	git  GitRunner
	hook hooks.Hook
}

// NewService creates a Service using the given config and GitRunner.
func NewService(cfg *config.Config, git GitRunner, hook hooks.Hook) *Service {
	return &Service{cfg: cfg, git: git, hook: hook}
}

// List returns all configured projects sorted by name. No filesystem access.
func (s *Service) List() []ProjectEntry {
	entries := make([]ProjectEntry, 0, len(s.cfg.Projects))
	for name, p := range s.cfg.Projects {
		var path string
		if rp, err := config.RepoPath(p.Repo); err == nil {
			path = filepath.Join(s.cfg.Defaults.ProjectsDir, rp)
		}
		entries = append(entries, ProjectEntry{
			Name:   name,
			Config: p,
			Path:   path,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Show returns the full entry for a single project, including Cloned status.
func (s *Service) Show(name string) (ProjectEntry, error) {
	p, ok := s.cfg.Projects[name]
	if !ok {
		return ProjectEntry{}, fmt.Errorf("project %q is not configured", name)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return ProjectEntry{}, fmt.Errorf("cannot parse repo URL %q: %w", p.Repo, err)
	}
	absPath := filepath.Join(s.cfg.Defaults.ProjectsDir, repoPath)
	_, statErr := os.Stat(absPath)
	return ProjectEntry{
		Name:   name,
		Config: p,
		Path:   absPath,
		Cloned: statErr == nil,
	}, nil
}

// Clone clones a single project into its derived path under projects_dir.
// Returns *AlreadyClonedError (wrapping ErrAlreadyCloned) if the path exists.
func (s *Service) Clone(name string) error {
	p, ok := s.cfg.Projects[name]
	if !ok {
		return fmt.Errorf("project %q is not configured", name)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return fmt.Errorf("cannot parse repo URL %q: %w", p.Repo, err)
	}
	absPath := filepath.Join(s.cfg.Defaults.ProjectsDir, repoPath)
	if _, err := os.Stat(absPath); err == nil {
		return &AlreadyClonedError{Path: absPath}
	}

	attrs := map[string]string{
		semconv.HookAttrProject:  name,
		semconv.HookAttrRepo:     p.Repo,
		semconv.HookAttrCloneDir: absPath,
	}

	if err := s.hook.Trigger(semconv.HookPreClone, attrs, s.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("pre-clone hook: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	if err := s.git.Clone(p.Repo, absPath, p.DefaultBranch); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	if err := s.hook.Trigger(semconv.HookPostClone, attrs, s.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("post-clone hook: %w", err)
	}

	return nil
}

// CloneAll clones all configured projects in sorted order.
func (s *Service) CloneAll() []CloneResult {
	names := make([]string, 0, len(s.cfg.Projects))
	for name := range s.cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]CloneResult, 0, len(names))
	for _, name := range names {
		results = append(results, CloneResult{Name: name, Err: s.Clone(name)})
	}
	return results
}
