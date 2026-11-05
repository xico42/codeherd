package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/tmux"
)

// Git runs git in dir with a fixed test identity and returns its stdout,
// failing the test on error.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@test.com"}, args...)
	out, stderr, err := git.Run(dir, full...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return string(out)
}

// InitRepo creates a repo at dir on branch main with one empty commit. The
// branch is pinned because init.defaultBranch differs between hosts (CI
// runners default to master), and the identity is stored in the repo so later
// commits made by code under test work too.
func InitRepo(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-b", "main")
	Git(t, dir, "config", "user.name", "Test")
	Git(t, dir, "config", "user.email", "test@test.com")
	Git(t, dir, "commit", "--allow-empty", "-m", "init")
}

// IsolateTmux gives the test a private tmux server and returns its socket.
//
// It sets tmux.SocketEnvVar so every tmux.RealRunner in the process — the code
// under test and Tmux below — targets a socket under t.TempDir(), clears $TMUX
// so new-session does not think it is nested, and kills the server on cleanup.
// Without this, tests would leak sessions into the developer's tmux server and
// collide with each other. It skips the test when tmux is missing or cannot
// daemonize (sandboxed CI).
func IsolateTmux(t testing.TB) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	t.Setenv("TMUX", "")

	r := tmux.NewRealRunner()
	if _, stderr, code, err := r.Run("new-session", "-d", "-s", "__probe__", "sleep", "30"); err != nil || code != 0 {
		t.Skipf("tmux daemonize unavailable: exit %d, %v\n%s", code, err, strings.TrimSpace(stderr))
	}
	t.Cleanup(func() {
		// The exit code is irrelevant: the server may already be gone.
		if _, _, _, err := r.Run("kill-server"); err != nil {
			t.Errorf("killing tmux server: %v", err)
		}
	})
	return socket
}

// Tmux runs a tmux command against the server IsolateTmux set up and returns
// its stdout and whether it exited zero.
func Tmux(t testing.TB, args ...string) (string, bool) {
	t.Helper()
	stdout, _, code, err := tmux.NewRealRunner().Run(args...)
	if err != nil {
		t.Fatalf("tmux %v: %v", args, err)
	}
	return stdout, code == 0
}
