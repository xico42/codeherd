//go:build integration

package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeListWorktreeConfig writes a config with default_branch set so the main
// clone dir has a stable identity ("main") to fall back to when its HEAD is on
// another branch.
func writeListWorktreeConfig(t *testing.T, projectsDir string) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[defaults]
projects_dir = "` + projectsDir + `"

[projects.myapp]
repo = "git@github.com:user/myapp.git"
default_branch = "main"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestListWorktree_noSession_showsLiveBranch is the end-to-end guard for the
// geomonitor bug over the primary path: worktrees with NO session. It drives
// the real `ch list worktree` over a real git repo, covering the whole flow the
// TUI shares (h.List -> workspaceFrom -> resolveDisplay -> FormatBranchLabel).
//
// The main clone dir, whose slot is the configured default branch, stays
// labelled "main" with the checkout as a hint. Every other worktree shows git's
// live branch verbatim — the folder name never surfaces, and no divergence hint
// is invented without a session to prove one.
func TestListWorktree_noSession_showsLiveBranch(t *testing.T) {
	// list worktree reaches tmux via h.Sessions(); isolate so it neither reads
	// nor leaks into the developer's server.
	useIsolatedTmux(t)

	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	initBareRepo(t, cloneDir) // main branch + one commit

	// The main clone dir has a non-default branch checked out.
	gitRun(t, cloneDir, "checkout", "-b", "docs/rbac-epic")

	// A worktree whose directory name does not match its branch (geomonitor's
	// chore-cron-rework), and a normal one.
	mismatch := filepath.Join(cloneDir+"__worktrees", "chore-cron-rework")
	gitRun(t, cloneDir, "worktree", "add", "-b", "chore/restore-cron-rework", mismatch, "main")
	normal := filepath.Join(cloneDir+"__worktrees", "feat")
	gitRun(t, cloneDir, "worktree", "add", "-b", "feat", normal, "main")

	cfgPath := writeListWorktreeConfig(t, projectsDir)

	out := captureStdout(t, func() {
		if err := runCmd(t, "--config", cfgPath, "list", "worktree"); err != nil {
			t.Fatalf("list worktree: %v", err)
		}
	})

	// Main clone dir: slot is "main" (config), HEAD on docs/rbac-epic.
	if !strings.Contains(out, "main (on docs/rbac-epic)") {
		t.Errorf("main worktree should read %q, got:\n%s", "main (on docs/rbac-epic)", out)
	}
	// Mismatched worktree, no session: git's live branch, clean.
	if !strings.Contains(out, "chore/restore-cron-rework") {
		t.Errorf("mismatched worktree should show its live branch, got:\n%s", out)
	}
	// Normal worktree: its branch, clean.
	if !strings.Contains(out, "feat") {
		t.Errorf("normal worktree should show %q, got:\n%s", "feat", out)
	}
	// The folder name must never surface, and no hint may be invented.
	if strings.Contains(out, "chore-cron-rework (") || strings.Contains(out, "(on chore/restore-cron-rework)") {
		t.Errorf("non-session worktree must show the live branch without a hint, got:\n%s", out)
	}
	if strings.Contains(out, "docs/rbac-epic (on docs/rbac-epic)") {
		t.Errorf("head hint restated the branch, got:\n%s", out)
	}
}

// TestListWorktree_sessionDivergence_showsRecordedBranch covers the refinement:
// when a running session records the branch the worktree is for, and HEAD has
// since moved off it, the row shows the recorded branch with the live checkout
// as a hint. A shell session persists in tmux and stamps @codeherd_branch.
func TestListWorktree_sessionDivergence_showsRecordedBranch(t *testing.T) {
	useIsolatedTmux(t)

	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	initBareRepo(t, cloneDir)

	cfgPath := writeListWorktreeConfig(t, projectsDir)

	// Create a worktree + shell session for "feat" (records @codeherd_branch).
	if err := runCmd(t, "--config", cfgPath, "create", "session", "myapp", "feat", "--shell"); err != nil {
		t.Fatalf("create shell session: %v", err)
	}

	// Move HEAD off "feat" inside that worktree.
	wtPath := filepath.Join(cloneDir+"__worktrees", "feat")
	gitRun(t, wtPath, "checkout", "-b", "other")

	out := captureStdout(t, func() {
		if err := runCmd(t, "--config", cfgPath, "list", "worktree"); err != nil {
			t.Fatalf("list worktree: %v", err)
		}
	})

	if !strings.Contains(out, "feat (on other)") {
		t.Errorf("session-proven divergence should read %q, got:\n%s", "feat (on other)", out)
	}
}
