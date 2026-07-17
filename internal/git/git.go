// Package git wraps git command execution. It is a mechanism package: it
// never sees the config, the active profile, or a Ref — it only takes paths
// and refs it is handed. Exactly one real implementation exists; the
// interfaces exist so internal/herd can fake the exec boundary in tests.
package git

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// WorktreeInfo holds data from a single git worktree entry.
type WorktreeInfo struct {
	Path     string
	Branch   string // empty if detached HEAD
	Detached bool   // true when HEAD is detached (e.g. rebase in progress)
}

// RemoteBranch is one remote-tracking branch (e.g. origin/feature-x).
type RemoteBranch struct {
	Remote string
	Branch string
	Ref    string // "<remote>/<branch>"
}

// WorktreeRunner abstracts git worktree operations for testability.
type WorktreeRunner interface {
	Add(cloneDir, worktreePath, branch string) error
	AddNewBranch(cloneDir, worktreePath, branch string) error
	AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
	Remove(cloneDir, worktreePath string) error
	List(cloneDir string) ([]WorktreeInfo, error)

	Fetch(cloneDir, remote, branch string) error
	FetchAll(cloneDir string) error
	FastForward(cloneDir, remote, branch string) error
	Remotes(cloneDir string) ([]string, error)
	ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)
	AddTracking(cloneDir, worktreePath, branch, remoteRef string) error
	HasLocalBranch(cloneDir, branch string) (bool, error)
}

// CloneRunner abstracts git clone execution to enable testing.
type CloneRunner interface {
	Clone(repo, path, branch string) error
}

// Runner is the union both herd and its tests depend on. Splitting it
// further is out of scope: it sits at the exec boundary where one real
// implementation exists.
type Runner interface {
	WorktreeRunner
	CloneRunner
}

// RealRunner runs git commands via os/exec.
type RealRunner struct{}

// NewRealRunner returns a Runner backed by the system git binary.
func NewRealRunner() *RealRunner { return &RealRunner{} }

func (r *RealRunner) Add(cloneDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", worktreePath, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return nil
}

func (r *RealRunner) AddNewBranch(cloneDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add -b: %w\n%s", err, out)
	}
	return nil
}

func (r *RealRunner) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, startPoint)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add -b (from): %w\n%s", err, out)
	}
	return nil
}

func (r *RealRunner) Remove(cloneDir, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %w\n%s", err, out)
	}
	return nil
}

func (r *RealRunner) List(cloneDir string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreePorcelain(string(out)), nil
}

func (r *RealRunner) Fetch(cloneDir, remote, branch string) error {
	cmd := exec.Command("git", "fetch", remote, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch %s %s: %w\n%s", remote, branch, err, out)
	}
	return nil
}

func (r *RealRunner) FetchAll(cloneDir string) error {
	cmd := exec.Command("git", "fetch", "--all", "--prune")
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch --all: %w\n%s", err, out)
	}
	return nil
}

// FastForward advances the local branch to <remote>/<branch> without losing
// local commits. It first tries a non-checkout ref update (works when the
// branch is not checked out); if that fails it falls back to a fast-forward-only
// merge (for the clone's currently checked-out branch). Either failure is
// reported but treated as best-effort by callers.
func (r *RealRunner) FastForward(cloneDir, remote, branch string) error {
	refspec := branch + ":" + branch
	cmd := exec.Command("git", "fetch", remote, refspec)
	cmd.Dir = cloneDir
	if _, err := cmd.CombinedOutput(); err == nil {
		return nil
	}
	remoteRef := remote + "/" + branch
	cmd = exec.Command("git", "merge", "--ff-only", remoteRef)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fast-forward %s: %w\n%s", branch, err, out)
	}
	return nil
}

func (r *RealRunner) Remotes(cloneDir string) ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote: %w", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

func (r *RealRunner) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	return parseRemoteBranches(string(out)), nil
}

func (r *RealRunner) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	cmd := exec.Command("git", "worktree", "add", "--track", "-b", branch, worktreePath, remoteRef)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add --track: %w\n%s", err, out)
	}
	return nil
}

func (r *RealRunner) HasLocalBranch(cloneDir, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = cloneDir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, fmt.Errorf("git show-ref %s: %w", ref, err)
}

// Clone runs git clone. If branch is non-empty, passes --branch <branch>.
func (r *RealRunner) Clone(repo, path, branch string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repo, path)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
	}
	return nil
}

// parseWorktreePorcelain parses the output of `git worktree list --porcelain`.
// Blocks are separated by blank lines.
func parseWorktreePorcelain(output string) []WorktreeInfo {
	var result []WorktreeInfo
	var current WorktreeInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			current.Detached = true
		case line == "":
			if current.Path != "" {
				result = append(result, current)
				current = WorktreeInfo{}
			}
		}
	}
	if current.Path != "" {
		result = append(result, current)
	}
	return result
}

// parseRemoteBranches parses `git for-each-ref --format=%(refname:short) refs/remotes`.
// The remote name is the segment before the first slash; the rest is the branch
// (which may contain slashes). Symbolic */HEAD entries are skipped.
func parseRemoteBranches(output string) []RemoteBranch {
	var result []RemoteBranch
	for _, line := range strings.Split(output, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		idx := strings.Index(s, "/")
		if idx <= 0 {
			continue
		}
		remote, branch := s[:idx], s[idx+1:]
		if branch == "" || branch == "HEAD" {
			continue
		}
		result = append(result, RemoteBranch{Remote: remote, Branch: branch, Ref: s})
	}
	return result
}

// ParseRef splits a user-supplied ref into a remote and branch. When ref is
// "<remote>/<rest>" and <remote> matches a configured remote, it returns
// (remote, rest, true). Otherwise it defaults to ("origin", ref, false), which
// keeps branch names containing slashes (e.g. feature/login) intact.
func ParseRef(remotes []string, ref string) (remote, branch string, explicit bool) {
	if idx := strings.Index(ref, "/"); idx > 0 {
		candidate := ref[:idx]
		for _, r := range remotes {
			if r == candidate {
				return candidate, ref[idx+1:], true
			}
		}
	}
	return "origin", ref, false
}
