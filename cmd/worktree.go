package cmd

import (
	"bufio"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/filecopy"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
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
	svc := newWorktreeService()
	entries, err := svc.List(project)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tBRANCH\tPATH\tSESSION")
	for _, e := range entries {
		sess := e.Session
		if sess == "" {
			sess = "--"
		}
		branch := e.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Project, branch, e.Path, sess)
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

	projCfg := cfg.Projects[project]
	h := hooks.New(projCfg.Hooks)

	var cloneDir string
	if repoPath, rpErr := config.RepoPath(projCfg.Repo); rpErr == nil {
		cloneDir = filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
	}

	svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
	var result worktree.NewResult
	var err error
	switch {
	case c.Track != "":
		fmt.Fprintf(cmd.OutOrStdout(), "Checking out %s into a new worktree...  ", c.Track)
		result, err = svc.NewTracking(project, posBranch, c.Track)
	case c.From != "":
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, posBranch)
		result, err = svc.NewFrom(project, posBranch, c.From)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, posBranch)
		result, err = svc.New(project, posBranch)
	}
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return worktreeErr(cmd, project, posBranch, err)
	}

	branch := result.Branch

	// File copy
	if len(projCfg.Files) > 0 {
		copySvc := filecopy.New(h)
		attrs := map[string]string{
			semconv.HookAttrProject:      project,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: result.Path,
		}
		if err := copySvc.Copy(projCfg.Files, cloneDir, result.Path, attrs); err != nil {
			return fmt.Errorf("copying files: %w", err)
		}
	}

	// Template processing
	tmplSvc := herdtemplate.New(h)
	tmplAttrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       branch,
		semconv.HookAttrWorktreePath: result.Path,
	}
	if _, err := tmplSvc.Process(herdtemplate.ProcessContext{
		Project:      project,
		Branch:       branch,
		WorktreePath: result.Path,
		SessionName:  semconv.SessionName("", project, branch),
	}, tmplAttrs); err != nil {
		return fmt.Errorf("processing templates: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "done")
	fmt.Fprintf(cmd.OutOrStdout(), "  Path: %s\n", result.Path)

	if c.Attach {
		flagAgent := ""
		if cmd.Flags().Changed("agent") {
			flagAgent = c.Agent
		}
		agentName, err := resolveAgentName(flagAgent)
		if err != nil {
			return err
		}
		agent, err := cfg.AgentByName(agentName)
		if err != nil {
			return fmt.Errorf("resolving agent: %w", err)
		}

		name := semconv.SessionName("", project, branch)
		fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", name)

		sesSvc := session.NewService(tmux.NewClient(tmux.NewRealRunner()), h)
		sessionID, err := sesSvc.Start(session.StartRequest{
			Project:  project,
			Branch:   branch,
			Path:     result.Path,
			CloneDir: cloneDir,
			Cmd:      agent.Command(),
			Env:      agent.Env,
			Attach:   true,
		})
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return fmt.Errorf("starting session: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		return execTmuxAttach(sessionID)
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
	svc := newWorktreeService()
	err := svc.Delete(worktree.DeleteRequest{
		Project: project,
		Branch:  branch,
		Force:   c.Force,
	})
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return worktreeErr(cmd, project, branch, err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "done")
	return nil
}
