package tui

import (
	"errors"
	"testing"

	"github.com/xico42/codeherd/internal/herd"
)

func TestHumanize_nil(t *testing.T) {
	if got := humanize(nil); got != "" {
		t.Fatalf("humanize(nil) = %q, want empty", got)
	}
}

func TestHumanize_sentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"notCloned", herd.ErrNotCloned, "Project is not cloned — clone it first."},
		{"worktreeExists", herd.ErrWorktreeExists, "Worktree already exists."},
		{"worktreeNotFound", herd.ErrWorktreeNotFound, "Worktree not found."},
		{"sessionNotFound", herd.ErrSessionNotFound, "No such session."},
		{"sessionRunning", herd.ErrSessionRunning, "Session is running — stop it first."},
		{"pathNotFound", herd.ErrPathNotFound, "Worktree path not found."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanize(tc.err); got != tc.want {
				t.Fatalf("humanize(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestHumanize_sessionExists_usesRef(t *testing.T) {
	se := &herd.SessionExistsError{
		Ref:  herd.Ref{Project: "myapp", Branch: "feat"},
		Type: herd.SessionTypeAgent,
	}
	want := "Session myapp/feat (agent) already exists."
	if got := humanize(se); got != want {
		t.Fatalf("humanize() = %q, want %q", got, want)
	}
}

func TestHumanize_unknown_passesThrough(t *testing.T) {
	raw := errors.New("some raw failure")
	if got := humanize(raw); got != "some raw failure" {
		t.Fatalf("humanize() = %q, want the raw message", got)
	}
}
