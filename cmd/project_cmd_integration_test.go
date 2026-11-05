//go:build integration

package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/testutil"
)

// localCloneGit clones from a local source repo whatever URL it is asked for,
// so the configured repo URL can stay a realistic remote.
type localCloneGit struct {
	git.Runner
	source string
}

func (g localCloneGit) Clone(_, path, branch string) error {
	if err := g.Runner.Clone(g.source, path, branch); err != nil {
		return fmt.Errorf("local clone of %s: %w", g.source, err)
	}
	return nil
}

// setupCloneHerd points the package Herd at a projects_dir with one
// uncloned project whose clones come from source. It returns projects_dir.
func setupCloneHerd(t *testing.T, source string) string {
	t.Helper()
	projectsDir := t.TempDir()
	c := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	setTestConfig(t, c)
	orig := h
	h = herd.New(c, registry, herd.Deps{Git: localCloneGit{Runner: git.NewRealRunner(), source: source}})
	t.Cleanup(func() { h = orig })
	return projectsDir
}

func runCloneProject(t *testing.T, args []string, all bool) (stdout, stderr string, err error) {
	t.Helper()
	c := &CloneProjectCmd{}
	cobraCmd := c.Cobra()
	c.All = all // after Cobra(), which resets flag fields to their defaults
	var out, errOut bytes.Buffer
	cobraCmd.SetOut(&out)
	cobraCmd.SetErr(&errOut)
	err = c.Run(cobraCmd, args)
	return out.String(), errOut.String(), err
}

func TestCloneProject_single_printsProgressAndPath(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	testutil.InitRepo(t, source)
	projectsDir := setupCloneHerd(t, source)

	out, _, err := runCloneProject(t, []string{"myapp"}, false)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	want := "Cloning myapp... done\n  Path: " + filepath.Join(projectsDir, "github.com", "user", "myapp") + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCloneProject_all_printsProgress(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	testutil.InitRepo(t, source)
	setupCloneHerd(t, source)

	out, _, err := runCloneProject(t, nil, true)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if out != "Cloning myapp... done\n" {
		t.Errorf("output = %q, want the clone progress line", out)
	}
}

func TestCloneProject_all_reportsFailuresOnStderr(t *testing.T) {
	setupCloneHerd(t, filepath.Join(t.TempDir(), "does-not-exist"))

	out, errOut, err := runCloneProject(t, nil, true)
	if err == nil || !strings.Contains(err.Error(), "one or more clones failed") {
		t.Fatalf("Run() = %v, want the aggregate clone failure", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing on stdout for a failed clone", out)
	}
	if !strings.HasPrefix(errOut, "Error: failed to clone myapp: ") {
		t.Errorf("stderr = %q, want the per-project failure", errOut)
	}
}
