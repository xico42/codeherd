package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.toml")
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", "--branch", "feat", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the rendered file was written
	out, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("rendered file not found: %v", err)
	}
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
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte(tmplContent), 0o644); err != nil {
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

// TestTemplate_resolveProjectFromDir verifies auto-detection from directory path.
func TestTemplate_resolveProjectFromDir(t *testing.T) {
	projectsDir := t.TempDir()
	// Simulate a worktree directory inside the project's worktrees root
	wtDir := filepath.Join(projectsDir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tmplContent := "PROJECT={{ .Project }}\n"
	if err := os.WriteFile(filepath.Join(wtDir, ".env.herd"), []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "template", "--branch", "feat", wtDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(wtDir, ".env"))
	if err != nil {
		t.Fatalf("rendered file not found: %v", err)
	}
	if !strings.Contains(string(out), "PROJECT=myapp") {
		t.Errorf("expected PROJECT=myapp, got: %s", out)
	}
}
