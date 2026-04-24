package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

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

// resolveAgentName returns the agent name from the flag or config default.
func resolveAgentName(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if cfg.Defaults.Agent != "" {
		return cfg.Defaults.Agent, nil
	}
	return "", fmt.Errorf("no agent specified; use --agent or set defaults.agent in config")
}

// execTmuxAttach attaches to a tmux session. If already inside tmux, uses
// switch-client. Otherwise, replaces the process with tmux attach-session.
var execTmuxAttach = func(name string) error {
	if os.Getenv("TMUX") != "" {
		tc := tmux.NewClient(tmux.NewRealRunner())
		if err := tc.SwitchClient(name); err != nil {
			return fmt.Errorf("switching client: %w", err)
		}
		return nil
	}
	tmuxBin, err := lookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}
	if err := syscall.Exec(tmuxBin, []string{"tmux", "attach-session", "-t", name}, os.Environ()); err != nil {
		return fmt.Errorf("exec tmux: %w", err)
	}
	return nil
}

// lookPath wraps exec.LookPath for testability.
var lookPath = func(file string) (string, error) {
	return exec.LookPath(file)
}

// ── list ─────────────────────────────────────────────────────────────────────

type ListSessionCmd struct{}

func (c *ListSessionCmd) Cobra() *cobra.Command {
	return &cobra.Command{
		Use:     "session",
		Aliases: []string{"sessions", "ses"},
		Short:   "List all active sessions",
		Args:    cobra.NoArgs,
		RunE:    c.Run,
	}
}

func (c *ListSessionCmd) Run(cmd *cobra.Command, _ []string) error {
	svc := newSessionService()
	sessions, err := listSessionsForProfile(svc)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SESSION\tTYPE\tSTATUS")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Type, s.Status)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}
	return nil
}

// ── show ─────────────────────────────────────────────────────────────────────

type ShowSessionCmd struct {
	Shell bool
}

func (c *ShowSessionCmd) Cobra() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:     "session <project> <branch>",
		Aliases: []string{"sessions", "ses"},
		Short:   "Show details for a session",
		Args:    cobra.ExactArgs(2),
		RunE:    c.Run,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	return cobraCmd
}

func (c *ShowSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	sessionType := sessionTypeFromFlag(c.Shell)
	svc := newSessionService()
	info, err := showSessionForProfile(svc, project, branch, sessionType)
	if err != nil {
		return sessionErr(cmd, err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Session:\t%s\n", info.Name)
	fmt.Fprintf(w, "Type:\t%s\n", info.Type)
	fmt.Fprintf(w, "Status:\t%s\n", info.Status)
	if info.Annotation != "" {
		fmt.Fprintf(w, "Annotation:\t%s\n", info.Annotation)
	}
	if !info.StartedAt.IsZero() {
		fmt.Fprintf(w, "Started:\t%s\n", info.StartedAt.Format(time.RFC3339))
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}
	return nil
}

// ── create ───────────────────────────────────────────────────────────────────

type CreateSessionCmd struct {
	Shell  bool
	Attach bool
	Agent  string
}

func (c *CreateSessionCmd) Cobra() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:     "session <project> <branch>",
		Aliases: []string{"sessions", "ses"},
		Short:   "Start a new session in a worktree",
		Args:    cobra.ExactArgs(2),
		RunE:    c.Run,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "start a shell-type session instead of an agent session")
	cobraCmd.Flags().BoolVar(&c.Attach, "attach", false, "attach to the session after starting")
	cobraCmd.Flags().StringVar(&c.Agent, "agent", "", "agent to use for the session")
	return cobraCmd
}

