//go:build integration

package cmd_test

import (
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/testutil"
)

// TestTemplate_detectBranchFromGit verifies that when --branch is omitted, the
// branch is auto-detected via git from the target directory. Requires git.
func TestTemplate_detectBranchFromGit(t *testing.T) {
	repoDir := t.TempDir()
	testutil.InitRepo(t, repoDir)
	testutil.Git(t, repoDir, "checkout", "-b", "my-branch")

	projectsDir := t.TempDir()
	cfgPath := writeConfig(t, projectsDir)
	err := runCmd(t, "--config", cfgPath, "template", "--project", "myapp", repoDir)
	if err != nil && strings.Contains(err.Error(), "could not detect branch") {
		t.Fatalf("detectGitBranch failed to auto-detect branch: %v", err)
	}
}
