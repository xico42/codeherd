package cmd

import (
	"bufio"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/herd"
)

// ── list ─────────────────────────────────────────────────────────────────────

type ListWorktreeCmd struct{}

func (c *ListWorktreeCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:               "worktree [project]",
		Aliases:           []string{"worktrees", "wt"},
		Short:             "List worktrees (all projects, or a single project)",
		Args:              cobra.MaximumNArgs(1),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectOnly,
	}
}

func (c *ListWorktreeCmd) Run(cmd *cobra.Command, args []string) error {
	project := ""
	if len(args) == 1 {
		project = args[0]
	}
	spaces, err := h.List(project)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tBRANCH\tPATH\tSESSION")
	for _, ws := range spaces {
		sess := "--"
		if ws.Agent != nil {
			sess = ws.Agent.Ref.CanonicalName() + " (running)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ws.Ref.Project, ws.BranchLabel(), ws.Path, sess)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}
	return nil
}

// ── create ───────────────────────────────────────────────────────────────────

type CreateWorktreeCmd struct {
	From   string
	Track  string
	Attach bool
	Agent  string
}

func (c *CreateWorktreeCmd) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "worktree <project> [branch]",
		Aliases:           []string{"worktrees", "wt"},
		Short:             "Create a new worktree for a project",
		Args:              cobra.RangeArgs(1, 2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectOnly,
	}
	cmd.Flags().StringVar(&c.From, "from", "", "base branch to create worktree from")
	cmd.Flags().StringVar(&c.Track, "track", "", "remote branch to fetch and check out (e.g. feat-x or upstream/feat-x)")
	cmd.Flags().BoolVar(&c.Attach, "attach", false, "start a coding session after creation")
	cmd.Flags().StringVar(&c.Agent, "agent", "", "agent to use for the session (with --attach)")
	cmd.MarkFlagsMutuallyExclusive("from", "track")
	_ = cmd.RegisterFlagCompletionFunc("from", completeBranches)
	_ = cmd.RegisterFlagCompletionFunc("track", completeRemoteBranches)
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgents)
	return cmd
}

func (c *CreateWorktreeCmd) Run(cmd *cobra.Command, args []string) error {
	project := args[0]
	posBranch := ""
	if len(args) > 1 {
		posBranch = args[1]
	}
	if c.Track == "" && posBranch == "" {
		return fmt.Errorf("a <branch> argument is required unless --track is given")
	}

	switch {
	case c.Track != "":
		fmt.Fprintf(cmd.OutOrStdout(), "Checking out %s into a new worktree...  ", c.Track)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, posBranch)
	}

	ws, err := h.EnsureWorkspace(h.Ref(project, posBranch), herd.EnsureOpts{
		AutoClone:  false, // the CLI never auto-clones — previously implicit, now stated
		Provision:  true,
		StartPoint: c.From,
		Track:      c.Track,
	})
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return herdErr(project, posBranch, err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "done")
	fmt.Fprintf(cmd.OutOrStdout(), "  Path: %s\n", ws.Path)

	if c.Attach {
		flagAgent := ""
		if cmd.Flags().Changed("agent") {
			flagAgent = c.Agent
		}
		// ws.Ref is authoritative — Track may have derived a different local branch.
		fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", ws.Ref.CanonicalName())
		handle, err := h.Launch(ws.Ref, herd.LaunchOpts{Agent: flagAgent, Attach: true})
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return herdErr(ws.Ref.Project, ws.Ref.Branch, err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "done")
		return execTmuxAttach(handle.ID)
	}

	return nil
}

// ── delete ───────────────────────────────────────────────────────────────────

type DeleteWorktreeCmd struct {
	Force bool
}

func (c *DeleteWorktreeCmd) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "worktree <project> <branch>",
		Aliases:           []string{"worktrees", "wt"},
		Short:             "Delete a worktree",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
	cmd.Flags().BoolVar(&c.Force, "force", false, "skip confirmation and kill any active session")
	return cmd
}

func (c *DeleteWorktreeCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]

	if !c.Force {
		fmt.Fprintf(cmd.OutOrStdout(), "Delete worktree %s/%s? [y/N] ", project, branch)
		scanner := bufio.NewScanner(cmd.InOrStdin())
		scanner.Scan()
		if scanner.Text() != "y" && scanner.Text() != "Y" {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleting worktree %s/%s...  ", project, branch)
	if err := h.Teardown(h.Ref(project, branch), herd.TeardownOpts{Force: c.Force}); err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return herdErr(project, branch, err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "done")
	return nil
}
