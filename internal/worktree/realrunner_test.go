package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs git in dir with a deterministic identity, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// realRunnerRepos builds an upstream repo (with main + feat-x branches) and a
// clone of it, returning the clone dir. Skips when git is unavailable.
func realRunnerRepos(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "init")
	runGit(t, remote, "checkout", "-b", "feat-x")
	if err := os.WriteFile(filepath.Join(remote, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "feature")
	runGit(t, remote, "checkout", "main")

	clone := filepath.Join(root, "clone")
	runGit(t, root, "clone", remote, clone)
	return clone
}

func TestRealWorktreeRunner_RemotesAndBranches(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	remotes, err := r.Remotes(clone)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 1 || remotes[0] != "origin" {
		t.Errorf("remotes = %v, want [origin]", remotes)
	}

	branches, err := r.ListRemoteBranches(clone)
	if err != nil {
		t.Fatalf("ListRemoteBranches: %v", err)
	}
	got := make(map[string]bool)
	for _, b := range branches {
		got[b.Ref] = true
	}
	if !got["origin/main"] || !got["origin/feat-x"] {
		t.Errorf("remote branches = %+v, want origin/main and origin/feat-x", branches)
	}
}

func TestRealWorktreeRunner_HasLocalBranch(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	has, err := r.HasLocalBranch(clone, "main")
	if err != nil || !has {
		t.Errorf("HasLocalBranch(main) = %v, %v; want true, nil", has, err)
	}
	has, err = r.HasLocalBranch(clone, "nonexistent")
	if err != nil || has {
		t.Errorf("HasLocalBranch(nonexistent) = %v, %v; want false, nil", has, err)
	}
}

func TestRealWorktreeRunner_FetchAndFastForward(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	if err := r.Fetch(clone, "origin", "feat-x"); err != nil {
		t.Errorf("Fetch: %v", err)
	}
	if err := r.FetchAll(clone); err != nil {
		t.Errorf("FetchAll: %v", err)
	}
	// main is checked out and already up to date — fast-forward is a no-op.
	if err := r.FastForward(clone, "origin", "main"); err != nil {
		t.Errorf("FastForward: %v", err)
	}
	if err := r.Fetch(clone, "origin", "no-such-branch"); err == nil {
		t.Error("Fetch of missing branch should error")
	}
}

func TestRealWorktreeRunner_AddTrackingAndList(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	if err := r.Fetch(clone, "origin", "feat-x"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	wtPath := filepath.Join(filepath.Dir(clone), "wt-feat-x")
	if err := r.AddTracking(clone, wtPath, "feat-x", "origin/feat-x"); err != nil {
		t.Fatalf("AddTracking: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "feature.txt")); err != nil {
		t.Errorf("expected feature.txt in tracking worktree: %v", err)
	}

	wts, err := r.List(clone)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, w := range wts {
		if w.Branch == "feat-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("List did not include feat-x worktree: %+v", wts)
	}
}

func TestRealWorktreeRunner_AddNewBranchFromAndRemove(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	wtPath := filepath.Join(filepath.Dir(clone), "wt-new")
	if err := r.AddNewBranchFrom(clone, wtPath, "new-branch", "main"); err != nil {
		t.Fatalf("AddNewBranchFrom: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("expected worktree dir: %v", err)
	}
	if err := r.Remove(clone, wtPath); err != nil {
		t.Errorf("Remove: %v", err)
	}

	// AddNewBranch (no start point) on a second worktree.
	wtPath2 := filepath.Join(filepath.Dir(clone), "wt-new2")
	if err := r.AddNewBranch(clone, wtPath2, "new-branch2"); err != nil {
		t.Errorf("AddNewBranch: %v", err)
	}
}

func TestRealWorktreeRunner_AddExistingBranch(t *testing.T) {
	clone := realRunnerRepos(t)
	r := NewRealWorktreeRunner()

	// Create a local branch to check out into a worktree via Add.
	runGit(t, clone, "branch", "local-x", "main")
	wtPath := filepath.Join(filepath.Dir(clone), "wt-local-x")
	if err := r.Add(clone, wtPath, "local-x"); err != nil {
		t.Errorf("Add: %v", err)
	}
}
