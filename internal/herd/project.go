package herd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// Project is a configured project with its derived clone path.
type Project struct {
	Name   string
	Config config.ProjectConfig
	Path   string // absolute path derived from repo URL + projects_dir
	Cloned bool   // true if Path exists on the filesystem
}

// Projects returns every configured project sorted by name. It does not touch
// the filesystem, so Cloned is always false — use Project for that.
func (h *Herd) Projects() []Project {
	names, _ := h.projectNames("") // "" cannot error
	entries := make([]Project, 0, len(names))
	for _, name := range names {
		path, _ := h.cloneDir(name) // unparseable repo URL yields an empty path
		entries = append(entries, Project{
			Name:   name,
			Config: h.cfg.Projects[name],
			Path:   path,
		})
	}
	return entries
}

// Project returns one project including its Cloned status.
func (h *Herd) Project(name string) (Project, error) {
	path, err := h.cloneDir(name)
	if err != nil {
		return Project{}, err
	}
	_, statErr := os.Stat(path)
	return Project{
		Name:   name,
		Config: h.cfg.Projects[name],
		Path:   path,
		Cloned: statErr == nil,
	}, nil
}

// Clone clones a project's repo into its derived path under projects_dir.
// Returns *AlreadyClonedError (wrapping ErrAlreadyCloned) if the path exists.
func (h *Herd) Clone(project string) error {
	path, err := h.cloneDir(project)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return &AlreadyClonedError{Path: path}
	}

	p := h.cfg.Projects[project]
	hook := h.hookFor(project)
	attrs := map[string]string{
		semconv.HookAttrProject:  project,
		semconv.HookAttrRepo:     p.Repo,
		semconv.HookAttrCloneDir: path,
	}

	if err := hook.Trigger(semconv.HookPreClone, attrs, h.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("pre-clone hook: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	if err := h.git.Clone(p.Repo, path, p.DefaultBranch); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}
	if err := hook.Trigger(semconv.HookPostClone, attrs, h.cfg.Defaults.ProjectsDir); err != nil {
		return fmt.Errorf("post-clone hook: %w", err)
	}
	return nil
}
