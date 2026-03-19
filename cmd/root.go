package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/tui"
	"github.com/xico42/codeherd/internal/worktree"
)

var (
	cfgFile string
	token   string
	noColor bool
	noTmux  bool
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "ch",
	Short: "Manage parallel agentic coding sessions",
	Long: `codeherd manages parallel agentic coding sessions across projects and git worktrees.

It organizes projects, creates isolated worktrees, configures per-agent environments
with deterministic port allocation, and orchestrates tmux sessions where AI coding
agents (Claude Code, Aider, Codex, or any CLI tool) run independently.

It is like a shepherd, but for coding agents :).
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(cmd)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.ApplyEnv()
		cfg.ApplyFlags(token)
		return nil
	},
}

func init() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.AddGroup(
		&cobra.Group{ID: "sessions", Title: "Session Management:"},
		&cobra.Group{ID: "projects", Title: "Project & Worktree Management:"},
		&cobra.Group{ID: "config", Title: "Configuration:"},
		&cobra.Group{ID: "remote", Title: "Remote Execution (planned):"},
	)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/codeherd/config.toml)")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "Digital Ocean API token (overrides config and DIGITALOCEAN_TOKEN)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.Flags().BoolVar(&noTmux, "no-tmux", false, "run TUI directly without tmux wrapping")
}

// Execute runs the root command and returns any error.
func Execute() error {
	resetAllFlags(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return fmt.Errorf("%w", err)
	}
	return nil
}

// resetAllFlags resets the Changed state of all flags in the command tree so
// that Execute() can be called multiple times (e.g. in tests) without flags
// from a previous call leaking into the next.
func resetAllFlags(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	}
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
	for _, sub := range cmd.Commands() {
		resetAllFlags(sub)
	}
}

func runTUI(cmd *cobra.Command) error {
	tmuxRunner := tmux.NewRealRunner()
	tmuxClient := tmux.NewClient(tmuxRunner)

	if noTmux {
		return runTUIDirect(tmuxClient)
	}
	return runTUIInTmux(tmuxClient)
}

func runTUIInTmux(tmuxClient *tmux.Client) error {
	sessionName := semconv.CodeherdSessionName
	inTmux := os.Getenv("TMUX") != ""

	if inTmux {
		currentSession, err := tmuxClient.CurrentSession()
		if err != nil {
			return fmt.Errorf("detecting tmux session: %w", err)
		}

		// Already in the codeherd session — select TUI window,
		// respawning the pane first if it is dead.
		if currentSession == sessionName {
			if err := respawnIfDead(tmuxClient, sessionName); err != nil {
				return err
			}
			if err := tmuxClient.SelectWindow(sessionName + ":0"); err != nil {
				return fmt.Errorf("selecting window: %w", err)
			}
			return nil
		}
	}

	// Ensure codeherd session exists.
	exists, err := tmuxClient.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		if err := createCodeherdSession(tmuxClient, sessionName); err != nil {
			return err
		}
	} else {
		// Session exists — respawn the TUI if the pane is dead.
		if err := respawnIfDead(tmuxClient, sessionName); err != nil {
			return err
		}
	}

	// Inside tmux, different session — switch client.
	if inTmux {
		if err := tmuxClient.SwitchClient(sessionName); err != nil {
			return fmt.Errorf("switching client: %w", err)
		}
		return nil
	}

	// Not inside tmux — exec into tmux attach.
	return execTmuxAttach(sessionName)
}

// createCodeherdSession creates the codeherd tmux session running the TUI.
func createCodeherdSession(tmuxClient *tmux.Client, sessionName string) error {
	chBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding ch binary: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/"
	}
	if err := tmuxClient.NewSessionWithCmd(sessionName, homeDir, chBin+" --no-tmux"); err != nil {
		return fmt.Errorf("creating codeherd session: %w", err)
	}
	// Keep the pane alive if ch --no-tmux exits with an error,
	// so the user can see what went wrong.
	_ = tmuxClient.SetOption(sessionName, "remain-on-exit", "on")
	return nil
}

// respawnIfDead checks whether the TUI pane (window 0) is dead and respawns
// the ch --no-tmux process if so. This recovers from the case where the TUI
// exited (intentionally or by accident) and remain-on-exit kept the dead pane.
func respawnIfDead(tmuxClient *tmux.Client, sessionName string) error {
	dead, err := tmuxClient.IsPaneDead(sessionName)
	if err != nil {
		return fmt.Errorf("checking pane status: %w", err)
	}
	if !dead {
		return nil
	}
	chBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding ch binary: %w", err)
	}
	if err := tmuxClient.RespawnPane(sessionName+":0", chBin+" --no-tmux"); err != nil {
		return fmt.Errorf("respawning TUI: %w", err)
	}
	return nil
}

func runTUIDirect(tmuxClient *tmux.Client) error {
	wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, &hooks.NoOp{})
	sesSvc := newSessionService()
	projSvc := project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})

	insideTmux := os.Getenv("TMUX") != ""
	m := tui.NewModel(cfg, wtSvc, sesSvc, projSvc, tmuxClient, insideTmux)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	// Only used when --no-tmux and not inside tmux.
	if fm, ok := finalModel.(tui.Model); ok && fm.PendingAttach != "" {
		return execTmuxAttach(fm.PendingAttach)
	}

	// Normal quit — if running inside the codeherd tmux session, kill it
	// so the user isn't left with a dead pane.
	if insideTmux {
		if cur, _ := tmuxClient.CurrentSession(); cur == semconv.CodeherdSessionName {
			_ = tmuxClient.KillSession(semconv.CodeherdSessionName)
		}
	}

	return nil
}
