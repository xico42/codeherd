package herd

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/hooks"
)

// fakeGit satisfies the whole git.Runner union. Every method is a func field
// defaulting to success, so a test overrides only what it cares about:
//
//	g := &fakeGit{}
//	g.AddFn = func(_, _, _ string) error { return errors.New("boom") }
type fakeGit struct {
	mu    sync.Mutex
	Calls []string // "<method> <arg> <arg>…", in order

	AddFn                func(cloneDir, worktreePath, branch string) error
	AddNewBranchFn       func(cloneDir, worktreePath, branch string) error
	AddNewBranchFromFn   func(cloneDir, worktreePath, branch, startPoint string) error
	RemoveFn             func(cloneDir, worktreePath string) error
	ListFn               func(cloneDir string) ([]git.WorktreeInfo, error)
	FetchFn              func(cloneDir, remote, branch string) error
	FetchAllFn           func(cloneDir string) error
	FastForwardFn        func(cloneDir, remote, branch string) error
	RemotesFn            func(cloneDir string) ([]string, error)
	ListRemoteBranchesFn func(cloneDir string) ([]git.RemoteBranch, error)
	AddTrackingFn        func(cloneDir, worktreePath, branch, remoteRef string) error
	HasLocalBranchFn     func(cloneDir, branch string) (bool, error)
	CloneFn              func(repo, path, branch string) error
}

func (g *fakeGit) record(parts ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Calls = append(g.Calls, strings.Join(parts, " "))
}

