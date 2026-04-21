package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/xico42/codeherd/internal/config"
)

var (
	cfgFile string
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
		return nil
	},
}

func init() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/codeherd/config.toml)")
	rootCmd.Flags().BoolVar(&noTmux, "no-tmux", false, "run TUI directly without tmux wrapping")

	registerCommands(rootCmd)
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
