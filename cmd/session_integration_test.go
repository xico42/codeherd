//go:build integration

package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/testutil"
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
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestCreateSession_autoCreate_createsWorktreeAndStartsSession verifies that
// create session creates the worktree when it is missing and the project is
// cloned. Requires tmux to be available.
func TestCreateSession_autoCreate_createsWorktreeAndStartsSession(t *testing.T) {
	testutil.IsolateTmux(t)

	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	testutil.InitRepo(t, cloneDir)

	cfgPath := writeSessionConfigWithProjectsDir(t, projectsDir)

	// No worktree exists yet — create session should create it automatically.
	err := runCmd(t, "--config", cfgPath, "create", "session", "myapp", "feat")
	if err != nil {
		t.Fatalf("create session with auto-create = %v, want nil", err)
	}
	// useIsolatedTmux kills the per-test tmux server in t.Cleanup, so the
	// session goes with it; no explicit kill-session needed.
}
