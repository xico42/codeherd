package tui

import (
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

// recordingRunner serves a fixed list-sessions table and records kill-session targets.
type recordingRunner struct {
	sessions string
	killed   []string
}

func (r *recordingRunner) Run(args ...string) (string, string, int, error) {
	switch args[0] {
	case "list-sessions":
		return r.sessions, "", 0, nil
	case "kill-session":
		// args: kill-session -t <target>
		r.killed = append(r.killed, args[2])
		return "", "", 0, nil
	}
	return "", "", 0, nil
}

// sessionRow builds one tab-separated list-sessions record.
func sessionRow(id, name, canonical, sessType, profile, branch string) string {
	return strings.Join([]string{id, name, canonical, sessType, "running", "", "", profile, branch}, "\t")
}

// A worktree whose HEAD has diverged displays the checked-out branch, which is
// not the identity branch its session was named after. Teardown must not depend
// on that display value. No profile is involved here.
func TestConfirmDeleteAll_divergedHeadSessionIsKilled(t *testing.T) {
	runner := &recordingRunner{
		sessions: sessionRow("$1", "myapp-feat", "myapp-feat", "agent", "", "feat"),
	}
	client := tmux.NewClient(runner)
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{}}

	m := Model{
		sesSvc:     session.NewService(client, &hooks.NoOp{}),
		wtSvc:      worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), client, &hooks.NoOp{}),
		tmuxClient: client,
		confirm: newConfirmModel(Item{
			Project:        "myapp",
			Branch:         "other", // displayed branch after divergence
			AgentSessionID: "$1",
			HasAgent:       true,
			HeadHint:       "on other",
		}),
	}

	_, cmd := m.confirmDeleteAll()
	cmd()

	if len(runner.killed) != 1 || runner.killed[0] != "$1" {
		t.Errorf("agent session $1 not killed; killed=%v", runner.killed)
	}
}

// deleteAll on a profile-scoped worktree must kill both tmux sessions. The
// worktree is force-deleted regardless, so a missed kill leaves the agent
// process running against a directory that no longer exists.
func TestConfirmDeleteAll_profileScopedSessionsAreKilled(t *testing.T) {
	runner := &recordingRunner{
		sessions: strings.Join([]string{
			sessionRow("$1", "work-myapp-feat", "work-myapp-feat", "agent", "work", "feat"),
			sessionRow("$2", "work-myapp-feat~sh", "work-myapp-feat", "shell", "work", "feat"),
		}, "\n"),
	}
	client := tmux.NewClient(runner)
	// No projects configured, so worktree.Delete returns an error instead of
	// touching the filesystem — teardown of the tmux sessions must still happen.
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{}}

	m := Model{
		sesSvc:     session.NewService(client, &hooks.NoOp{}),
		wtSvc:      worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), client, &hooks.NoOp{}),
		tmuxClient: client,
		confirm: newConfirmModel(Item{
			Project:        "myapp",
			Branch:         "feat",
			AgentSessionID: "$1",
			ShellSessionID: "$2",
			HasAgent:       true,
			HasShell:       true,
		}),
	}

	_, cmd := m.confirmDeleteAll()
	if cmd == nil {
		t.Fatal("confirmDeleteAll returned no command")
	}
	cmd() // executes the teardown closure; worktree.Delete fails harmlessly (no repo)

	for _, want := range []string{"$1", "$2"} {
		found := false
		for _, got := range runner.killed {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("session %s was never killed; killed=%v (dangling tmux session + processes)", want, runner.killed)
		}
	}
}
