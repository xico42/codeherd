package herd

import (
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
)

func pathsHerd(t *testing.T) (*Herd, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	return New(cfg, nil, Deps{}), dir
}

func TestCloneDir_derivedFromRepoURL(t *testing.T) {
	h, dir := pathsHerd(t)
	got, err := h.cloneDir("myapp")
	if err != nil {
		t.Fatalf("cloneDir: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp")
	if got != want {
		t.Errorf("cloneDir = %q, want %q", got, want)
	}
}

func TestWorktreesRoot_derivedFromRepoURL(t *testing.T) {
	h, dir := pathsHerd(t)
	got, err := h.worktreesRoot("myapp")
	if err != nil {
		t.Fatalf("worktreesRoot: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp__worktrees")
	if got != want {
		t.Errorf("worktreesRoot = %q, want %q", got, want)
	}
}

func TestWorktreePath_flattensBranch(t *testing.T) {
	h, dir := pathsHerd(t)
	got, err := h.worktreePath(h.Ref("myapp", "feat/login"))
	if err != nil {
		t.Fatalf("worktreePath: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat-login")
	if got != want {
		t.Errorf("worktreePath = %q, want %q", got, want)
	}
}

// The main worktree lives at the clone dir itself, not under __worktrees/.
// worktreePath must resolve the default-branch identity to the clone dir so
// operations on the main worktree (e.g. launching a shell) find it on disk.
func TestWorktreePath_defaultBranchResolvesToCloneDir(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{
			{Path: cloneDir, Branch: "main"},
			{Path: filepath.Join(cloneDir+"__worktrees", "feat"), Branch: "feat"},
		}, nil
	}}
	h := New(cfg, nil, Deps{Git: g})

	got, err := h.worktreePath(h.Ref("myapp", "main"))
	if err != nil {
		t.Fatalf("worktreePath: %v", err)
	}
	if got != cloneDir {
		t.Errorf("worktreePath(main) = %q, want clone dir %q", got, cloneDir)
	}
}

// With no default_branch configured, the main worktree's identity is the
// branch its HEAD is on. worktreePath must still resolve it to the clone dir.
func TestWorktreePath_unconfiguredDefaultResolvesToCloneDir(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: cloneDir, Branch: "master"}}, nil
	}}
	h := New(cfg, nil, Deps{Git: g})

	got, err := h.worktreePath(h.Ref("myapp", "master"))
	if err != nil {
		t.Fatalf("worktreePath: %v", err)
	}
	if got != cloneDir {
		t.Errorf("worktreePath(master) = %q, want clone dir %q", got, cloneDir)
	}
}

func TestPaths_unconfiguredProject(t *testing.T) {
	h, _ := pathsHerd(t)
	if _, err := h.cloneDir("nope"); err == nil {
		t.Error("want error for unconfigured project, got nil")
	}
}

func TestProjectNames_sortedOrAll(t *testing.T) {
	h, _ := pathsHerd(t)
	h.cfg.Projects["alpha"] = config.ProjectConfig{Repo: "git@github.com:user/alpha.git"}

	all, err := h.projectNames("")
	if err != nil {
		t.Fatalf("projectNames(\"\"): %v", err)
	}
	if len(all) != 2 || all[0] != "alpha" || all[1] != "myapp" {
		t.Errorf("projectNames(\"\") = %v, want [alpha myapp]", all)
	}

	one, err := h.projectNames("myapp")
	if err != nil {
		t.Fatalf("projectNames(\"myapp\"): %v", err)
	}
	if len(one) != 1 || one[0] != "myapp" {
		t.Errorf("projectNames(\"myapp\") = %v", one)
	}
	if _, err := h.projectNames("nope"); err == nil {
		t.Error("want error for unconfigured project, got nil")
	}
}
