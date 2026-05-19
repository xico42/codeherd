//go:build integration

package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/tmux"
)

// useIsolatedTmux gives the calling test a private tmux server: tmux is
// invoked with `-S <path>` pointing at a socket under the test's TempDir,
// so the server is reachable only via tmuxCmd(t, ...) and is torn down at
// the end of the test. Returns early via t.Skip when tmux is missing or
// when the server cannot daemonize in the current environment (sandboxed
// PID namespace etc.). Returns the socket path so subsequent tmuxCmd calls
// in the test target the same server.
//
// Why: codeherd's integration tests previously created sessions on the
// default tmux socket. That made them dependent on the developer's outer
// tmux server (leaks sessions, collides with other tests, fails in
// sandboxes that isolate /tmp). This helper makes every test stand on its
// own.
func useIsolatedTmux(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv(tmux.SocketEnvVar, socket)
	// Inside another tmux session, `new-session` errors out. Tests rarely
	// need to know we cleared this; only matters when CI runs `go test`
	// from inside a tmux pane.
	t.Setenv("TMUX", "")

	probe := exec.Command("tmux", "-S", socket, "new-session", "-d", "-s", "__probe__", "sleep", "30")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux daemonize unavailable: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	// Whether or not the rest of the test succeeds, kill the server so the
	// socket file disappears with the TempDir.
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
	})
	return socket
}

// tmuxCmd builds an exec.Cmd that targets the test's isolated tmux server.
// Tests should use this instead of `exec.Command("tmux", ...)` so that
// inspection commands (ls, kill-session, has-session) reach the same server
// the code-under-test wrote into.
func tmuxCmd(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	socket := socketFromEnv(t)
	full := append([]string{"-S", socket}, args...)
	return exec.Command("tmux", full...)
}

func socketFromEnv(t *testing.T) string {
	t.Helper()
	socket := os.Getenv(tmux.SocketEnvVar)
	if socket == "" {
		t.Fatalf("tmuxCmd called without useIsolatedTmux; %s is unset", tmux.SocketEnvVar)
	}
	return socket
}
