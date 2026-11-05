//go:build integration

package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/testutil"
	"github.com/xico42/codeherd/internal/tmux"
)

// setupWorktreeHerd builds a cloned myapp project under a temp projects_dir and
// points the package Herd at real git and an isolated tmux server. It returns
// the clone dir.
func setupWorktreeHerd(t *testing.T, agents map[string]config.AgentConfig, defaultAgent string) string {
	t.Helper()
	testutil.IsolateTmux(t)
	projectsDir := t.TempDir()
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	testutil.InitRepo(t, cloneDir)

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
	testutil.InitRepo(t, remote)
	testutil.Git(t, remote, "branch", "feat-x")
	testutil.Git(t, cloneDir, "remote", "add", "origin", remote)

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
	testutil.Git(t, cloneDir, "worktree", "add", "-b", "feat", filepath.Join(cloneDir+"__worktrees", "feat"), "main")

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
