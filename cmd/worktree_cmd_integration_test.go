//go:build integration

package cmd

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/tmux"
)

// isolateTmux points every tmux call at a per-test server and skips the test
// when tmux cannot daemonize (sandboxed CI).
func isolateTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	t.Setenv("TMUX", "")
	probe := exec.Command("tmux", "-S", socket, "new-session", "-d", "-s", "__probe__", "sleep", "30")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux daemonize unavailable: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
	})
}

// gitIn runs git in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initRepo creates a repo on branch main with one commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	gitIn(t, dir, "config", "user.email", "test@test.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "init")
}

// setupWorktreeHerd builds a cloned myapp project under a temp projects_dir and
// points the package Herd at real git and an isolated tmux server. It returns
// the clone dir.
func setupWorktreeHerd(t *testing.T, agents map[string]config.AgentConfig, defaultAgent string) string {
	t.Helper()
	isolateTmux(t)
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	initRepo(t, cloneDir)

	c := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir, Agent: defaultAgent},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
		Agents: agents,
	}
	setTestConfig(t, c)
	orig := h
	h = herd.New(c, registry, herd.Deps{Tmux: tmux.NewRealRunner(), Git: git.NewRealRunner()})
	t.Cleanup(func() { h = orig })
	return cloneDir
}

func runCreateWorktree(t *testing.T, args []string, setFlags func(*CreateWorktreeCmd)) (string, error) {
	t.Helper()
	c := &CreateWorktreeCmd{}
	cobraCmd := c.Cobra()
	if setFlags != nil {
		setFlags(c) // after Cobra(), which resets flag fields to their defaults
	}
	var out bytes.Buffer
	cobraCmd.SetOut(&out)
	err := c.Run(cobraCmd, args)
	return out.String(), err
}

func TestCreateWorktree_printsProgressAndPath(t *testing.T) {
	cloneDir := setupWorktreeHerd(t, nil, "")

	out, err := runCreateWorktree(t, []string{"myapp", "feat"}, nil)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	want := "Creating worktree myapp/feat...  done\n  Path: " + filepath.Join(cloneDir+"__worktrees", "feat") + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCreateWorktree_track_printsCheckoutProgress(t *testing.T) {
	cloneDir := setupWorktreeHerd(t, nil, "")
	remote := filepath.Join(t.TempDir(), "remote")
	initRepo(t, remote)
	gitIn(t, remote, "branch", "feat-x")
	gitIn(t, cloneDir, "remote", "add", "origin", remote)

	out, err := runCreateWorktree(t, []string{"myapp"}, func(c *CreateWorktreeCmd) { c.Track = "feat-x" })
	if err != nil {
		t.Fatalf("Run() = %v, want nil (output %q)", err, out)
	}
	if !strings.HasPrefix(out, "Checking out feat-x into a new worktree...  done\n") {
		t.Errorf("output = %q, want the track progress line", out)
	}
}

func TestCreateWorktree_attach_startsSessionAndAttaches(t *testing.T) {
	setupWorktreeHerd(t, map[string]config.AgentConfig{
		"sleeper": {Cmd: "sleep", Args: []string{"30"}},
	}, "sleeper")

	origExec := execTmuxAttach
	t.Cleanup(func() { execTmuxAttach = origExec })
	var attachedTo string
	execTmuxAttach = func(id string) error {
		attachedTo = id
		return nil
	}

	out, err := runCreateWorktree(t, []string{"myapp", "feat"}, func(c *CreateWorktreeCmd) { c.Attach = true })
	if err != nil {
		t.Fatalf("Run() = %v, want nil (output %q)", err, out)
	}
	if !strings.HasSuffix(out, "Starting session myapp-feat...  done\n") {
		t.Errorf("output = %q, want the session progress line to end in done", out)
	}
	if attachedTo == "" {
		t.Error("execTmuxAttach was not called")
	}
}

func TestCreateWorktree_attach_launchError(t *testing.T) {
	setupWorktreeHerd(t, nil, "") // no agent configured, so Launch fails

	out, err := runCreateWorktree(t, []string{"myapp", "feat"}, func(c *CreateWorktreeCmd) { c.Attach = true })
	if err == nil {
		t.Fatalf("Run() = nil, want a launch error (output %q)", out)
	}
	if !strings.HasSuffix(out, "Starting session myapp-feat...  \n") {
		t.Errorf("output = %q, want the session progress line terminated by a newline", out)
	}
}

func runDeleteWorktree(t *testing.T, branch string) (string, error) {
	t.Helper()
	c := &DeleteWorktreeCmd{}
	cobraCmd := c.Cobra()
	c.Force = true // after Cobra(), which resets flag fields to their defaults
	var out bytes.Buffer
	cobraCmd.SetOut(&out)
	err := c.Run(cobraCmd, []string{"myapp", branch})
	return out.String(), err
}

func TestDeleteWorktree_force_printsProgress(t *testing.T) {
	cloneDir := setupWorktreeHerd(t, nil, "")
	gitIn(t, cloneDir, "worktree", "add", "-b", "feat", filepath.Join(cloneDir+"__worktrees", "feat"), "main")

	out, err := runDeleteWorktree(t, "feat")
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if out != "Deleting worktree myapp/feat...  done\n" {
		t.Errorf("output = %q, want the delete progress line", out)
	}
}

func TestDeleteWorktree_missing_returnsError(t *testing.T) {
	setupWorktreeHerd(t, nil, "")

	out, err := runDeleteWorktree(t, "nope")
	if err == nil {
		t.Fatal("Run() = nil, want an error for a missing worktree")
	}
	if out != "Deleting worktree myapp/nope...  \n" {
		t.Errorf("output = %q, want the progress line terminated by a newline", out)
	}
}
