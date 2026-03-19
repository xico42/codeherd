package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

var (
	templateProject string
	templateBranch  string
	templateDryRun  bool
)

var templateCmd = &cobra.Command{
	Use:   "template [dir]",
	Short: "Process .herd template files in a directory",
	Long: `Process all .herd template files in a directory, rendering them with
project and branch context. The project is resolved by matching the directory
against configured projects. The branch is detected from git. Use --project
and --branch flags as fallbacks.`,
	Args:    cobra.MaximumNArgs(1),
	GroupID: "projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		project := templateProject
		if project == "" {
			project = resolveProjectFromDir(absDir)
		}
		if project == "" {
			return fmt.Errorf("could not resolve project for %s; use --project flag", absDir)
		}

		branch := templateBranch
		if branch == "" {
			branch = detectGitBranch(absDir)
		}
		if branch == "" {
			return fmt.Errorf("could not detect branch for %s; use --branch flag", absDir)
		}

		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)

		if !templateDryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Processing templates...  ")
		}

		tmplSvc := herdtemplate.New(h)
		result, err := tmplSvc.Process(herdtemplate.ProcessContext{
			Project:      project,
			Branch:       branch,
			WorktreePath: absDir,
			SessionName:  semconv.SessionName(project, branch),
			DryRun:       templateDryRun,
		}, map[string]string{
			semconv.HookAttrProject:      project,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: absDir,
		})
		if err != nil {
			if !templateDryRun {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return fmt.Errorf("processing templates: %w", err)
		}

		if templateDryRun {
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
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return ""
	}
	return branch
}

func init() {
	templateCmd.Flags().StringVar(&templateProject, "project", "", "project name (auto-detected from directory)")
	templateCmd.Flags().StringVar(&templateBranch, "branch", "", "branch name (auto-detected from git)")
	templateCmd.Flags().BoolVar(&templateDryRun, "dry-run", false, "print rendered output without writing")
	rootCmd.AddCommand(templateCmd)
}
