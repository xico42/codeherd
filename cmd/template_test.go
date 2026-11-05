package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/testutil"
)

// TestTemplate_noArgs_usesCurrentDir verifies that running "template" with no
// project resolvable and no flags produces an error.
func TestTemplate_noArgs_usesCurrentDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.toml"
	err := runCmd(t, "--config", cfgPath, "template")
	if err == nil {
		t.Fatal("expected error when project can't be resolved")
	}
	if !strings.Contains(err.Error(), "could not resolve project") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTemplate_tooManyArgs verifies MaximumNArgs(1) rejects extra arguments.
func TestTemplate_tooManyArgs(t *testing.T) {
	err := runCmd(t, "template", "dir1", "dir2")
	if err == nil {
		t.Fatal("expected error with too many args")
	}
}

// TestTemplate_help verifies the template command is registered and has help output.
func TestTemplate_help(t *testing.T) {
	err := runCmd(t, "template", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTemplate_projectFlag verifies the --project flag is accepted.
func TestTemplate_projectFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.toml"
	// With --project but no --branch, should fail on branch detection
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "main", dir)
	// No .herd files so it should succeed (no templates to process)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTemplate_dryRunFlag verifies the --dry-run flag is accepted.
func TestTemplate_dryRunFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.toml"
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "main", "--dry-run", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTemplate_processesHerdFiles verifies that .herd files are rendered.
func TestTemplate_processesHerdFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a .herd template file
	tmplContent := "PROJECT={{ .Project }}\nBRANCH={{ .Branch }}\n"
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte(tmplContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.toml")
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "feat", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the rendered file was written
	out := testutil.ReadFile(t, dir, ".env")
	if !strings.Contains(string(out), "PROJECT=myapp") {
		t.Errorf("expected PROJECT=myapp, got: %s", out)
	}
	if !strings.Contains(string(out), "BRANCH=feat") {
		t.Errorf("expected BRANCH=feat, got: %s", out)
	}
}

// TestTemplate_dryRun_doesNotWrite verifies --dry-run renders but doesn't write.
func TestTemplate_dryRun_doesNotWrite(t *testing.T) {
	dir := t.TempDir()

	tmplContent := "PROJECT={{ .Project }}\n"
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte(tmplContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.toml")
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "main", "--dry-run", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should NOT exist
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		t.Error("expected .env to not exist in dry-run mode")
	}
}

// TestTemplate_resolveProjectFromCloneDir verifies auto-detection when the directory
// is the clone dir (not a worktree subdirectory).
func TestTemplate_resolveProjectFromCloneDir(t *testing.T) {
	projectsDir := t.TempDir()
	// Simulate the clone directory path: <projectsDir>/github.com/user/myapp
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o700); err != nil {
		t.Fatal(err)
	}

	tmplContent := "PROJECT={{ .Project }}\n"
	if err := os.WriteFile(filepath.Join(cloneDir, ".env.herd"), []byte(tmplContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfig(t, projectsDir)
	// Auto-detect project from clone dir; supply branch explicitly to avoid git dependency.
	err := runCmd(t, "--config", cfgPath, "template", "--branch", "main", cloneDir)
	if err != nil {
		t.Fatalf("unexpected error resolving project from clone dir: %v", err)
	}
}

// TestTemplate_resolveProjectFromDir verifies auto-detection from directory path.
func TestTemplate_resolveProjectFromDir(t *testing.T) {
	projectsDir := t.TempDir()
	// Simulate a worktree directory inside the project's worktrees root
	wtDir := filepath.Join(projectsDir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(wtDir, 0o700); err != nil {
		t.Fatal(err)
	}

	tmplContent := "PROJECT={{ .Project }}\n"
	if err := os.WriteFile(filepath.Join(wtDir, ".env.herd"), []byte(tmplContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "template", "--branch", "feat", wtDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := testutil.ReadFile(t, wtDir, ".env")
	if !strings.Contains(string(out), "PROJECT=myapp") {
		t.Errorf("expected PROJECT=myapp, got: %s", out)
	}
}

// TestTemplate_invalidTemplate_returnsError verifies that a template that
// fails to parse aborts the command with a processing error.
func TestTemplate_invalidTemplate_returnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("{{ .Project "), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.toml")
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "feat", dir)
	if err == nil || !strings.Contains(err.Error(), "processing templates") {
		t.Fatalf("expected a processing templates error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(statErr) {
		t.Errorf("no output file should be written for an invalid template, stat err = %v", statErr)
	}
}

// TestTemplate_branchNotDetectable verifies that a directory outside any git
// repo, with no --branch, fails with a pointer to the flag.
func TestTemplate_branchNotDetectable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", dir)
	if err == nil || !strings.Contains(err.Error(), "use --branch flag") {
		t.Fatalf("expected a branch detection error, got %v", err)
	}
}

// TestTemplate_unresolvableDir verifies that a relative dir that cannot be made
// absolute (the working directory was removed) is reported.
func TestTemplate_unresolvableDir(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "main", "rel")
	if err == nil || !strings.Contains(err.Error(), "resolving path") {
		t.Fatalf("expected a path resolution error, got %v", err)
	}
}
