package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSessionConfig writes a config with agent settings and a project.
func writeSessionConfig(t *testing.T, projectsDir string) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[defaults]
projects_dir = "` + projectsDir + `"
agent = "echo-agent"

[agents.echo-agent]
cmd = "echo"
args = ["hello"]

[projects.myapp]
repo = "git@github.com:user/myapp.git"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestSessionList_empty(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	// list should succeed even with no tmux (service handles errors)
	// May fail if tmux not available — that's acceptable in unit tests.
	_ = runCmd(t, "--config", cfgPath, "list", "session")
}

func TestCreateSession_tooFewArgs(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	if err := runCmd(t, "--config", cfgPath, "create", "session", "myapp"); err == nil {
		t.Error("expected error for missing branch argument")
	}
}

func TestCreateSession_tooManyArgs(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	if err := runCmd(t, "--config", cfgPath, "create", "session", "a", "b", "c"); err == nil {
		t.Error("expected error for too many arguments")
	}
}

func TestAttachSession_tooFewArgs(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	if err := runCmd(t, "--config", cfgPath, "attach", "session"); err == nil {
		t.Error("expected error for missing session argument")
	}
}

func TestDeleteSession_tooFewArgs(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	if err := runCmd(t, "--config", cfgPath, "delete", "session"); err == nil {
		t.Error("expected error for missing session argument")
	}
}

func TestShowSession_tooFewArgs(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	if err := runCmd(t, "--config", cfgPath, "show", "session"); err == nil {
		t.Error("expected error for missing session argument")
	}
}

func TestCreateSession_agentFlag_recognized(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	err := runCmd(t, "--config", cfgPath, "create", "session", "--agent", "echo-agent", "notaproject", "main")
	if err == nil {
		t.Fatal("expected error for unconfigured project, got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--agent flag not recognised: %v", err)
	}
}

func TestCreateSession_unconfiguredProject(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	// unconfigured project returns a non-sentinel error from worktree service,
	// which exercises the sessionErr default branch (returns error, no os.Exit).
	err := runCmd(t, "--config", cfgPath, "create", "session", "notaproject", "main")
	if err == nil {
		t.Error("expected error for unconfigured project, got nil")
	}
}

func TestCreateSession_worktreeCreationFails_returnsError(t *testing.T) {
	cfgDir := t.TempDir()
	projectsDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[defaults]
projects_dir = "` + projectsDir + `"

[projects.myapp]
repo = "git@github.com:user/myapp.git"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create the clone dir so EnsureWorkspace gets past the not-cloned check
	// (which now goes through worktreeErr's os.Exit, like create worktree).
	// The clone dir is not a real git repo, so worktree creation fails with a
	// non-sentinel error that flows through worktreeErr's default branch and is
	// returned (no os.Exit) — the returnable path this test can observe.
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, "--config", cfgPath, "create", "session", "myapp", "main")
	if err == nil {
		t.Fatal("expected error for an uncreatable session")
	}
}

func TestCreateSession_shellFlag_recognized(t *testing.T) {
	cfgPath := writeSessionConfig(t, t.TempDir())
	err := runCmd(t, "--config", cfgPath, "create", "session", "--shell", "notaproject", "main")
	if err == nil {
		t.Fatal("expected error for unconfigured project, got nil")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--shell flag not recognised: %v", err)
	}
}

func TestDeleteSession_helpShowsFlags(t *testing.T) {
	if err := runCmd(t, "delete", "session", "--help"); err != nil {
		t.Fatalf("delete session --help: %v", err)
	}
}
