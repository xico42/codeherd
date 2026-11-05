package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

// TemplateCmd processes .herd template files in a directory.
// It is a root-level verb command with no subject, because `template`
// is a one-off operation on a directory rather than a resource CRUD verb.
type TemplateCmd struct {
	Project string
	Branch  string
	DryRun  bool
}

func (c *TemplateCmd) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template [dir]",
		Short: "Process .herd template files in a directory",
		Long: `Process all .herd template files in a directory, rendering them with
project and branch context. The project is resolved by matching the directory
against configured projects. The branch is detected from git. Use --project
and --branch flags as fallbacks.`,
		Args: cobra.MaximumNArgs(1),
		RunE: c.Run,
	}
	cmd.Flags().StringVar(&c.Project, "project", "", "project name (auto-detected from directory)")
	cmd.Flags().StringVar(&c.Branch, "branch", "", "branch name (auto-detected from git)")
	cmd.Flags().BoolVar(&c.DryRun, "dry-run", false, "print rendered output without writing")
	return cmd
}

func (c *TemplateCmd) Run(cmd *cobra.Command, args []string) error {
	target, err := c.resolveTarget(args)
	if err != nil {
		return err
	}

	out := newPrinter(cmd.OutOrStdout())
	if !c.DryRun {
		out.Printf("Processing templates...  ")
	}

	hook := hooks.New(cfg.Projects[target.project].Hooks)
	result, err := herdtemplate.New(hook).Process(herdtemplate.ProcessContext{
		Project:      target.project,
		Branch:       target.branch,
		WorktreePath: target.dir,
		SessionName:  h.Ref(target.project, target.branch).CanonicalName(),
		DryRun:       c.DryRun,
	}, map[string]string{
		semconv.HookAttrProject:      target.project,
		semconv.HookAttrBranch:       target.branch,
		semconv.HookAttrWorktreePath: target.dir,
	})
	if err != nil {
		if !c.DryRun {
			out.Println()
		}
		return fmt.Errorf("processing templates: %w", err)
	}

	if c.DryRun {
		for _, f := range result.Files {
			out.Printf("# %s\n%s\n", f.Target, f.Output)
		}
		return out.Err()
	}

	out.Println("done")
	for _, f := range result.Files {
		out.Printf("  %s\n", f.Target)
	}
	return out.Err()
}

// templateTarget is what a template run renders for.
type templateTarget struct {
	dir, project, branch string
}

// resolveTarget picks the directory to render and its project and branch. The
// flags win; otherwise the project comes from where the directory sits under
// projects_dir and the branch from git.
func (c *TemplateCmd) resolveTarget(args []string) (templateTarget, error) {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return templateTarget{}, fmt.Errorf("resolving path: %w", err)
	}

	project := c.Project
	if project == "" {
		project = resolveProjectFromDir(absDir)
	}
	if project == "" {
		return templateTarget{}, fmt.Errorf("could not resolve project for %s; use --project flag", absDir)
	}

	branch := c.Branch
	if branch == "" {
		branch = detectGitBranch(absDir)
	}
	if branch == "" {
		return templateTarget{}, fmt.Errorf("could not detect branch for %s; use --branch flag", absDir)
	}
	return templateTarget{dir: absDir, project: project, branch: branch}, nil
}

// resolveProjectFromDir matches a directory against configured projects by checking
// if the directory is inside any project's clone dir or worktrees root.
func resolveProjectFromDir(absDir string) string {
	for name, p := range cfg.Projects {
		if p.Repo == "" {
			continue
		}
		repoPath, err := config.RepoPath(p.Repo)
		if err != nil {
			continue
		}
		cloneDir := semconv.CloneDir(cfg.Defaults.ProjectsDir, repoPath)
		worktreesRoot := semconv.WorktreesRoot(cfg.Defaults.ProjectsDir, repoPath)

		if absDir == cloneDir || strings.HasPrefix(absDir, cloneDir+string(os.PathSeparator)) {
			return name
		}
		if absDir == worktreesRoot || strings.HasPrefix(absDir, worktreesRoot+string(os.PathSeparator)) {
			return name
		}
	}
	return ""
}

// detectGitBranch runs git rev-parse to get the current branch name.
func detectGitBranch(dir string) string {
	out, _, err := git.Run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return ""
	}
	return branch
}
