package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/tui"
	"github.com/xico42/codeherd/internal/worktree"
)

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
