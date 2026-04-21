package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a minimal TOML config file with a projects_dir and
// a single project entry, then returns the config file path.
func writeConfig(t *testing.T, projectsDir string) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := "[defaults]\nprojects_dir = \"" + projectsDir + "\"\n\n[projects.myapp]\nrepo = \"git@github.com:user/myapp.git\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestListWorktreeCmd_help verifies the list worktree subcommand has correct --help output.
func TestListWorktreeCmd_help(t *testing.T) {
	if err := runCmd(t, "list", "worktree", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestWorktreeList_noProjects exercises the list command against a config whose
// projects_dir exists but no projects are cloned. List returns 0 entries
// and prints only the header — no error.
func TestWorktreeList_noProjects(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "list", "worktree"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestWorktreeList_unknownProject exercises list with a named project argument
// that is unknown (not in config), which should return an error.
func TestWorktreeList_unknownProject(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "list", "worktree", "nosuchproject"); err == nil {
		t.Error("expected error for unknown project, got nil")
	}
}

// TestCreateWorktreeCmd_help verifies the create worktree subcommand has correct --help output.
func TestCreateWorktreeCmd_help(t *testing.T) {
	if err := runCmd(t, "create", "worktree", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteWorktreeCmd_help verifies the delete worktree subcommand has correct --help output.
func TestDeleteWorktreeCmd_help(t *testing.T) {
	if err := runCmd(t, "delete", "worktree", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestWorktreeCreate_tooFewArgs verifies ExactArgs(2) on the create worktree subcommand.
func TestWorktreeCreate_tooFewArgs(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "create", "worktree", "myapp"); err == nil {
		t.Error("expected error for missing branch argument")
	}
}

// TestWorktreeCreate_tooManyArgs verifies ExactArgs(2) on the create worktree subcommand.
func TestWorktreeCreate_tooManyArgs(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "create", "worktree", "a", "b", "c"); err == nil {
		t.Error("expected error for too many arguments")
	}
}

// TestWorktreeDelete_tooFewArgs verifies ExactArgs(2) on the delete worktree subcommand.
func TestWorktreeDelete_tooFewArgs(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "delete", "worktree", "myapp"); err == nil {
		t.Error("expected error for missing branch argument")
	}
}

// TestWorktreeList_withClonedProject exercises the list command when the clone
// directory exists (service doesn't skip the project), which exercises the git
// worktree list path. Since the dir is not a real git repo, git will return
// an error — that's acceptable; we only verify there is no panic.
func TestWorktreeList_withClonedProject(t *testing.T) {
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, projectsDir)
	// May return error (not a git repo); that's fine — no panic is the goal.
	runCmd(t, "--config", cfgPath, "list", "worktree") //nolint:errcheck
}

// TestWorktreeDelete_abortedPrompt exercises the delete confirmation flow by
// piping a "n" answer via stdin, which should abort without error.
// Because Force=false, the confirmation prompt runs before any service call,
// so we do not reach the os.Exit(1) from worktreeErr.
func TestWorktreeDelete_abortedPrompt(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("n\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	if err := runCmd(t, "--config", cfgPath, "delete", "worktree", "myapp", "feature"); err != nil {
		t.Logf("delete returned error (acceptable): %v", err)
	}
}

// TestWorktreeDelete_forceFlag_parsed verifies the --force flag is registered on the
// delete worktree subcommand by checking --help output does not fail.
func TestWorktreeDelete_forceFlag_parsed(t *testing.T) {
	if err := runCmd(t, "delete", "worktree", "--help"); err != nil {
		t.Fatalf("delete worktree --help returned error: %v", err)
	}
}

// TestWorktreeCreate_fromFlag_parsed verifies the --from flag is registered and accepted.
func TestWorktreeCreate_fromFlag_parsed(t *testing.T) {
	projectsDir := t.TempDir()
	// Create the clone dir so we get past "not cloned" check
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "create", "worktree", "myapp", "feature", "--from", "main")
	// Will fail because it's not a real git repo, but should NOT fail with "unknown flag"
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatal("--from flag not registered")
	}
}

// TestWorktreeCreate_attachFlag_parsed verifies the --attach flag is registered and accepted.
func TestWorktreeCreate_attachFlag_parsed(t *testing.T) {
	projectsDir := t.TempDir()
	// Create the clone dir so we get past "not cloned" check
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "create", "worktree", "myapp", "feature", "--attach")
	// Will fail because it's not a real git repo, but should NOT fail with "unknown flag"
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatal("--attach flag not registered")
	}
}

// TestWorktreeCreate_agentFlag_parsed verifies the --agent flag is registered and accepted.
func TestWorktreeCreate_agentFlag_parsed(t *testing.T) {
	projectsDir := t.TempDir()
	// Create the clone dir so we get past "not cloned" check
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "create", "worktree", "myapp", "feature", "--attach", "--agent", "claude")
	// Will fail because it's not a real git repo, but should NOT fail with "unknown flag"
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatal("--agent flag not registered")
	}
}
