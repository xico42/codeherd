package cmd

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

// loadCompletionConfig loads config during a shell-completion call.
// PersistentPreRunE does not run before completion functions, so the cfg
// global is nil here. It reads the --config and --profile flag values off
// cmd and applies the same profile precedence as the runtime via
// resolveProfileArg (main_profile < CODEHERD_PROFILE < --profile). On any
// load error it returns an empty (non-nil) config so callers need no nil
// checks. Declared as a var so tests can stub it.
var loadCompletionConfig = func(cmd *cobra.Command) *config.Config {
	c, _, err := config.Load(completionFlag(cmd, "config"), resolveProfileArg(completionFlag(cmd, "profile")))
	if err != nil {
		return &config.Config{}
	}
	return c
}

// completionFlag returns the string value of a persistent flag (possibly
// inherited from a parent command), or "" when the flag is not defined.
func completionFlag(cmd *cobra.Command, name string) string {
	if f := cmd.Root().PersistentFlags().Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}

// completeAgents completes against configured agent names.
func completeAgents(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return loadCompletionConfig(cmd).AgentNames(), cobra.ShellCompDirectiveNoFileComp
}

// completeProjects completes against configured project names.
func completeProjects(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg := loadCompletionConfig(cmd)
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completionProfileNames discovers profile names for completion. Declared
// as a var so tests can stub it.
var completionProfileNames = func(cmd *cobra.Command) []string {
	return config.ProfileNamesFor(completionFlag(cmd, "config"))
}

// completeProfiles completes against discoverable profile names. Works even
// when no profile is active (e.g. profiles_enabled with no main_profile).
func completeProfiles(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return completionProfileNames(cmd), cobra.ShellCompDirectiveNoFileComp
}

// completionBranchLister lists worktree entries for a project during
// completion. Declared as a var so tests can stub it without touching git
// or tmux.
var completionBranchLister = func(cfg *config.Config, project string) ([]worktree.ListEntry, error) {
	svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
	return svc.List(project)
}

// completeBranches completes against the worktree branches of the project
// named in args[0]. Returns nothing when no project is present or listing
// fails (e.g. project not cloned).
func completeBranches(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	entries, err := completionBranchLister(loadCompletionConfig(cmd), args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return branchNames(entries), cobra.ShellCompDirectiveNoFileComp
}

// branchNames returns the sorted, deduplicated, non-empty branch names
// from worktree entries.
func branchNames(entries []worktree.ListEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	var names []string
	for _, e := range entries {
		if e.Branch == "" {
			continue
		}
		if _, dup := seen[e.Branch]; dup {
			continue
		}
		seen[e.Branch] = struct{}{}
		names = append(names, e.Branch)
	}
	sort.Strings(names)
	return names
}

// completeProjectThenBranch completes the <project> positional (arg 0),
// then the <branch> positional (arg 1) against that project's worktrees.
// Used by commands operating on an existing branch.
func completeProjectThenBranch(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeProjects(cmd, args, toComplete)
	case 1:
		return completeBranches(cmd, args, toComplete)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeProjectOnly completes only the <project> positional (arg 0).
// Later positionals (e.g. a new branch name) get no completion.
func completeProjectOnly(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeProjects(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
