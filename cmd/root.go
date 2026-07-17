package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

var (
	cfgFile     string
	noTmux      bool
	profileFlag string
	cfg         *config.Config
	registry    *config.ProfileRegistry
	// h is the domain. It is the only service any command constructs, and
	// it is constructed exactly once, here.
	h *herd.Herd
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
		cfg, registry, err = config.Load(cfgFile, resolveProfileArg(profileFlag))
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		h = herd.New(cfg, registry, herd.Deps{
			Tmux: tmux.NewRealRunner(),
			Git:  git.NewRealRunner(),
		})
		return nil
	},
}

func init() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/codeherd/config.toml)")
	rootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "profile to load; overrides $CODEHERD_PROFILE and defaults.main_profile (requires profiles_enabled=true)")
	_ = rootCmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	rootCmd.Flags().BoolVar(&noTmux, "no-tmux", false, "run TUI directly without tmux wrapping")

	registerCommands(rootCmd)
}

// resolveProfileArg returns the profile name to hand to config.Load:
// the --profile flag when set, otherwise the CODEHERD_PROFILE env var.
// config.Load then falls back to defaults.main_profile, yielding the
// precedence main_profile < CODEHERD_PROFILE < --profile.
func resolveProfileArg(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv(semconv.EnvProfile)
}

// Execute runs the root command and returns any error.
// version is the build-time version string surfaced via `ch --version` / `ch version`.
func Execute(version string) error {
	resetAllFlags(rootCmd)
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
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
