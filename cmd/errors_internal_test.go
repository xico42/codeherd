package cmd

import (
	"errors"
	"testing"

	"github.com/xico42/codeherd/internal/herd"
)

func TestHerdErr_notCloned(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrNotCloned)
	want := "myapp is not cloned. Run 'ch clone project myapp' first"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_worktreeExists(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrWorktreeExists)
	want := "worktree myapp/feat already exists"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_worktreeNotFound(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrWorktreeNotFound)
	want := "worktree myapp/feat not found. Run 'ch create worktree myapp feat' first"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_sessionRunning(t *testing.T) {
	got := herdErr("myapp", "feat", herd.ErrSessionRunning)
	want := "session myapp-feat is running. Stop it first or use --force"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_sessionExists_carriesRefFromTypedError(t *testing.T) {
	se := &herd.SessionExistsError{
		Ref:  herd.Ref{Project: "myapp", Branch: "feat"},
		Type: herd.SessionTypeAgent,
	}
	got := herdErr("ignored", "ignored", se)
	want := "session myapp/feat (agent) already exists. Attach with 'ch attach session myapp feat'"
	if got == nil || got.Error() != want {
		t.Fatalf("herdErr() = %v, want %q", got, want)
	}
}

func TestHerdErr_unknownSentinel_passesThrough(t *testing.T) {
	raw := errors.New("boom")
	got := herdErr("myapp", "feat", raw)
	if got != raw {
		t.Fatalf("herdErr() = %v, want the original error %v", got, raw)
	}
}
