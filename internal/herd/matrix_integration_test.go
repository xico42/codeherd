//go:build integration

package herd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/tmux"
)

// TestMatrix_LaunchAndResolve fills the Launch and Resolve rows of the §10
// matrix against real tmux: a launched agent session must exist on the server
// under its (possibly profile-prefixed) canonical name, and Resolve must find
// it by the same identity Ref that created it.
func TestMatrix_LaunchAndResolve(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, _ := setupMatrixHerd(t, col.registry)

			launched, err := h.Launch(ref, LaunchOpts{})
			if err != nil {
				t.Fatalf("Launch: %v", err)
			}

			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Fatalf("tmux server has no session %q after Launch", ref.CanonicalName())
			}

			got, err := h.Resolve(ref, SessionTypeAgent)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.ID != launched.ID {
				t.Errorf("Resolve ID = %q, want %q", got.ID, launched.ID)
			}
			if got.Canonical != ref.CanonicalName() {
				t.Errorf("Resolve Canonical = %q, want %q", got.Canonical, ref.CanonicalName())
			}
		})
	}
}

// TestMatrix_StopSessions fills the StopSessions row: after launching both an
// agent and a shell session, StopSessions(All) must stop both, return two
// handles, and leave neither on the real tmux server. Under a profile this is
// the cell that was a gap — the pre-refactor code rebuilt a profile-blind name
// and missed the profile-prefixed session.
func TestMatrix_StopSessions(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, _ := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{Type: SessionTypeAgent}); err != nil {
				t.Fatalf("Launch agent: %v", err)
			}
			if _, err := h.Launch(ref, LaunchOpts{Type: SessionTypeShell}); err != nil {
				t.Fatalf("Launch shell: %v", err)
			}

			agentName := ref.CanonicalName()
			shellName := ref.CanonicalName() + "~sh" // == semconv.ShellSessionName(...)
			if !tmuxHasSession(t, socket, agentName) || !tmuxHasSession(t, socket, shellName) {
				t.Fatalf("precondition: expected both sessions running (agent=%v shell=%v)",
					tmuxHasSession(t, socket, agentName), tmuxHasSession(t, socket, shellName))
			}

			stopped, err := h.StopSessions(ref, StopOpts{All: true})
			if err != nil {
				t.Fatalf("StopSessions: %v", err)
			}
			if len(stopped) != 2 {
				t.Errorf("StopSessions stopped %d sessions, want 2", len(stopped))
			}
			if tmuxHasSession(t, socket, agentName) {
				t.Errorf("agent session %q survived StopSessions", agentName)
			}
			if tmuxHasSession(t, socket, shellName) {
				t.Errorf("shell session %q survived StopSessions", shellName)
			}
		})
	}
}

// matrixProfiles is the two columns of the §10 coverage matrix: every
// operation is exercised with profiles off and on. "off" passes a nil
// registry (profile mode disabled — what config.Load returns in the common
// case); "on" passes a registry with an active profile, so h.Ref() stamps the
// profile and every session name is prefixed (e.g. work-myapp-feat).
var matrixProfiles = []struct {
	name     string
	registry *config.ProfileRegistry
}{
	{"profile off", nil},
	{"profile on", &config.ProfileRegistry{Active: "work"}},
}

// useIsolatedTmux gives the calling test a private tmux server reached via a
// socket under t.TempDir(). It sets CODEHERD_TMUX_SOCKET so the Herd's real
// tmux runner targets the same server, clears $TMUX so new-session does not
// think it is nested, probes once, and t.Skips when tmux cannot daemonize
// (missing binary or sandboxed CI). The server is killed on cleanup so the
// socket — and any sleep processes the sessions started — disappear with the
// TempDir. Returns the socket path for direct tmux assertions.
func useIsolatedTmux(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	t.Setenv("TMUX", "")
	probe := exec.Command("tmux", "-S", socket, "new-session", "-d", "-s", "__probe__", "sleep", "30")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux daemonize unavailable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
	})
	return socket
}

// tmuxHasSession reports whether the isolated server has an exactly-named
// session. The "=" target prefix forces an exact match so an agent session
// (work-myapp-feat) is never confused with its shell (work-myapp-feat~sh).
func tmuxHasSession(t *testing.T, socket, name string) bool {
	t.Helper()
	return exec.Command("tmux", "-S", socket, "has-session", "-t", "="+name).Run() == nil
}

// setupMatrixHerd builds a Herd wired to REAL tmux and REAL git for the given
// profile column, with the myapp project cloned and a "feat" worktree created
// on disk. It returns the Herd, the identity Ref (carrying the profile when
// the registry is non-nil), and the worktree path.
func setupMatrixHerd(t *testing.T, registry *config.ProfileRegistry) (*Herd, Ref, string) {
	t.Helper()
	root := t.TempDir()

	// A tiny upstream repo with a single commit on main.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "init")

	// Clone into the codeherd layout: <projectsDir>/github.com/user/myapp.
	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "clone", remote, cloneDir)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir, Agent: "agent"},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
		Agents: map[string]config.AgentConfig{
			// A long sleep keeps the tmux session alive for the assertions.
			"agent": {Cmd: "sleep", Args: []string{"300"}},
		},
	}
	h := New(cfg, registry, Deps{Tmux: tmux.NewRealRunner(), Git: git.NewRealRunner()})

	ref := h.Ref("myapp", "feat")
	ws, err := h.EnsureWorkspace(ref, EnsureOpts{})
	if err != nil {
		t.Fatalf("EnsureWorkspace: %v", err)
	}
	return h, ref, ws.Path
}

// TestMatrix_Teardown fills the Teardown row — the row the shipped defect
// lived in. With Force, Teardown must kill the (profile-prefixed) session AND
// remove the worktree from disk. A surviving session under "profile on" is
// exactly the orphaned-agent bug the matrix exists to catch.
func TestMatrix_Teardown(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, wtPath := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{}); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Fatalf("precondition: session %q not running", ref.CanonicalName())
			}

			if err := h.Teardown(ref, TeardownOpts{Force: true}); err != nil {
				t.Fatalf("Teardown: %v", err)
			}

			if tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Errorf("session %q survived Teardown (orphaned agent)", ref.CanonicalName())
			}
			if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
				t.Errorf("worktree %q still on disk after Teardown (stat err=%v)", wtPath, err)
			}
		})
	}
}

// TestMatrix_TeardownRefusesRunning is the non-force half: Teardown without
// Force must refuse with ErrSessionRunning while a session is live, and must
// leave both the session and the worktree intact.
func TestMatrix_TeardownRefusesRunning(t *testing.T) {
	for _, col := range matrixProfiles {
		t.Run(col.name, func(t *testing.T) {
			socket := useIsolatedTmux(t)
			h, ref, wtPath := setupMatrixHerd(t, col.registry)

			if _, err := h.Launch(ref, LaunchOpts{}); err != nil {
				t.Fatalf("Launch: %v", err)
			}

			err := h.Teardown(ref, TeardownOpts{Force: false})
			if !errors.Is(err, ErrSessionRunning) {
				t.Fatalf("Teardown(Force:false) err = %v, want ErrSessionRunning", err)
			}
			if !tmuxHasSession(t, socket, ref.CanonicalName()) {
				t.Errorf("session %q was killed despite refusal", ref.CanonicalName())
			}
			if _, err := os.Stat(wtPath); err != nil {
				t.Errorf("worktree %q removed despite refusal: %v", wtPath, err)
			}
		})
	}
}
