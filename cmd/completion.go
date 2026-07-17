package cmd

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/tmux"
)

// ensureCompletionHerd builds the h global from the completion config when it
// is nil. PersistentPreRunE does not run before completion functions (the same
// reason loadCompletionConfig exists), so h is otherwise unset here.
func ensureCompletionHerd(cmd *cobra.Command) {
	if h == nil {
		h = herd.New(loadCompletionConfig(cmd), nil, herd.Deps{
			Tmux: tmux.NewRealRunner(),
			Git:  git.NewRealRunner(),
		})
	}
}

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

// completionBranchLister lists workspaces for a project during completion.
// Declared as a var so tests can stub it without touching git or tmux.
var completionBranchLister = func(project string) ([]herd.Workspace, error) {
	return h.List(project)
}

// completeBranches completes against the worktree branches of the project
// named in args[0]. Returns nothing when no project is present or listing
// fails (e.g. project not cloned).
func completeBranches(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ensureCompletionHerd(cmd)
	spaces, err := completionBranchLister(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return branchNames(spaces), cobra.ShellCompDirectiveNoFileComp
}

// branchNames returns the sorted, deduplicated, non-empty identity branch
// names from a project's workspaces. Identity — not the display branch — is
// what a user completing a command should type.
func branchNames(spaces []herd.Workspace) []string {
	seen := make(map[string]struct{}, len(spaces))
	var names []string
	for _, ws := range spaces {
		if ws.Ref.Branch == "" {
			continue
		}
		if _, dup := seen[ws.Ref.Branch]; dup {
			continue
		}
		seen[ws.Ref.Branch] = struct{}{}
		names = append(names, ws.Ref.Branch)
	}
	sort.Strings(names)
	return names
}

// completionRemoteBrancher lists a project's remote-tracking branches during
// completion (no fetch). Declared as a var so tests can stub it.
var completionRemoteBrancher = func(project string) ([]herd.RemoteBranch, error) {
	return h.RemoteBranches(project, false) // no fetch — completion must stay fast
}

// completeRemoteBranches completes the --track flag against the remote-tracking
// branches of the project named in args[0].
func completeRemoteBranches(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ensureCompletionHerd(cmd)
	branches, err := completionRemoteBrancher(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	refs := make([]string, 0, len(branches))
	for _, b := range branches {
		refs = append(refs, b.Ref)
	}
	sort.Strings(refs)
	return refs, cobra.ShellCompDirectiveNoFileComp
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
