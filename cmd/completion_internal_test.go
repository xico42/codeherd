package cmd

import (
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/worktree"
)

// withStubConfig swaps loadCompletionConfig for the duration of a test.
func withStubConfig(t *testing.T, c *config.Config) {
	t.Helper()
	orig := loadCompletionConfig
	loadCompletionConfig = func(*cobra.Command) *config.Config { return c }
	t.Cleanup(func() { loadCompletionConfig = orig })
}

func TestCompleteRemoteBranches(t *testing.T) {
	orig := completionRemoteBrancher
	t.Cleanup(func() { completionRemoteBrancher = orig })
	completionRemoteBrancher = func(_ *config.Config, project string) ([]worktree.RemoteBranch, error) {
		return []worktree.RemoteBranch{
			{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
			{Remote: "upstream", Branch: "fix-y", Ref: "upstream/fix-y"},
		}, nil
	}

	cmd := (&CreateWorktreeCmd{}).Cobra()
	got, directive := completeRemoteBranches(cmd, []string{"myapp"}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v", directive)
	}
	want := []string{"origin/feat-x", "upstream/fix-y"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("completions = %v, want %v", got, want)
	}
}

func TestCompleteRemoteBranches_noProject(t *testing.T) {
	cmd := (&CreateWorktreeCmd{}).Cobra()
	got, _ := completeRemoteBranches(cmd, nil, "")
	if got != nil {
		t.Errorf("expected nil completions with no project, got %v", got)
	}
}

func TestCompleteAgents(t *testing.T) {
	withStubConfig(t, &config.Config{Agents: map[string]config.AgentConfig{
		"claude": {Cmd: "claude"},
		"aider":  {Cmd: "aider"},
	}})

	got, dir := completeAgents(&cobra.Command{}, nil, "")
	want := []string{"aider", "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteProfiles(t *testing.T) {
	orig := completionProfileNames
	completionProfileNames = func(*cobra.Command) []string { return []string{"home", "work"} }
	t.Cleanup(func() { completionProfileNames = orig })

	got, dir := completeProfiles(&cobra.Command{}, nil, "")
	want := []string{"home", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profiles = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteProjects_sorted(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{
		"zeta":  {},
		"alpha": {},
	}})

	got, dir := completeProjects(&cobra.Command{}, nil, "")
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestBranchNames_dedupAndSkipEmpty(t *testing.T) {
	entries := []worktree.ListEntry{
		{Project: "p", Branch: "main"},
		{Project: "p", Branch: ""},
		{Project: "p", Branch: "feature"},
		{Project: "p", Branch: "main"},
	}
	got := branchNames(entries)
	want := []string{"feature", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branchNames = %v, want %v", got, want)
	}
}

func TestCompleteBranches_needsProject(t *testing.T) {
	got, dir := completeBranches(&cobra.Command{}, nil, "")
	if got != nil {
		t.Fatalf("branches = %v, want nil when no project arg", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteBranches_listsForProject(t *testing.T) {
	orig := completionBranchLister
	var gotProject string
	completionBranchLister = func(_ *config.Config, project string) ([]worktree.ListEntry, error) {
		gotProject = project
		return []worktree.ListEntry{{Branch: "main"}, {Branch: "dev"}}, nil
	}
	t.Cleanup(func() { completionBranchLister = orig })
	withStubConfig(t, &config.Config{})

	got, dir := completeBranches(&cobra.Command{}, []string{"myproj"}, "")
	if gotProject != "myproj" {
		t.Fatalf("lister got project %q, want myproj", gotProject)
	}
	want := []string{"dev", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteBranches_listerErrorYieldsNothing(t *testing.T) {
	orig := completionBranchLister
	completionBranchLister = func(*config.Config, string) ([]worktree.ListEntry, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { completionBranchLister = orig })
	withStubConfig(t, &config.Config{})

	got, dir := completeBranches(&cobra.Command{}, []string{"myproj"}, "")
	if got != nil {
		t.Fatalf("branches = %v, want nil on error", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteProjectThenBranch_dispatch(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{"alpha": {}}})
	orig := completionBranchLister
	completionBranchLister = func(*config.Config, string) ([]worktree.ListEntry, error) {
		return []worktree.ListEntry{{Branch: "main"}}, nil
	}
	t.Cleanup(func() { completionBranchLister = orig })

	got, _ := completeProjectThenBranch(&cobra.Command{}, nil, "")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("pos0 = %v, want [alpha]", got)
	}
	got, _ = completeProjectThenBranch(&cobra.Command{}, []string{"alpha"}, "")
	if !reflect.DeepEqual(got, []string{"main"}) {
		t.Fatalf("pos1 = %v, want [main]", got)
	}
	got, _ = completeProjectThenBranch(&cobra.Command{}, []string{"alpha", "main"}, "")
	if got != nil {
		t.Fatalf("pos2 = %v, want nil", got)
	}
}

func TestCompleteProjectOnly_dispatch(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{"alpha": {}}})

	got, _ := completeProjectOnly(&cobra.Command{}, nil, "")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("pos0 = %v, want [alpha]", got)
	}
	got, _ = completeProjectOnly(&cobra.Command{}, []string{"alpha"}, "")
	if got != nil {
		t.Fatalf("pos1 = %v, want nil", got)
	}
}
