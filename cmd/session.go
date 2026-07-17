package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

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
	sessions, err := h.Sessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SESSION\tTYPE\tSTATUS")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Ref.CanonicalName(), s.Type, s.Status)
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
		Use:               "session <project> <branch>",
		Aliases:           []string{"sessions", "ses"},
		Short:             "Show details for a session",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	return cobraCmd
}

func (c *ShowSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	info, err := h.Resolve(h.Ref(project, branch), sessionTypeFromFlag(c.Shell))
	if err != nil {
		return herdErr(project, branch, err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Session:\t%s\n", info.Ref.CanonicalName())
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
		Use:               "session <project> <branch>",
		Aliases:           []string{"sessions", "ses"},
		Short:             "Start a new session in a worktree",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "start a shell-type session instead of an agent session")
	cobraCmd.Flags().BoolVar(&c.Attach, "attach", false, "attach to the session after starting")
	cobraCmd.Flags().StringVar(&c.Agent, "agent", "", "agent to use for the session")
	_ = cobraCmd.RegisterFlagCompletionFunc("agent", completeAgents)
	return cobraCmd
}

func (c *CreateSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]

	sessionType := sessionTypeFromFlag(c.Shell)

	flagAgent := ""
	if cmd.Flags().Changed("agent") {
		flagAgent = c.Agent
	}

	ref := h.Ref(project, branch)
	// Ensure the worktree exists, tolerating the common case where it already
	// does. One call replaces the old probe-then-create dance.
	if _, err := h.EnsureWorkspace(ref, herd.EnsureOpts{Provision: true}); err != nil {
		if !errors.Is(err, herd.ErrWorktreeExists) {
			return herdErr(project, branch, err)
		}
		// Already there — that is the common case for `create session`.
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Worktree %s/%s not found, creating...  done\n", project, branch)
	}

	name := ref.CanonicalName()
	if sessionType == herd.SessionTypeShell {
		name = semconv.ShellSessionName(ref.Profile, project, branch)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Starting session %s...  ", name)

	handle, err := h.Launch(ref, herd.LaunchOpts{
		Type:   sessionType,
		Agent:  flagAgent, // "" means defaults.agent — resolved inside Launch
		Attach: c.Attach,
	})
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return herdErr(project, branch, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "done")
	if !c.Attach {
		shellSuffix := ""
		if c.Shell {
			shellSuffix = " --shell"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Attach with: ch attach session %s %s%s\n", project, branch, shellSuffix)
		return nil
	}
	return execTmuxAttach(handle.ID)
}

// ── delete ───────────────────────────────────────────────────────────────────

type DeleteSessionCmd struct {
	Shell bool
	Force bool
}

func (c *DeleteSessionCmd) Cobra() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:               "session <project> <branch>",
		Aliases:           []string{"sessions", "ses"},
		Short:             "Stop a session",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	cobraCmd.Flags().BoolVar(&c.Force, "force", false, "skip confirmation prompt")
	return cobraCmd
}

func (c *DeleteSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	sessionType := sessionTypeFromFlag(c.Shell)

	if !c.Force {
		info, err := h.Resolve(h.Ref(project, branch), sessionType)
		if err != nil {
			return herdErr(project, branch, err)
		}
		if info.Status == herd.StatusRunning {
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
	if _, err := h.StopSessions(h.Ref(project, branch), herd.StopOpts{Type: sessionType}); err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return herdErr(project, branch, err)
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
		Use:               "session <project> <branch>",
		Aliases:           []string{"sessions", "ses"},
		Short:             "Attach to an existing session",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
	cobraCmd.Flags().BoolVar(&c.Shell, "shell", false, "target the shell-type session")
	return cobraCmd
}

func (c *AttachSessionCmd) Run(cmd *cobra.Command, args []string) error {
	project, branch := args[0], args[1]
	info, err := h.Resolve(h.Ref(project, branch), sessionTypeFromFlag(c.Shell))
	if err != nil {
		return herdErr(project, branch, err)
	}
	return execTmuxAttach(info.ID)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// sessionTypeFromFlag maps the --shell flag to a herd session type.
func sessionTypeFromFlag(shell bool) herd.SessionType {
	if shell {
		return herd.SessionTypeShell
	}
	return herd.SessionTypeAgent
}
