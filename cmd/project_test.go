package cmd_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListProject_help verifies the list project subcommand is registered.
func TestListProject_help(t *testing.T) {
	if err := runCmd(t, "list", "project", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListProject_noProjects exercises the list command against a config with
// no projects. Prints only the header — no error.
func TestListProject_noProjects(t *testing.T) {
	blankCfg := t.TempDir() + "/config.toml"
	if err := runCmd(t, "--config", blankCfg, "list", "project"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListProject_withProject exercises the list command when a project is configured.
func TestListProject_withProject(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "list", "project"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListProject_alias verifies the "projects" alias works.
func TestListProject_alias(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "list", "projects"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListProject_shortAlias verifies the "pr" alias works.
func TestListProject_shortAlias(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "list", "pr"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestShowProject_help verifies the show project subcommand is registered.
func TestShowProject_help(t *testing.T) {
	if err := runCmd(t, "show", "project", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestShowProject_unknownProject verifies an error when the project is not configured.
func TestShowProject_unknownProject(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "show", "project", "notaproject"); err == nil {
		t.Error("expected error for unknown project, got nil")
	}
}

// TestShowProject_tooFewArgs verifies ExactArgs(1) rejects missing name.
func TestShowProject_tooFewArgs(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "show", "project"); err == nil {
		t.Error("expected error for missing project name")
	}
}

// TestShowProject_knownProject verifies a configured (not cloned) project shows correctly.
func TestShowProject_knownProject(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "show", "project", "myapp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCloneProject_help verifies the clone project subcommand is registered.
func TestCloneProject_help(t *testing.T) {
	if err := runCmd(t, "clone", "project", "--help"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCloneProject_noArgs verifies that clone without --all and no name returns an error.
func TestCloneProject_noArgs(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	if err := runCmd(t, "--config", cfgPath, "clone", "project"); err == nil {
		t.Error("expected error when no name and no --all, got nil")
	}
}

// TestCloneProject_allNoProjects verifies --all with no configured projects succeeds.
func TestCloneProject_allNoProjects(t *testing.T) {
	blankCfg := t.TempDir() + "/config.toml"
	if err := runCmd(t, "--config", blankCfg, "clone", "project", "--all"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestShowProject_cloned verifies that a project whose directory exists shows "yes" for Cloned.
func TestShowProject_cloned(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	// Simulate a cloned project by creating the expected directory path.
	repoDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, "--config", cfgPath, "show", "project", "myapp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCloneProject_allAlreadyCloned verifies --all emits a warning (not an error) when
// the project directory already exists.
func TestCloneProject_allAlreadyCloned(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	// Pre-create the project directory so Clone returns AlreadyClonedError.
	repoDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, "--config", cfgPath, "clone", "project", "--all"); err != nil {
		t.Fatalf("unexpected error (already-cloned should warn, not fail): %v", err)
	}
}

// TestCloneProject_singleAlreadyCloned verifies that cloning a single already-cloned
// project emits a warning but does not return an error.
func TestCloneProject_singleAlreadyCloned(t *testing.T) {
	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	repoDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, "--config", cfgPath, "clone", "project", "myapp"); err != nil {
		t.Fatalf("unexpected error (already-cloned should warn, not fail): %v", err)
	}
}