// called reports whether any recorded call contains all the given substrings.
func (g *fakeGit) called(want ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.Calls {
		ok := true
		for _, w := range want {
			if !strings.Contains(c, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (g *fakeGit) Add(cloneDir, worktreePath, branch string) error {
	g.record("Add", cloneDir, worktreePath, branch)
	if g.AddFn != nil {
		return g.AddFn(cloneDir, worktreePath, branch)
	}
	return nil
}

func (g *fakeGit) AddNewBranch(cloneDir, worktreePath, branch string) error {
	g.record("AddNewBranch", cloneDir, worktreePath, branch)
	if g.AddNewBranchFn != nil {
		return g.AddNewBranchFn(cloneDir, worktreePath, branch)
	}
	return nil
}

func (g *fakeGit) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	g.record("AddNewBranchFrom", cloneDir, worktreePath, branch, startPoint)
	if g.AddNewBranchFromFn != nil {
		return g.AddNewBranchFromFn(cloneDir, worktreePath, branch, startPoint)
	}
	return nil
}

func (g *fakeGit) Remove(cloneDir, worktreePath string) error {
	g.record("Remove", cloneDir, worktreePath)
	if g.RemoveFn != nil {
		return g.RemoveFn(cloneDir, worktreePath)
	}
	return nil
}

func (g *fakeGit) List(cloneDir string) ([]git.WorktreeInfo, error) {
	g.record("List", cloneDir)
	if g.ListFn != nil {
		return g.ListFn(cloneDir)
	}
	return nil, nil
}

func (g *fakeGit) Fetch(cloneDir, remote, branch string) error {
	g.record("Fetch", cloneDir, remote, branch)
	if g.FetchFn != nil {
		return g.FetchFn(cloneDir, remote, branch)
	}
	return nil
}

func (g *fakeGit) FetchAll(cloneDir string) error {
	g.record("FetchAll", cloneDir)
	if g.FetchAllFn != nil {
		return g.FetchAllFn(cloneDir)
	}
	return nil
}

func (g *fakeGit) FastForward(cloneDir, remote, branch string) error {
	g.record("FastForward", cloneDir, remote, branch)
	if g.FastForwardFn != nil {
		return g.FastForwardFn(cloneDir, remote, branch)
	}
	return nil
}

func (g *fakeGit) Remotes(cloneDir string) ([]string, error) {
	g.record("Remotes", cloneDir)
	if g.RemotesFn != nil {
		return g.RemotesFn(cloneDir)
	}
	return []string{"origin"}, nil
}

func (g *fakeGit) ListRemoteBranches(cloneDir string) ([]git.RemoteBranch, error) {
	g.record("ListRemoteBranches", cloneDir)
	if g.ListRemoteBranchesFn != nil {
		return g.ListRemoteBranchesFn(cloneDir)
	}
	return nil, nil
}

func (g *fakeGit) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	g.record("AddTracking", cloneDir, worktreePath, branch, remoteRef)
	if g.AddTrackingFn != nil {
		return g.AddTrackingFn(cloneDir, worktreePath, branch, remoteRef)
	}
	return nil
}

func (g *fakeGit) HasLocalBranch(cloneDir, branch string) (bool, error) {
	g.record("HasLocalBranch", cloneDir, branch)
	if g.HasLocalBranchFn != nil {
		return g.HasLocalBranchFn(cloneDir, branch)
	}
	return false, nil
}

func (g *fakeGit) Clone(repo, path, branch string) error {
	g.record("Clone", repo, path, branch)
	if g.CloneFn != nil {
		return g.CloneFn(repo, path, branch)
	}
	return nil
}

// fakeTmux satisfies tmux.Runner. Sessions is the raw list-sessions table it
// serves; Calls records every invocation.
type fakeTmux struct {
	mu       sync.Mutex
	Sessions []sessionRow
	Calls    [][]string
	RunFn    func(args ...string) (string, string, int, error) // overrides everything
}

// sessionRow is one record in the fake's list-sessions table, in the field
// order tmux.Client.ListSessions parses.
type sessionRow struct {
	ID, Name, Canonical, Type, Status, Annotation, StartedAt, Profile, Branch, Project string
}

func (r sessionRow) format() string {
	return strings.Join([]string{
		r.ID, r.Name, r.Canonical, r.Type, r.Status,
		r.Annotation, r.StartedAt, r.Profile, r.Branch, r.Project,
	}, "\t")
}

func (f *fakeTmux) Run(args ...string) (string, string, int, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, args)
	f.mu.Unlock()

	if f.RunFn != nil {
		return f.RunFn(args...)
	}
	switch args[0] {
	case "list-sessions":
		if len(f.Sessions) == 0 {
			return "", "", 1, nil // tmux exits 1 when there are no sessions
		}
		rows := make([]string, len(f.Sessions))
		for i, s := range f.Sessions {
			rows[i] = s.format()
		}
		return strings.Join(rows, "\n"), "", 0, nil
	case "new-session":
		return "$1", "", 0, nil
	case "has-session":
		return "", "", 1, nil
	}
	return "", "", 0, nil
}

// called reports whether any recorded tmux invocation contains all the given
// substrings, in any position.
func (f *fakeTmux) called(want ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		joined := strings.Join(c, " ")
		ok := true
		for _, w := range want {
			if !strings.Contains(joined, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// killed returns every kill-session target, in order.
func (f *fakeTmux) killed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		if len(c) >= 3 && c[0] == "kill-session" {
			out = append(out, c[2])
		}
	}
	return out
}

// mockHook records hook triggers and can fail a named one.
type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

// withHook forces every operation on h to use the given hook, bypassing
// config lookup. This is the seam that keeps the hook tests intact.
func withHook(h *Herd, m hooks.Hook) *Herd {
	h.newHook = func(config.HooksConfig) hooks.Hook { return m }
	return h
}

// TestFakeTmux_smoke validates the shared tmux fake that the session and
// worktree domains (Tasks 4-5) build on. It stays off the Sessions table and
// sessionRow.Project, which depend on the tmux SplitN widening Task 4 lands
// before any list-sessions test may set them.
func TestFakeTmux_smoke(t *testing.T) {
	f := &fakeTmux{}
	// A Herd routes tmux through the runner it is given.
	_ = New(&config.Config{}, nil, Deps{Tmux: f})

	row := sessionRow{ID: "$1", Name: "myapp-feat", Canonical: "myapp-feat", Type: string(SessionTypeAgent)}
	if !strings.Contains(row.format(), "myapp-feat") {
		t.Fatalf("format() = %q, want it to contain the session name", row.format())
	}

	if _, _, _, err := f.Run("kill-session", "-t", "$1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called("kill-session", "$1") {
		t.Errorf("called() did not observe the kill-session invocation; calls=%v", f.Calls)
	}
	if got := f.killed(); len(got) != 1 || got[0] != "$1" {
		t.Errorf("killed() = %v, want [$1]", got)
	}
}
