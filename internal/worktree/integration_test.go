//go:build integration

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/tmux"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestNewTracking_endToEnd(t *testing.T) {
	root := t.TempDir()

	// Build an upstream repo with a PR branch.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "init")
	git(t, remote, "checkout", "-b", "feat-x")
	if err := os.WriteFile(filepath.Join(remote, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "feature")
	git(t, remote, "checkout", "main")

	// Clone into the codeherd layout: <projectsDir>/github.com/user/myapp.
	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "clone", remote, cloneDir)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	svc := NewService(cfg, NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})

	result, err := svc.NewTracking("myapp", "", "feat-x")
	if err != nil {
		t.Fatalf("NewTracking: %v", err)
	}
	if result.Branch != "feat-x" {
		t.Errorf("branch = %q, want feat-x", result.Branch)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "feature.txt")); err != nil {
		t.Errorf("expected feature.txt in worktree: %v", err)
	}

	branch := strings.TrimSpace(git(t, result.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "feat-x" {
		t.Errorf("worktree branch = %q, want feat-x", branch)
	}
	upstream := strings.TrimSpace(git(t, result.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"))
	if upstream != "origin/feat-x" {
		t.Errorf("upstream = %q, want origin/feat-x", upstream)
	}
}