func (c *CreateSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]

	sessionType := sessionTypeFromFlag(c.Shell)

	var sessionCmd string
	var sessionEnv map[string]string

	if c.Shell {
		sessionCmd = os.Getenv("SHELL")
		if sessionCmd == "" {
			sessionCmd = "/bin/sh"
		}
		sessionEnv = nil
	} else {
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
		sessionCmd = agent.Command()
		sessionEnv = agent.Env
	}

	projCfg := cfg.Projects[project]
	h := hooks.New(projCfg.Hooks)

	wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
	path, err := wtSvc.WorktreePath(project, branch)
	if err != nil {
		if errors.Is(err, worktree.ErrWorktreeNotFound) {
			fmt.Fprintf(cmd.OutOrStdout(), "Worktree %s/%s not found, creating...  ", project, branch)
			result, createErr := wtSvc.New(project, branch)
			if createErr != nil {
				fmt.Fprintln(cmd.OutOrStdout())
				return worktreeErr(cmd, project, branch, createErr)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "done")
			path = result.Path

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
				SessionName:  semconv.SessionName(activeProfile(), project, branch),
			}, tmplAttrs); err != nil {
				return fmt.Errorf("processing templates: %w", err)
			}
		} else {
			return sessionErr(cmd, err)
		}
	}

	profile := activeProfile()
	name := semconv.SessionName(profile, project, branch)
	if sessionType == semconv.SessionTypeShell {
		name = semconv.ShellSessionName(profile, project, branch)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", name)

	var cloneDir string
	if repoPath, rpErr := config.RepoPath(projCfg.Repo); rpErr == nil {
		cloneDir = filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
	}

	tc := tmux.NewClient(tmux.NewRealRunner())
	svc := session.NewService(tc, h)
	sessionID, err := svc.Start(session.StartRequest{
		Project:  project,
		Branch:   branch,
		Path:     path,
		CloneDir: cloneDir,
		Type:     sessionType,
		Cmd:      sessionCmd,
		Env:      sessionEnv,
		Profile:  profile,
		Attach:   c.Attach,
	})
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return sessionErr(cmd, err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "done")
	if !c.Attach {
		shellSuffix := ""
		if c.Shell {
			shellSuffix = " --shell"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Attach with: ch attach session %s %s%s\n", project, branch, shellSuffix)
	}

	if c.Attach {
		return execTmuxAttach(sessionID)
	}
	return nil
}

// ── delete ───────────────────────────────────────────────────────────────────

type DeleteSessionCmd struct {
	Shell bool
	Force bool
}

func (c *DeleteSessionCmd) Cobra() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:     "session <project> <branch>",
		Aliases: []string{"sessions", "ses"},
		Short:   "Stop a session",
		Args:    cobra.ExactArgs(2),
		RunE:    c.Run,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	cobraCmd.Flags().BoolVar(&c.Force, "force", false, "skip confirmation prompt")
	return cobraCmd
}

func (c *DeleteSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	sessionType := sessionTypeFromFlag(c.Shell)
	svc := newSessionService()

	if !c.Force {
		info, err := showSessionForProfile(svc, project, branch, sessionType)
		if err != nil {
			return sessionErr(cmd, err)
		}
		if info.Status == semconv.StatusRunning {
			fmt.Fprintf(cmd.OutOrStdout(), "Delete session %s/%s (%s)? [y/N] ", project, branch, sessionType)
			scanner := bufio.NewScanner(cmd.InOrStdin())
			scanner.Scan()
			if scanner.Text() != "y" && scanner.Text() != "Y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Stopping %s/%s...  ", project, branch)
	if err := stopSessionForProfile(svc, project, branch, sessionType); err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return sessionErr(cmd, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "done")
	return nil
}

// ── attach ───────────────────────────────────────────────────────────────────

type AttachSessionCmd struct {
	Shell bool
}

func (c *AttachSessionCmd) Cobra() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:     "session <project> <branch>",
		Aliases: []string{"sessions", "ses"},
		Short:   "Attach to an existing session",
		Args:    cobra.ExactArgs(2),
		RunE:    c.Run,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	return cobraCmd
}

func (c *AttachSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	sessionType := sessionTypeFromFlag(c.Shell)
	svc := newSessionService()
	info, err := showSessionForProfile(svc, project, branch, sessionType)
	if err != nil {
		return sessionErr(cmd, err)
	}
	return execTmuxAttach(info.SessionID)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// sessionTypeFromFlag maps the --shell flag to a session type constant.
func sessionTypeFromFlag(shell bool) string {
	if shell {
		return semconv.SessionTypeShell
	}
	return semconv.SessionTypeAgent
}
