package cmd

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/herd"
)

// ── ListProjectCmd ───────────────────────────────────────────────────

type ListProjectCmd struct{}

func (c *ListProjectCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:     "project",
		Aliases: []string{"projects", "pr"},
		Short:   "List all configured projects",
		Args:    cobra.NoArgs,
		RunE:    c.Run,
	}
}

func (c *ListProjectCmd) Run(cmd *cobra.Command, args []string) error {
	entries := h.Projects()
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	w := newPrinter(tw)
	w.Println("NAME\tREPO\tBRANCH")
	for _, e := range entries {
		w.Printf("%s\t%s\t%s\n", e.Name, e.Config.Repo, e.Config.DefaultBranch)
	}
	return flushTable(tw, w)
}

// ── ShowProjectCmd ───────────────────────────────────────────────────

type ShowProjectCmd struct{}

func (c *ShowProjectCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:               "project <name>",
		Aliases:           []string{"projects", "pr"},
		Short:             "Show config for a project",
		Args:              cobra.ExactArgs(1),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectOnly,
	}
}

func (c *ShowProjectCmd) Run(cmd *cobra.Command, args []string) error {
	e, err := h.Project(args[0])
	if err != nil {
		return fmt.Errorf("show project: %w", err)
	}
	cloned := "no"
	if e.Cloned {
		cloned = "yes"
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	w := newPrinter(tw)
	w.Printf("Name:\t%s\n", e.Name)
	w.Printf("Repo:\t%s\n", e.Config.Repo)
	w.Printf("Branch:\t%s\n", e.Config.DefaultBranch)
	w.Printf("Path:\t%s\n", e.Path)
	w.Printf("Cloned:\t%s\n", cloned)
	return flushTable(tw, w)
}

// ── CloneProjectCmd ──────────────────────────────────────────────────

type CloneProjectCmd struct {
	All bool
}

func (c *CloneProjectCmd) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "project [<name>]",
		Aliases:           []string{"projects", "pr"},
		Short:             "Clone a project's repo into projects_dir",
		RunE:              c.Run,
		ValidArgsFunction: completeProjectOnly,
	}
	cmd.Flags().BoolVar(&c.All, "all", false, "clone all configured projects")
	return cmd
}

func (c *CloneProjectCmd) Run(cmd *cobra.Command, args []string) error {
	if c.All {
		return cloneAll(cmd)
	}
	if len(args) == 0 {
		return fmt.Errorf("requires a project name, or use --all")
	}
	return cloneOne(cmd, args[0])
}

// cloneAll clones every configured project, reporting each failure on stderr
// and carrying on; it fails at the end if any clone failed.
func cloneAll(cmd *cobra.Command) error {
	out := newPrinter(cmd.OutOrStdout())
	errOut := newPrinter(cmd.ErrOrStderr())
	hadFailure := false
	for _, p := range h.Projects() {
		err := h.Clone(p.Name)
		var ace *herd.AlreadyClonedError
		switch {
		case err == nil:
			out.Printf("Cloning %s... done\n", p.Name)
		case errors.As(err, &ace):
			out.Printf("Warning: %s\n", ace)
		default:
			errOut.Printf("Error: failed to clone %s: %v\n", p.Name, err)
			hadFailure = true
		}
	}
	if hadFailure {
		return fmt.Errorf("one or more clones failed")
	}
	return errors.Join(out.Err(), errOut.Err())
}

// cloneOne clones a single project. An already-cloned project is a warning,
// not a failure.
func cloneOne(cmd *cobra.Command, name string) error {
	out := newPrinter(cmd.OutOrStdout())
	out.Printf("Cloning %s... ", name)
	err := h.Clone(name)
	var ace *herd.AlreadyClonedError
	switch {
	case err == nil:
		out.Println("done")
		if e, showErr := h.Project(name); showErr == nil {
			out.Printf("  Path: %s\n", e.Path)
		}
	case errors.As(err, &ace):
		out.Println()
		out.Printf("Warning: %s\n", ace)
	default:
		out.Println()
		return fmt.Errorf("failed to clone %s: %w", name, err)
	}
	return out.Err()
}
