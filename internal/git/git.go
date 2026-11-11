// Package git wraps git command execution. It is a mechanism package: it
// never sees the config, the active profile, or a Ref — it only takes paths
// and refs it is handed. Exactly one real implementation exists; the
// interfaces exist so internal/herd can fake the exec boundary in tests.
package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"slices"
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

// Run executes git with args in dir (the current directory when dir is "")
// and returns its stdout and stderr. It is the package's only exec site.
//
// Every caller puts --end-of-options before the arguments that come from
// outside — branch names, start points, paths, remotes — so a value such as
// "--upload-pack=…" reaches git as an argument and is never parsed as an
// option.
func Run(dir string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, and callers end option parsing before outside input
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("running git: %w", err)
	}
	return outBuf.Bytes(), errBuf.Bytes(), nil
}

// run is Run for commands whose output only matters on failure, where git's
// stderr is folded into the error.
func run(dir, what string, args ...string) error {
	if _, stderr, err := Run(dir, args...); err != nil {
		return fmt.Errorf("%s: %w\n%s", what, err, stderr)
	}
	return nil
}

// RealRunner runs git commands via os/exec.
type RealRunner struct{}

// NewRealRunner returns a Runner backed by the system git binary.
func NewRealRunner() *RealRunner { return &RealRunner{} }

func (r *RealRunner) Add(cloneDir, worktreePath, branch string) error {
	return run(cloneDir, "git worktree add",
		"worktree", "add", "--end-of-options", worktreePath, branch)
}

func (r *RealRunner) AddNewBranch(cloneDir, worktreePath, branch string) error {
	return run(cloneDir, "git worktree add -b",
		"worktree", "add", "-b", branch, "--end-of-options", worktreePath)
}

func (r *RealRunner) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	return run(cloneDir, "git worktree add -b (from)",
		"worktree", "add", "-b", branch, "--end-of-options", worktreePath, startPoint)
}

func (r *RealRunner) Remove(cloneDir, worktreePath string) error {
	return run(cloneDir, "git worktree remove",
		"worktree", "remove", "--end-of-options", worktreePath)
}

func (r *RealRunner) List(cloneDir string) ([]WorktreeInfo, error) {
	out, _, err := Run(cloneDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreePorcelain(string(out)), nil
}

func (r *RealRunner) Fetch(cloneDir, remote, branch string) error {
	return run(cloneDir, fmt.Sprintf("git fetch %s %s", remote, branch),
		"fetch", "--end-of-options", remote, branch)
}

func (r *RealRunner) FetchAll(cloneDir string) error {
	return run(cloneDir, "git fetch --all", "fetch", "--all", "--prune")
}

// FastForward advances the local branch to <remote>/<branch> without losing
// local commits. It first tries a non-checkout ref update (works when the
// branch is not checked out); if that fails it falls back to a fast-forward-only
// merge (for the clone's currently checked-out branch). Either failure is
// reported but treated as best-effort by callers.
func (r *RealRunner) FastForward(cloneDir, remote, branch string) error {
	refspec := branch + ":" + branch
	if _, _, err := Run(cloneDir, "fetch", "--end-of-options", remote, refspec); err == nil {
		return nil
	}
	return run(cloneDir, "fast-forward "+branch,
		"merge", "--ff-only", "--end-of-options", remote+"/"+branch)
}

func (r *RealRunner) Remotes(cloneDir string) ([]string, error) {
	out, _, err := Run(cloneDir, "remote")
	if err != nil {
		return nil, fmt.Errorf("git remote: %w", err)
	}
	var names []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

func (r *RealRunner) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	out, _, err := Run(cloneDir, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	return parseRemoteBranches(string(out)), nil
}

func (r *RealRunner) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	return run(cloneDir, "git worktree add --track",
		"worktree", "add", "--track", "-b", branch, "--end-of-options", worktreePath, remoteRef)
}

func (r *RealRunner) HasLocalBranch(cloneDir, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	_, _, err := Run(cloneDir, "show-ref", "--verify", "--quiet", "--end-of-options", ref)
	if err == nil {
		return true, nil
	}
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
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
	args = append(args, "--end-of-options", repo, path)
	return run("", "git clone", args...)
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
		case line == "" && current.Path != "":
			result = append(result, current)
			current = WorktreeInfo{}
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
	for line := range strings.SplitSeq(output, "\n") {
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
		if slices.Contains(remotes, candidate) {
			return candidate, ref[idx+1:], true
		}
	}
	return "origin", ref, false
}
