package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

func newSessionService() *session.Service {
	tc := tmux.NewClient(tmux.NewRealRunner())
	return session.NewService(tc)
}

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

var sessionCmd = &cobra.Command{
	Use:     "session",
	Short:   "Manage agent sessions",
	GroupID: "sessions",
}

// ── start ────────────────────────────────────────────────────────────────────

var sessionStartAttach bool
var sessionStartNoCreate bool
var sessionStartAgent string

var sessionStartCmd = &cobra.Command{
	Use:   "start <project> <branch>",
	Short: "Start a new agent session in a worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]

		flagAgent := ""
		if cmd.Flags().Changed("agent") {
			flagAgent = sessionStartAgent
		}
		agentName, err := resolveAgentName(flagAgent)
		if err != nil {
			return err
		}
		agent, err := cfg.AgentByName(agentName)
		if err != nil {
			return fmt.Errorf("resolving agent: %w", err)
		}

		wtSvc := newWorktreeService()
		path, err := wtSvc.WorktreePath(project, branch)
		if err != nil {
			if errors.Is(err, worktree.ErrWorktreeNotFound) && !sessionStartNoCreate {
				fmt.Fprintf(cmd.OutOrStdout(), "Worktree %s/%s not found, creating...  ", project, branch)
				result, createErr := wtSvc.New(project, branch)
				if createErr != nil {
					fmt.Fprintln(cmd.OutOrStdout())
					return worktreeErr(cmd, project, branch, createErr)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "done")
				path = result.Path
			} else {
				return sessionErr(cmd, err)
			}
		}

		name := semconv.SessionName(project, branch)
		fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", name)

		svc := newSessionService()
		sessionID, err := svc.Start(session.StartRequest{
			Project: project,
			Branch:  branch,
			Path:    path,
			Cmd:     agent.Command(),
			Env:     agent.Env,
			Attach:  sessionStartAttach,
		})
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return sessionErr(cmd, err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		if !sessionStartAttach {
			fmt.Fprintf(cmd.OutOrStdout(), "Attach with: ch session attach %s\n", name)
		}

		if sessionStartAttach {
			return execTmuxAttach(sessionID)
		}
		return nil
	},
}

// ── list ─────────────────────────────────────────────────────────────────────

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active sessions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := newSessionService()
		sessions, err := svc.List()
		if err != nil {
			return fmt.Errorf("listing sessions: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "SESSION\tSTATUS")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\n", s.Name, s.Status)
		}
		return w.Flush()
	},
}

// ── show ─────────────────────────────────────────────────────────────────────

var sessionShowCmd = &cobra.Command{
	Use:   "show <session>",
	Short: "Show details for a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := newSessionService()
		info, err := svc.Show(args[0])
		if err != nil {
			return sessionErr(cmd, err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Session:\t%s\n", info.Name)
		fmt.Fprintf(w, "Status:\t%s\n", info.Status)
		if info.Annotation != "" {
			fmt.Fprintf(w, "Annotation:\t%s\n", info.Annotation)
		}
		if !info.StartedAt.IsZero() {
			fmt.Fprintf(w, "Started:\t%s\n", info.StartedAt.Format("2006-01-02T15:04:05Z"))
		}
		return w.Flush()
	},
}

// ── attach ───────────────────────────────────────────────────────────────────

var sessionAttachCmd = &cobra.Command{
	Use:   "attach <session>",
	Short: "Attach to an existing session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := newSessionService()
		info, err := svc.Show(args[0]) // verify it exists
		if err != nil {
			return sessionErr(cmd, err)
		}
		return execTmuxAttach(info.SessionID) // use stable session_id for attach
	},
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

// ── stop ─────────────────────────────────────────────────────────────────────

var sessionStopForce bool

var sessionStopCmd = &cobra.Command{
	Use:   "stop <session>",
	Short: "Stop a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		svc := newSessionService()

		if !sessionStopForce {
			info, err := svc.Show(name)
			if err != nil {
				return sessionErr(cmd, err)
			}
			if info.Status == semconv.StatusRunning {
				fmt.Fprintf(cmd.OutOrStdout(), "Session %s is running. Stop? [y/N] ", name)
				scanner := bufio.NewScanner(cmd.InOrStdin())
				scanner.Scan()
				if scanner.Text() != "y" && scanner.Text() != "Y" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Stopping %s...  ", name)
		if err := svc.Stop(name); err != nil {
			fmt.Fprintln(cmd.OutOrStdout())
			return sessionErr(cmd, err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "done")
		return nil
	},
}

// ── error helper ─────────────────────────────────────────────────────────────

func sessionErr(cmd *cobra.Command, err error) error {
	switch {
	case errors.Is(err, session.ErrSessionExists):
		var sesErr *session.SessionExistsError
		if errors.As(err, &sesErr) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: session %s already exists. Attach with 'ch session attach %s'.\n", sesErr.Name, sesErr.Name)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
		}
	case errors.Is(err, session.ErrSessionNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, session.ErrPathNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, worktree.ErrNotCloned):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	case errors.Is(err, worktree.ErrWorktreeNotFound):
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)
	default:
		return err
	}
	os.Exit(1)
	return nil
}

func init() {
	sessionStartCmd.Flags().BoolVar(&sessionStartAttach, "attach", false, "attach to the session after starting")
	sessionStartCmd.Flags().BoolVar(&sessionStartNoCreate, "no-create", false, "fail if worktree does not exist instead of creating it")
	sessionStartCmd.Flags().StringVar(&sessionStartAgent, "agent", "", "agent to use for the session")
	sessionStopCmd.Flags().BoolVar(&sessionStopForce, "force", false, "skip confirmation prompt")

	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionShowCmd)
	sessionCmd.AddCommand(sessionAttachCmd)
	sessionCmd.AddCommand(sessionStopCmd)
	rootCmd.AddCommand(sessionCmd)
}
