//go:build integration

package herd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
)

// Teardown must refuse the main worktree with ErrMainWorktree — the main
// worktree is the clone dir itself, and removing it makes no sense. The guard
// fires for both Force values and never reaches git.Remove, so the clone dir
// survives.
func TestTeardown_mainWorktree_refused(t *testing.T) {
	root := t.TempDir()

	// Build a real upstream repo with a main branch.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "go.mod"), []byte("module myapp\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "init")

	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: nil, Git: localCloneGit{Runner: git.NewRealRunner(), source: remote}})

	// Clone so the main worktree resolves to the clone dir. Cloning already
	// materializes the main worktree at the clone dir, so there is no separate
	// worktree to create — EnsureWorkspace(main) would only hit ErrWorktreeExists.
	if err := h.Clone("myapp"); err != nil {
		t.Fatalf("Clone(myapp): %v", err)
	}

	for _, force := range []bool{false, true} {
		err := h.Teardown(h.Ref("myapp", "main"), TeardownOpts{Force: force})
		if !errors.Is(err, ErrMainWorktree) {
			t.Errorf("Teardown(main, force=%v) err = %v, want ErrMainWorktree", force, err)
		}
	}

	// The clone dir must still be on disk — the guard fired before git.Remove.
	if _, err := os.Stat(filepath.Join(cloneDir, "go.mod")); err != nil {
		t.Errorf("clone dir removed by Teardown(main): %v", err)
	}
}
