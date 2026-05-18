//go:build integration

package cmd_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTemplate_detectBranchFromGit verifies that when --branch is omitted, the
// branch is auto-detected via git from the target directory. Requires git.
func TestTemplate_detectBranchFromGit(t *testing.T) {
	repoDir := t.TempDir()
	for _, c := range [][]string{
		{"git", "init", repoDir},
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test"},
		{"git", "-C", repoDir, "checkout", "-b", "my-branch"},
		{"git", "-C", repoDir, "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v\n%s", c, err, out)
		}
	}

	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", repoDir)
	if err != nil && strings.Contains(err.Error(), "could not detect branch") {
		t.Fatalf("detectGitBranch failed to auto-detect branch: %v", err)
	}
}
