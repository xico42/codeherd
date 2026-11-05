package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/tmux"
)

func TestInitRepoAndGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	InitRepo(t, dir)

	if got := strings.TrimSpace(Git(t, dir, "branch", "--show-current")); got != "main" {
		t.Errorf("current branch = %q, want main", got)
	}
	if got := strings.TrimSpace(Git(t, dir, "rev-list", "--count", "HEAD")); got != "1" {
		t.Errorf("commit count = %q, want 1", got)
	}
}

func TestGit_failureFailsTheTest(t *testing.T) {
	r := &recorder{TB: t}
	defer func() {
		if p := recover(); p != r {
			t.Fatalf("expected Git to fail the test, recovered %v", p)
		}
	}()
	Git(r, t.TempDir(), "rev-parse", "HEAD") // not a repo
}

func TestIsolateTmuxAndTmux(t *testing.T) {
	socket := IsolateTmux(t)

	if got := os.Getenv(tmux.SocketEnvVar); got != socket {
		t.Errorf("%s = %q, want %q", tmux.SocketEnvVar, got, socket)
	}
	if _, ok := Tmux(t, "has-session", "-t", "=__probe__"); !ok {
		t.Error("probe session missing on the isolated server")
	}
	if _, ok := Tmux(t, "has-session", "-t", "=no-such-session"); ok {
		t.Error("has-session for a missing session reported success")
	}
}
