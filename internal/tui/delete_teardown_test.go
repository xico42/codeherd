package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/tmux"
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

// sessionRowLine builds one tab-separated list-sessions record, in the field
// order tmux.Client.ListSessions parses (…, profile, branch, project).
func sessionRowLine(id, name, canonical, sessType, profile, branch, project string) string {
	return strings.Join([]string{id, name, canonical, sessType, "running", "", "", profile, branch, project}, "\t")
}

// teardownHerd builds a Herd wired to the given tmux runner, with the project's
// worktree materialized on disk so Teardown's stat check passes.
func teardownHerd(t *testing.T, runner tmux.Runner, registry *config.ProfileRegistry, branch string) *herd.Herd {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	wt := filepath.Join(dir, "github.com", "user", "myapp__worktrees", branch)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	return herd.New(cfg, registry, herd.Deps{Tmux: runner, Git: git.NewRealRunner()})
}

// A worktree whose HEAD has diverged displays the checked-out branch, which is
// not the identity branch its session was named after. Teardown must address
// the session by the identity Ref, never the display value. No profile here.
func TestConfirmDeleteAll_divergedHeadSessionIsKilled(t *testing.T) {
	runner := &recordingRunner{
		sessions: sessionRowLine("$1", "myapp-feat", "myapp-feat", "agent", "", "feat", "myapp"),
	}
	hrd := teardownHerd(t, runner, nil, "feat")

	m := Model{
		herd:       hrd,
		tmuxClient: tmux.NewClient(runner),
		confirm: newConfirmModel(Item{
			// Identity survives the divergence; display shows the other branch.
			Ref:            herd.Ref{Project: "myapp", Branch: "feat"},
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
			sessionRowLine("$1", "work-myapp-feat", "work-myapp-feat", "agent", "work", "feat", "myapp"),
			sessionRowLine("$2", "work-myapp-feat~sh", "work-myapp-feat", "shell", "work", "feat", "myapp"),
		}, "\n"),
	}
	reg := &config.ProfileRegistry{Active: "work"}
	hrd := teardownHerd(t, runner, reg, "feat")

	m := Model{
		herd:       hrd,
		tmuxClient: tmux.NewClient(runner),
		confirm: newConfirmModel(Item{
			Ref:            herd.Ref{Profile: "work", Project: "myapp", Branch: "feat"},
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
	cmd() // executes the teardown closure

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
