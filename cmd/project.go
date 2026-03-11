package cmd

import (
	"errors"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/project"
)

var projectCmd = &cobra.Command{
	Use:     "project",
	Short:   "Manage projects",
	GroupID: "projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured projects",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})
		entries := svc.List()
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NAME\tREPO\tBRANCH")
		for _, e := range entries {
			fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.Config.Repo, e.Config.DefaultBranch)
		}
		return w.Flush()
	},
}

var projectShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show config for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})
		e, err := svc.Show(args[0])
		if err != nil {
			return fmt.Errorf("show project: %w", err)
		}
		cloned := "no"
		if e.Cloned {
			cloned = "yes"
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Name:\t%s\n", e.Name)
		fmt.Fprintf(w, "Repo:\t%s\n", e.Config.Repo)
		fmt.Fprintf(w, "Branch:\t%s\n", e.Config.DefaultBranch)
		fmt.Fprintf(w, "Path:\t%s\n", e.Path)
		fmt.Fprintf(w, "Cloned:\t%s\n", cloned)
		return w.Flush()
	},
}

var cloneAll bool

var projectCloneCmd = &cobra.Command{
	Use:   "clone [<name>]",
	Short: "Clone a project's repo into projects_dir",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cloneAll {
			names := make([]string, 0, len(cfg.Projects))
			for name := range cfg.Projects {
				names = append(names, name)
			}
			sort.Strings(names)
			hadFailure := false
			for _, name := range names {
				projCfg := cfg.Projects[name]
				h := hooks.New(projCfg.Hooks)
				svc := project.NewService(cfg, project.NewRealGitRunner(), h)
				err := svc.Clone(name)
				switch {
				case err == nil:
					fmt.Fprintf(cmd.OutOrStdout(), "Cloning %s... done\n", name)
				default:
					var ace *project.AlreadyClonedError
					if errors.As(err, &ace) {
						fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", ace)
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "Error: failed to clone %s: %v\n", name, err)
						hadFailure = true
					}
				}
			}
			if hadFailure {
				return fmt.Errorf("one or more clones failed")
			}
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("requires a project name, or use --all")
		}
		name := args[0]
		projCfg := cfg.Projects[name]
		h := hooks.New(projCfg.Hooks)
		svc := project.NewService(cfg, project.NewRealGitRunner(), h)
		fmt.Fprintf(cmd.OutOrStdout(), "Cloning %s... ", name)
		err := svc.Clone(name)
		switch {
		case err == nil:
			fmt.Fprintln(cmd.OutOrStdout(), "done")
			if e, showErr := svc.Show(name); showErr == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Path: %s\n", e.Path)
			}
		default:
			fmt.Fprintln(cmd.OutOrStdout()) // newline after "Cloning..."
			var ace *project.AlreadyClonedError
			if errors.As(err, &ace) {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", ace)
			} else {
				return fmt.Errorf("failed to clone %s: %w", name, err)
			}
		}
		return nil
	},
}

func init() {
	projectCloneCmd.Flags().BoolVar(&cloneAll, "all", false, "clone all configured projects")
	projectCmd.AddCommand(projectCloneCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectShowCmd)
	rootCmd.AddCommand(projectCmd)
}
