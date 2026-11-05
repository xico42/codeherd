//go:build integration

package herd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/testutil"
)

// localCloneGit is a real git runner whose Clone ignores the configured repo
// URL and clones a local remote instead, so AutoClone can be exercised offline
// while the clone dir still derives from the github-style URL.
type localCloneGit struct {
	git.Runner
	source string
}

func (g localCloneGit) Clone(_, path, branch string) error {
	if err := g.Runner.Clone(g.source, path, branch); err != nil {
		return fmt.Errorf("local clone of %s: %w", g.source, err)
	}
	return nil
}

// EnsureWorkspace with AutoClone must clone the project first, then create the
// new-branch worktree under __worktrees/. This guards the ordering after
// worktreePath began consulting git.List: the List call must only ever run
// against an already-cloned repo.
//
// The clone source is a synthetic upstream repo with a main branch, built here
// with real git, so the test exercises AutoClone against genuine git while
// staying hermetic — it must not depend on the outer checkout having a local
// main branch, which CI (single-branch PR checkouts) does not.
func TestEnsureWorkspace_autoClone_endToEnd(t *testing.T) {
	root := t.TempDir()

	// Build an upstream repo whose default branch is main and that carries a
	// go.mod, so the clone and its worktree can be asserted below.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "go.mod"), []byte("module myapp\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, remote, "add", ".")
	testutil.Git(t, remote, "commit", "-m", "init")

	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: nil, Git: localCloneGit{Runner: git.NewRealRunner(), source: remote}})

	// The project is not cloned yet — AutoClone must do it.
	if _, err := os.Stat(cloneDir); !os.IsNotExist(err) {
		t.Fatalf("clone dir should not exist before EnsureWorkspace: %v", err)
	}

	ws, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{AutoClone: true})
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}

	// The clone happened.
	if _, err := os.Stat(filepath.Join(cloneDir, "go.mod")); err != nil {
		t.Errorf("expected clone at %s: %v", cloneDir, err)
	}
	// The new worktree lives under __worktrees/, not at the clone dir.
	wantPath := filepath.Join(cloneDir+"__worktrees", "feature")
	if ws.Path != wantPath {
		t.Errorf("worktree path = %q, want %q", ws.Path, wantPath)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "go.mod")); err != nil {
		t.Errorf("expected go.mod in worktree: %v", err)
	}

	// And the default branch resolves to the clone dir on the freshly cloned
	// repo — the bug this change fixes, proven end-to-end.
	mainPath, err := h.worktreePath(h.Ref("myapp", "main"))
	if err != nil {
		t.Fatalf("worktreePath(main): %v", err)
	}
	if mainPath != cloneDir {
		t.Errorf("worktreePath(main) = %q, want clone dir %q", mainPath, cloneDir)
	}
}

func TestEnsureWorkspace_tracking_endToEnd(t *testing.T) {
	root := t.TempDir()

	// Build an upstream repo with a PR branch.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, remote, "add", ".")
	testutil.Git(t, remote, "commit", "-m", "init")
	testutil.Git(t, remote, "checkout", "-b", "feat-x")
	if err := os.WriteFile(filepath.Join(remote, "feature.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, remote, "add", ".")
	testutil.Git(t, remote, "commit", "-m", "feature")
	testutil.Git(t, remote, "checkout", "main")

	// Clone into the codeherd layout: <projectsDir>/github.com/user/myapp.
	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, root, "clone", remote, cloneDir)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: nil, Git: git.NewRealRunner()})

	ws, err := h.EnsureWorkspace(h.Ref("myapp", ""), EnsureOpts{Track: "feat-x"})
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	if ws.Ref.Branch != "feat-x" {
		t.Errorf("branch = %q, want feat-x", ws.Ref.Branch)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "feature.txt")); err != nil {
		t.Errorf("expected feature.txt in worktree: %v", err)
	}

	branch := strings.TrimSpace(testutil.Git(t, ws.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "feat-x" {
		t.Errorf("worktree branch = %q, want feat-x", branch)
	}
	upstream := strings.TrimSpace(testutil.Git(t, ws.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"))
	if upstream != "origin/feat-x" {
		t.Errorf("upstream = %q, want origin/feat-x", upstream)
	}
}
