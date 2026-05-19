//go:build integration

package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeSessionConfigWithProjectsDir writes a config pointing at a real
// projects_dir that has a cloned repo at the expected path.
func writeSessionConfigWithProjectsDir(t *testing.T, projectsDir string) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[defaults]
projects_dir = "` + projectsDir + `"
agent = "test-agent"

[agents.test-agent]
cmd = "true"

[projects.myapp]
repo = "git@github.com:user/myapp.git"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// initBareRepo creates a minimal git repo at cloneDir suitable for
// `git worktree add` calls.
func initBareRepo(t *testing.T, cloneDir string) {
	t.Helper()
	os.MkdirAll(cloneDir, 0o755)
	cmds := [][]string{
		{"git", "init", cloneDir},
		{"git", "-C", cloneDir, "config", "user.email", "test@test.com"},
		{"git", "-C", cloneDir, "config", "user.name", "Test"},
		{"git", "-C", cloneDir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", c, err, out)
		}
	}
}

// TestCreateSession_autoCreate_createsWorktreeAndStartsSession verifies that
// create session creates the worktree when it is missing and the project is
// cloned. Requires tmux to be available.
func TestCreateSession_autoCreate_createsWorktreeAndStartsSession(t *testing.T) {
	useIsolatedTmux(t)

	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	initBareRepo(t, cloneDir)

	cfgPath := writeSessionConfigWithProjectsDir(t, projectsDir)

	// No worktree exists yet — create session should create it automatically.
	err := runCmd(t, "--config", cfgPath, "create", "session", "myapp", "feat")
	if err != nil {
		t.Fatalf("create session with auto-create = %v, want nil", err)
	}
	// useIsolatedTmux kills the per-test tmux server in t.Cleanup, so the
	// session goes with it; no explicit kill-session needed.
}
