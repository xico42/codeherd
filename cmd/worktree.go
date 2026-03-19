package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

func newWorktreeService() *worktree.Service {
	return worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Short:   "Manage git worktrees for configured projects",
	GroupID: "projects",
}

// ── list ─────────────────────────────────────────────────────────────────────

var worktreeListCmd = &cobra.Command{
	Use:   "list [project]",
	Short: "List worktrees (all projects, or a single project)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			session := e.Session
			if session == "" {
				session = "--"
			}
			branch := e.Branch
			if branch == "" {
				branch = "(detached)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Project, branch, e.Path, session)
		}
		return w.Flush()
	},
}

// ── new ──────────────────────────────────────────────────────────────────────

var (
	worktreeNewFrom   string
	worktreeNewAttach bool
	worktreeNewAgent  string
)

var worktreeNewCmd = &cobra.Command{
	Use:   "new <project> <branch>",
	Short: "Create a new worktree for a project",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, branch)

		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)
		svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
		var result worktree.NewResult
		var err error
		if worktreeNewFrom != "" {
			result, err = svc.NewFrom(project, branch, worktreeNewFrom)
		} else {
			result, err = svc.New(project, branch)
		}
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return worktreeErr(cmd, project, branch, err)
		}

		// File copy
		if len(projCfg.Files) > 0 {
			repoPath, _ := config.RepoPath(projCfg.Repo)
			cloneDir := filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
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
			SessionName:  semconv.SessionName(project, branch),
		}, tmplAttrs); err != nil {
			return fmt.Errorf("processing templates: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		fmt.Fprintf(cmd.OutOrStdout(), "  Path: %s\n", result.Path)

		if worktreeNewAttach {
			flagAgent := ""
			if cmd.Flags().Changed("agent") {
				flagAgent = worktreeNewAgent
			}
			agentName, err := resolveAgentName(flagAgent)
			if err != nil {
				return err
			}
			agent, err := cfg.AgentByName(agentName)
			if err != nil {
				return fmt.Errorf("resolving agent: %w", err)
			}

			name := semconv.SessionName(project, branch)
			fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", name)

			sesSvc := session.NewService(tmux.NewClient(tmux.NewRealRunner()), h)
			sessionID, err := sesSvc.Start(session.StartRequest{
				Project: project,
				Branch:  branch,
				Path:    result.Path,
				Cmd:     agent.Command(),
				Env:     agent.Env,
				Attach:  true,
			})
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout())
				return fmt.Errorf("starting session: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "done")
			return execTmuxAttach(sessionID)
		}

		return nil
	},
}

// ── delete ───────────────────────────────────────────────────────────────────

var worktreeForce bool

var worktreeDeleteCmd = &cobra.Command{
	Use:   "delete <project> <branch>",
	Short: "Delete a worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]

		if !worktreeForce {
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
			Force:   worktreeForce,
		})
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return worktreeErr(cmd, project, branch, err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		return nil
	},
}

// ── shell ─────────────────────────────────────────────────────────────────────

var worktreeShellCmd = &cobra.Command{
	Use:   "shell <project> <branch>",
	Short: "Open an interactive shell in a worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]
		svc := newWorktreeService()
		path, err := svc.WorktreePath(project, branch)
		if err != nil {
			return worktreeErr(cmd, project, branch, err)
		}

		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		if err := os.Chdir(path); err != nil {
			return fmt.Errorf("chdir %s: %w", path, err)
		}

		return syscall.Exec(shell, []string{shell}, os.Environ())
	},
}

// ── template ─────────────────────────────────────────────────────────────────

var worktreeTemplateDryRun bool

var worktreeTemplateCmd = &cobra.Command{
	Use:   "template <project> <branch>",
	Short: "Process .herd template files in a worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]

		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)
		svc := newWorktreeService()

		path, err := svc.WorktreePath(project, branch)
		if err != nil {
			return worktreeErr(cmd, project, branch, err)
		}

		if !worktreeTemplateDryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Processing templates...  ")
		}

		tmplSvc := herdtemplate.New(h)
		result, err := tmplSvc.Process(herdtemplate.ProcessContext{
			Project:      project,
			Branch:       branch,
			WorktreePath: path,
			SessionName:  semconv.SessionName(project, branch),
			DryRun:       worktreeTemplateDryRun,
		}, map[string]string{
			semconv.HookAttrProject:      project,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: path,
		})
		if err != nil {
			if !worktreeTemplateDryRun {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return fmt.Errorf("processing templates: %w", err)
		}

		if worktreeTemplateDryRun {
			for _, f := range result.Files {
				fmt.Fprintf(cmd.OutOrStdout(), "# %s\n%s\n", f.Target, f.Output)
			}
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		for _, f := range result.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.Target)
		}
		return nil
	},
}

// ── error helper ─────────────────────────────────────────────────────────────

func worktreeErr(cmd *cobra.Command, project, branch string, err error) error {
	switch {
	case errors.Is(err, worktree.ErrNotCloned):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s is not cloned. Run 'ch project clone %s' first.\n", project, project)
	case errors.Is(err, worktree.ErrWorktreeExists):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: worktree %s/%s already exists.\n", project, branch)
	case errors.Is(err, worktree.ErrWorktreeNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: worktree %s/%s not found. Run 'ch worktree new %s %s' first.\n", project, branch, project, branch)
	case errors.Is(err, worktree.ErrSessionRunning):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: session %s-%s is running. Stop it first or use --force.\n", project, branch)
	default:
		return err
	}
	os.Exit(1)
	return nil
}

func init() {
	worktreeNewCmd.Flags().StringVar(&worktreeNewFrom, "from", "", "base branch to create worktree from")
	worktreeNewCmd.Flags().BoolVar(&worktreeNewAttach, "attach", false, "start a coding session after creation")
	worktreeNewCmd.Flags().StringVar(&worktreeNewAgent, "agent", "", "agent to use for the session (with --attach)")

	worktreeDeleteCmd.Flags().BoolVar(&worktreeForce, "force", false, "skip confirmation and kill any active session")

	worktreeTemplateCmd.Flags().BoolVar(&worktreeTemplateDryRun, "dry-run", false, "print rendered output without writing")

	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeNewCmd)
	worktreeCmd.AddCommand(worktreeDeleteCmd)
	worktreeCmd.AddCommand(worktreeShellCmd)
	worktreeCmd.AddCommand(worktreeTemplateCmd)
	rootCmd.AddCommand(worktreeCmd)
}
