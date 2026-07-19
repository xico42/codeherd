package herd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
	"github.com/xico42/codeherd/internal/semconv"
)

// workspaceHerd builds a Herd wired to the given fakes, with one configured
// project "myapp". Returns the Herd and the projects_dir so tests can
// materialize the worktree paths derived from a Ref.
func workspaceHerd(t *testing.T, g *fakeGit, f *fakeTmux) (*Herd, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	return New(cfg, nil, Deps{Tmux: f, Git: g}), tmpDir
}

// cloneDirPath returns the expected clone path for "myapp" in tmpDir.
func cloneDirPath(tmpDir string) string {
	return filepath.Join(tmpDir, "github.com", "user", "myapp")
}

// ── freshenStartPoint ─────────────────────────────────────────────────────────

func TestFreshenStartPoint_localBranchPreferred(t *testing.T) {
	g := &fakeGit{HasLocalBranchFn: func(_, _ string) (bool, error) { return true, nil }}
	h, _ := workspaceHerd(t, g, &fakeTmux{})
	got := h.freshenStartPoint("/clone", "main")
	if got != "main" {
		t.Errorf("start point = %q, want %q", got, "main")
	}
	if !g.called("Fetch", "origin", "main") {
		t.Errorf("expected Fetch origin main; calls=%v", g.Calls)
	}
	if !g.called("FastForward", "origin", "main") {
		t.Errorf("expected FastForward origin main; calls=%v", g.Calls)
	}
}

func TestFreshenStartPoint_noLocalBranchUsesRemoteTracking(t *testing.T) {
	g := &fakeGit{HasLocalBranchFn: func(_, _ string) (bool, error) { return false, nil }}
	h, _ := workspaceHerd(t, g, &fakeTmux{})
	got := h.freshenStartPoint("/clone", "feat-x")
	if got != "origin/feat-x" {
		t.Errorf("start point = %q, want %q", got, "origin/feat-x")
	}
	if g.called("FastForward") {
		t.Errorf("did not expect fast-forward; calls=%v", g.Calls)
	}
}

func TestFreshenStartPoint_fetchFailsFallsBackToRaw(t *testing.T) {
	g := &fakeGit{FetchFn: func(_, _, _ string) error { return fmt.Errorf("no such branch") }}
	h, _ := workspaceHerd(t, g, &fakeTmux{})
	got := h.freshenStartPoint("/clone", "v1.2.3")
	if got != "v1.2.3" {
		t.Errorf("start point = %q, want %q", got, "v1.2.3")
	}
}

func TestFreshenStartPoint_explicitRemoteRef(t *testing.T) {
	g := &fakeGit{RemotesFn: func(_ string) ([]string, error) { return []string{"origin", "upstream"}, nil }}
	h, _ := workspaceHerd(t, g, &fakeTmux{})
	got := h.freshenStartPoint("/clone", "upstream/feat-x")
	if got != "upstream/feat-x" {
		t.Errorf("start point = %q, want %q", got, "upstream/feat-x")
	}
	if !g.called("Fetch", "upstream", "feat-x") {
		t.Errorf("expected Fetch upstream feat-x; calls=%v", g.Calls)
	}
}

// ── EnsureWorkspace (default / --from) ────────────────────────────────────────

func TestEnsureWorkspace_notCloned(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{})
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestEnsureWorkspace_worktreeExists(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{})
	if !errors.Is(err, ErrWorktreeExists) {
		t.Errorf("expected ErrWorktreeExists, got %v", err)
	}
}

func TestEnsureWorkspace_success(t *testing.T) {
	g := &fakeGit{}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("Add", "feature") {
		t.Errorf("expected git.Add to be called; calls=%v", g.Calls)
	}
	want := cloneDirPath(tmpDir) + "__worktrees/feature"
	if ws.Path != want {
		t.Errorf("path = %q, want %q", ws.Path, want)
	}
	if ws.Ref.Branch != "feature" {
		t.Errorf("ref branch = %q, want feature", ws.Ref.Branch)
	}
}

func TestEnsureWorkspace_branchNotFound_createsFromFreshDefault(t *testing.T) {
	g := &fakeGit{
		AddFn:            func(_, _, _ string) error { return fmt.Errorf("invalid reference") },
		HasLocalBranchFn: func(_, _ string) (bool, error) { return false, nil },
	}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", "new-feature"), EnsureOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("AddNewBranchFrom", "new-feature", "origin/main") {
		t.Errorf("expected AddNewBranchFrom from origin/main; calls=%v", g.Calls)
	}
	if ws.Ref.Branch != "new-feature" {
		t.Errorf("branch = %q, want new-feature", ws.Ref.Branch)
	}
}

func TestEnsureWorkspace_withFromBranch(t *testing.T) {
	g := &fakeGit{HasLocalBranchFn: func(_, _ string) (bool, error) { return false, nil }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", "my-feature"), EnsureOpts{StartPoint: "feature-auth"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("AddNewBranchFrom", "my-feature", "origin/feature-auth") {
		t.Errorf("expected AddNewBranchFrom from origin/feature-auth; calls=%v", g.Calls)
	}
	if !g.called("Fetch", "origin", "feature-auth") {
		t.Errorf("expected Fetch origin feature-auth; calls=%v", g.Calls)
	}
	want := cloneDirPath(tmpDir) + "__worktrees/my-feature"
	if ws.Path != want {
		t.Errorf("path = %q, want %q", ws.Path, want)
	}
}

func TestEnsureWorkspace_fromAndTrackMutuallyExclusive(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "x"), EnsureOpts{StartPoint: "main", Track: "origin/main"})
	if err == nil {
		t.Fatal("expected error when both StartPoint and Track are set")
	}
}

func TestEnsureWorkspace_fromBranch_notCloned(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{StartPoint: "main"})
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestEnsureWorkspace_fromBranch_worktreeExists(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{StartPoint: "main"})
	if !errors.Is(err, ErrWorktreeExists) {
		t.Errorf("expected ErrWorktreeExists, got %v", err)
	}
}

func TestEnsureWorkspace_fromBranch_gitError(t *testing.T) {
	g := &fakeGit{AddNewBranchFromFn: func(_, _, _, _ string) error { return fmt.Errorf("invalid start point") }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{StartPoint: "nonexistent"})
	if err == nil {
		t.Fatal("expected error when AddNewBranchFrom fails")
	}
}

func TestEnsureWorkspace_unknownProject(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.EnsureWorkspace(h.Ref("unknown", "feature"), EnsureOpts{})
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestEnsureWorkspace_bothAddsFail(t *testing.T) {
	g := &fakeGit{
		AddFn:              func(_, _, _ string) error { return fmt.Errorf("invalid reference") },
		AddNewBranchFromFn: func(_, _, _, _ string) error { return fmt.Errorf("already exists") },
	}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "new-feature"), EnsureOpts{})
	if err == nil {
		t.Fatal("expected error when both Add and AddNewBranchFrom fail")
	}
}

func TestEnsureWorkspace_branchFlattened(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", "feature/login"), EnsureOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(ws.Path) != "feature-login" {
		t.Errorf("expected flattened path, got %q", ws.Path)
	}
}

func TestEnsureWorkspace_invalidRepo(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"badrepo": {Repo: "not-a-valid-repo-url"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: &fakeTmux{}, Git: &fakeGit{}})
	_, err := h.EnsureWorkspace(h.Ref("badrepo", "feature"), EnsureOpts{})
	if err == nil {
		t.Fatal("expected error for invalid repo URL")
	}
}

// ── EnsureWorkspace (--track) ─────────────────────────────────────────────────

func TestEnsureWorkspace_tracking_success(t *testing.T) {
	g := &fakeGit{}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", ""), EnsureOpts{Track: "feat-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("Fetch", "origin", "feat-x") {
		t.Errorf("expected Fetch origin feat-x; calls=%v", g.Calls)
	}
	if !g.called("AddTracking", "feat-x", "origin/feat-x") {
		t.Errorf("expected AddTracking feat-x origin/feat-x; calls=%v", g.Calls)
	}
	// The result's Ref is authoritative — the local branch was derived.
	if ws.Ref.Branch != "feat-x" {
		t.Errorf("ref branch = %q, want feat-x", ws.Ref.Branch)
	}
	want := cloneDirPath(tmpDir) + "__worktrees/feat-x"
	if ws.Path != want {
		t.Errorf("path = %q, want %q", ws.Path, want)
	}
}

func TestEnsureWorkspace_tracking_overrideName(t *testing.T) {
	g := &fakeGit{RemotesFn: func(_ string) ([]string, error) { return []string{"origin", "upstream"}, nil }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := h.EnsureWorkspace(h.Ref("myapp", "review"), EnsureOpts{Track: "upstream/feat-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("AddTracking", "review", "upstream/feat-x") {
		t.Errorf("expected AddTracking review upstream/feat-x; calls=%v", g.Calls)
	}
	if ws.Ref.Branch != "review" {
		t.Errorf("ref branch = %q, want review", ws.Ref.Branch)
	}
}

func TestEnsureWorkspace_tracking_localBranchExists(t *testing.T) {
	g := &fakeGit{HasLocalBranchFn: func(_, _ string) (bool, error) { return true, nil }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", ""), EnsureOpts{Track: "feat-x"})
	if !errors.Is(err, ErrLocalBranchExists) {
		t.Errorf("expected ErrLocalBranchExists, got %v", err)
	}
	if g.called("AddTracking") {
		t.Error("AddTracking should not be called when the branch already exists")
	}
}

func TestEnsureWorkspace_tracking_fetchFails(t *testing.T) {
	g := &fakeGit{FetchFn: func(_, _, _ string) error { return fmt.Errorf("no such ref") }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", ""), EnsureOpts{Track: "feat-x"})
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}
	if g.called("AddTracking") {
		t.Error("AddTracking should not run after a failed fetch")
	}
}

func TestEnsureWorkspace_tracking_notCloned(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.EnsureWorkspace(h.Ref("myapp", ""), EnsureOpts{Track: "feat-x"})
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

// ── Hooks ─────────────────────────────────────────────────────────────────────

func TestEnsureWorkspace_TriggersHooks(t *testing.T) {
	g := &fakeGit{}
	hook := &mockHook{}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	withHook(h, hook)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{}); err != nil {
		t.Fatalf("EnsureWorkspace error = %v", err)
	}
	if len(hook.calls) < 2 {
		t.Fatalf("expected at least 2 hook calls, got %d", len(hook.calls))
	}
	if hook.calls[0].name != semconv.HookPreWorktree {
		t.Errorf("first hook = %q, want %q", hook.calls[0].name, semconv.HookPreWorktree)
	}
	if hook.calls[1].name != semconv.HookPostWorktree {
		t.Errorf("second hook = %q, want %q", hook.calls[1].name, semconv.HookPostWorktree)
	}
}

func TestEnsureWorkspace_preHookFailure(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	withHook(h, &mockHook{failOn: semconv.HookPreWorktree})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{})
	if err == nil || !contains(err.Error(), "pre-worktree hook") {
		t.Errorf("expected pre-worktree hook error, got %v", err)
	}
}

func TestEnsureWorkspace_postHookFailure(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	withHook(h, &mockHook{failOn: semconv.HookPostWorktree})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{})
	if err == nil || !contains(err.Error(), "post-worktree hook") {
		t.Errorf("expected post-worktree hook error, got %v", err)
	}
}

func TestEnsureWorkspace_fromBranch_preHookFailure(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	withHook(h, &mockHook{failOn: semconv.HookPreWorktree})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{StartPoint: "main"})
	if err == nil || !contains(err.Error(), "pre-worktree hook") {
		t.Errorf("expected pre-worktree hook error, got %v", err)
	}
}

func TestEnsureWorkspace_fromBranch_postHookFailure(t *testing.T) {
	h, tmpDir := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	withHook(h, &mockHook{failOn: semconv.HookPostWorktree})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := h.EnsureWorkspace(h.Ref("myapp", "feature"), EnsureOpts{StartPoint: "main"})
	if err == nil || !contains(err.Error(), "post-worktree hook") {
		t.Errorf("expected post-worktree hook error, got %v", err)
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList_allProjects(t *testing.T) {
	tmpDir := t.TempDir()
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{
			{Path: cloneDirPath(tmpDir), Branch: "main"},
			{Path: cloneDirPath(tmpDir) + "__worktrees/feature", Branch: "feature"},
		}, nil
	}}
	h, _ := workspaceHerdIn(t, tmpDir, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(spaces))
	}
	if spaces[0].Ref.Project != "myapp" {
		t.Errorf("expected project myapp, got %q", spaces[0].Ref.Project)
	}
}

func TestList_withRunningSession(t *testing.T) {
	tmpDir := t.TempDir()
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: cloneDirPath(tmpDir) + "__worktrees/feature", Branch: "feature"}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "running", Branch: "feature", Project: "myapp"},
	}}
	h, _ := workspaceHerdIn(t, tmpDir, g, f)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) == 0 {
		t.Fatal("expected workspaces")
	}
	if spaces[0].Agent == nil {
		t.Errorf("expected running agent to be joined to its workspace")
	}
}

func TestList_cloneDirDetachedUsesDefaultBranch(t *testing.T) {
	tmpDir := t.TempDir()
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: cloneDirPath(tmpDir), Branch: "", Detached: true}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-main", Canonical: "myapp-main",
			Type: "agent", Status: "running", Branch: "main", Project: "myapp"},
	}}
	h, _ := workspaceHerdIn(t, tmpDir, g, f)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(spaces))
	}
	if spaces[0].Agent == nil {
		t.Errorf("expected session correlated via DefaultBranch identity")
	}
	if spaces[0].HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", spaces[0].HeadHint)
	}
	// A detached clone dir still has an identity — the default branch — so the
	// row stays labelled "main (detached)" rather than losing its branch.
	if spaces[0].DisplayBranch != "main" {
		t.Errorf("DisplayBranch = %q, want %q for a detached clone dir", spaces[0].DisplayBranch, "main")
	}
}

func TestList_skipUncloned(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	// cloneDir does not exist — project should be skipped.
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 0 {
		t.Errorf("expected no workspaces for uncloned project, got %d", len(spaces))
	}
}

func TestList_singleProject_notConfigured(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.List("nonexistent")
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestList_gitListError(t *testing.T) {
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) { return nil, fmt.Errorf("git error") }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 0 {
		t.Errorf("expected 0 workspaces when git.List fails, got %d", len(spaces))
	}
}

func TestList_invalidRepoSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"badrepo": {Repo: "not-a-valid-url"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: &fakeTmux{}, Git: &fakeGit{}})
	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spaces) != 0 {
		t.Errorf("expected 0 workspaces for invalid repo, got %d", len(spaces))
	}
}

// ── Teardown ──────────────────────────────────────────────────────────────────

func TestTeardown_notFound(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{})
	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("expected ErrWorktreeNotFound, got %v", err)
	}
}

func TestTeardown_success(t *testing.T) {
	g := &fakeGit{}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("Remove") {
		t.Errorf("expected git.Remove to be called; calls=%v", g.Calls)
	}
}

func TestTeardown_unknownProject(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	err := h.Teardown(h.Ref("unknown", "feature"), TeardownOpts{})
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestTeardown_listSessionsError(t *testing.T) {
	f := &fakeTmux{RunFn: func(args ...string) (string, string, int, error) {
		if args[0] == "list-sessions" {
			return "", "boom", 2, fmt.Errorf("tmux not running")
		}
		return "", "", 0, nil
	}}
	h, tmpDir := workspaceHerd(t, &fakeGit{}, f)
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{})
	if err == nil {
		t.Fatal("expected error when listing tmux sessions fails")
	}
}

func TestTeardown_gitRemoveError(t *testing.T) {
	g := &fakeGit{RemoveFn: func(_, _ string) error { return fmt.Errorf("git worktree remove failed") }}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{})
	if err == nil || !contains(err.Error(), "removing worktree") {
		t.Errorf("expected 'removing worktree' error, got %v", err)
	}
}

func TestTeardown_forceKillSessionError(t *testing.T) {
	f := &fakeTmux{
		Sessions: []sessionRow{
			{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature", Type: "agent", Branch: "feature", Project: "myapp"},
		},
		RunFn: func(args ...string) (string, string, int, error) {
			switch args[0] {
			case "list-sessions":
				return strings.Join([]string{"$1\tmyapp-feature\tmyapp-feature\tagent\t\t\t\t\tfeature\tmyapp"}, "\n"), "", 0, nil
			case "kill-session":
				return "", "kill failed", 1, fmt.Errorf("kill failed")
			}
			return "", "", 0, nil
		},
	}
	h, tmpDir := workspaceHerd(t, &fakeGit{}, f)
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{Force: true})
	if err == nil || !contains(err.Error(), "killing session") {
		t.Errorf("expected 'killing session' error, got %v", err)
	}
}

func TestTeardown_Force_KillsBothSessionTypes(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature", Type: "agent", Branch: "feature", Project: "myapp"},
		{ID: "$2", Name: "myapp-feature~sh", Canonical: "myapp-feature", Type: "shell", Branch: "feature", Project: "myapp"},
	}}
	g := &fakeGit{}
	h, tmpDir := workspaceHerd(t, g, f)
	if err := os.MkdirAll(cloneDirPath(tmpDir)+"__worktrees/feature", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Teardown(h.Ref("myapp", "feature"), TeardownOpts{Force: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	killed := f.killed()
	sort.Strings(killed)
	if len(killed) != 2 || killed[0] != "$1" || killed[1] != "$2" {
		t.Errorf("killed = %v, want [$1 $2]", killed)
	}
}

// ── RemoteBranches ────────────────────────────────────────────────────────────

func TestRemoteBranches_fetchesThenLists(t *testing.T) {
	g := &fakeGit{ListRemoteBranchesFn: func(string) ([]git.RemoteBranch, error) {
		return []git.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}, nil
	}}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := h.RemoteBranches("myapp", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.called("FetchAll") {
		t.Error("expected FetchAll to be called")
	}
	if len(got) != 1 || got[0].Ref != "origin/feat-x" {
		t.Errorf("branches = %+v", got)
	}
}

func TestRemoteBranches_noFetch(t *testing.T) {
	g := &fakeGit{ListRemoteBranchesFn: func(string) ([]git.RemoteBranch, error) {
		return []git.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}, nil
	}}
	h, tmpDir := workspaceHerd(t, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := h.RemoteBranches("myapp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.called("FetchAll") {
		t.Error("no-fetch RemoteBranches must not fetch")
	}
	if len(got) != 1 {
		t.Errorf("branches = %+v", got)
	}
}

func TestRemoteBranches_notCloned(t *testing.T) {
	h, _ := workspaceHerd(t, &fakeGit{}, &fakeTmux{})
	_, err := h.RemoteBranches("myapp", false)
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

// ── The defect, stated as tests ───────────────────────────────────────────────

// The defect, stated as a test. A worktree deleted under an active profile
// must take its sessions with it. worktree.Delete rebuilt the names with
// SessionName("", …), searched for myapp-feat, missed work-myapp-feat, and
// force-deleted the worktree anyway — leaving the agent process alive against
// a directory that no longer existed.
func TestTeardown_underProfile_killsSessionsThenDeletesWorktree(t *testing.T) {
	g := &fakeGit{}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "work-myapp-feat~sh", Canonical: "work-myapp-feat",
			Type: "shell", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: g})

	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := h.Teardown(h.Ref("myapp", "feat"), TeardownOpts{Force: true}); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	killed := f.killed()
	sort.Strings(killed)
	if len(killed) != 2 || killed[0] != "$1" || killed[1] != "$2" {
		t.Errorf("killed = %v, want [$1 $2]; a missed kill orphans the agent process", killed)
	}
	if !g.called("Remove", wtPath) {
		t.Errorf("worktree was not removed; calls=%v", g.Calls)
	}
}

// Without --force, a running session blocks the delete rather than being
// killed under the user.
func TestTeardown_runningSessionWithoutForce(t *testing.T) {
	g := &fakeGit{}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feat", Canonical: "myapp-feat",
			Type: "agent", Branch: "feat", Project: "myapp"},
	}}
	h, dir := workspaceHerd(t, g, f)
	if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := h.Teardown(h.Ref("myapp", "feat"), TeardownOpts{})
	if !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("err = %v, want ErrSessionRunning", err)
	}
	if len(f.killed()) != 0 {
		t.Errorf("killed %v without --force", f.killed())
	}
	if g.called("Remove") {
		t.Error("worktree was removed despite a running session")
	}
}

// A non-force Teardown must refuse when a pre-upgrade session (empty Project,
// real stored Canonical) is running — same as it does for a normal session.
// Before matching on the stored canonical, the pre-check missed it and let the
// delete proceed.
func TestTeardown_nonForce_preUpgradeSessionRunning_refuses(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: ""},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	// Teardown stats the worktree path before the running-check, so create it.
	if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := h.Teardown(h.Ref("myapp", "feat"), TeardownOpts{}); !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("Teardown err = %v, want ErrSessionRunning", err)
	}
}

// List joins worktrees to sessions on the Ref, so the join is profile-correct.
// worktree.Service.List hardcoded SessionName("", …) at line 593, which is why
// `ch list worktree`'s "(running)" marker never appeared under a profile.
func TestList_underProfile_findsRunningSession(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "feat"}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(spaces))
	}
	if spaces[0].Agent == nil {
		t.Fatal("running agent session not joined to its workspace under a profile")
	}
	if spaces[0].Agent.ID != "$1" {
		t.Errorf("Agent.ID = %q, want $1", spaces[0].Agent.ID)
	}
}

// The main worktree's row is labelled by its identity (the default branch),
// not by whatever HEAD happens to sit on. When the clone dir has a non-default
// branch checked out, DisplayBranch must stay "main" and HeadHint must carry
// the actual checkout — otherwise "main" vanishes from every listing and the
// main worktree becomes unspottable (the geomonitor bug).
func TestList_mainWorktree_divergedHead_displayBranchIsIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: cloneDirPath(tmpDir), Branch: "docs/rbac-epic"}}, nil
	}}
	h, _ := workspaceHerdIn(t, tmpDir, g, &fakeTmux{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(spaces))
	}
	if !spaces[0].IsMain {
		t.Errorf("IsMain = false, want true for the clone dir")
	}
	if spaces[0].Ref.Branch != "main" {
		t.Errorf("Ref.Branch = %q, want %q — identity is the default branch", spaces[0].Ref.Branch, "main")
	}
	if spaces[0].DisplayBranch != "main" {
		t.Errorf("DisplayBranch = %q, want %q — the main row is labelled by its identity", spaces[0].DisplayBranch, "main")
	}
	if spaces[0].HeadHint != "on docs/rbac-epic" {
		t.Errorf("HeadHint = %q, want %q", spaces[0].HeadHint, "on docs/rbac-epic")
	}
}

// Addressing and display are separate. Ref stays the folder identity so
// operations always land on the right worktree; display, for a non-main
// worktree with no session, is git's live branch — the ground truth — with no
// divergence hint, because the folder name is only an addressing key and there
// is no recorded original branch to diverge from. (Row G′ of the settled table.)
func TestList_divergedHead_refKeepsIdentityBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		// The folder is "feat" but HEAD sits on "other" — with no session, we
		// cannot know "feat" was ever the intended branch, so trust git.
		return []git.WorktreeInfo{{Path: wtPath, Branch: "other"}}, nil
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: &fakeTmux{}, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].Ref.Branch != "feat" {
		t.Errorf("Ref.Branch = %q, want %q — identity must survive for addressing", spaces[0].Ref.Branch, "feat")
	}
	if spaces[0].DisplayBranch != "other" {
		t.Errorf("DisplayBranch = %q, want %q — no session, so show git's live branch", spaces[0].DisplayBranch, "other")
	}
	if spaces[0].HeadHint != "" {
		t.Errorf("HeadHint = %q, want empty — no recorded original to diverge from", spaces[0].HeadHint)
	}
}

// The geomonitor chore-cron-rework row: a worktree whose directory name does
// not match its branch, with no session. The folder name must never surface;
// we show git's live branch, clean, exactly as v0.2.0's CLI did. (Row F.)
func TestList_nonMainWorktree_dirNameMismatch_noSession_showsLiveBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "chore-cron-rework")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "chore/restore-cron-rework"}}, nil
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: &fakeTmux{}, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].DisplayBranch != "chore/restore-cron-rework" {
		t.Errorf("DisplayBranch = %q, want the live branch %q", spaces[0].DisplayBranch, "chore/restore-cron-rework")
	}
	if spaces[0].HeadHint != "" {
		t.Errorf("HeadHint = %q, want empty — no session, no divergence", spaces[0].HeadHint)
	}
	if spaces[0].BranchLabel() != "chore/restore-cron-rework" {
		t.Errorf("BranchLabel() = %q, want %q", spaces[0].BranchLabel(), "chore/restore-cron-rework")
	}
}

// A running session records the branch the worktree is for (@codeherd_branch).
// When HEAD has since moved off it, that is a genuine divergence: label the row
// by the recorded original and put the live branch in the hint. (Row G.)
func TestList_nonMainWorktree_sessionDivergence_showsRecordedBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "other"}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feat", Canonical: "myapp-feat",
			Type: "agent", Status: "running", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: f, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].Agent == nil {
		t.Fatal("expected the session to join its workspace")
	}
	if spaces[0].DisplayBranch != "feat" {
		t.Errorf("DisplayBranch = %q, want the recorded branch %q", spaces[0].DisplayBranch, "feat")
	}
	if spaces[0].HeadHint != "on other" {
		t.Errorf("HeadHint = %q, want %q", spaces[0].HeadHint, "on other")
	}
}

// When the recorded branch and git's HEAD agree, there is no divergence and we
// prefer git's live branch for display — the recorded value may be flattened,
// git's is the real, unflattened name. (Row E.)
func TestList_nonMainWorktree_sessionAgrees_showsLiveBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat-x")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "feat/x"}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		// Recorded flattened (a session launched from a listed Ref); git's
		// "feat/x" is the same branch, unflattened.
		{ID: "$1", Name: "myapp-feat-x", Canonical: "myapp-feat-x",
			Type: "agent", Status: "running", Branch: "feat-x", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: f, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].DisplayBranch != "feat/x" {
		t.Errorf("DisplayBranch = %q, want git's unflattened branch %q", spaces[0].DisplayBranch, "feat/x")
	}
	if spaces[0].HeadHint != "" {
		t.Errorf("HeadHint = %q, want empty — recorded and live agree", spaces[0].HeadHint)
	}
}

// A detached non-main worktree with a running session recovers the branch from
// the session record, so the row stays identifiable rather than collapsing to a
// bare "(detached)". (Row H.)
func TestList_nonMainWorktree_detachedWithSession_recoversRecordedBranch(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	wtPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &fakeGit{ListFn: func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: wtPath, Branch: "", Detached: true}}, nil
	}}
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feat", Canonical: "myapp-feat",
			Type: "agent", Status: "running", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: f, Git: g})

	spaces, err := h.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if spaces[0].DisplayBranch != "feat" {
		t.Errorf("DisplayBranch = %q, want recovered %q", spaces[0].DisplayBranch, "feat")
	}
	if spaces[0].HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", spaces[0].HeadHint)
	}
}

// FormatBranchLabel is the single formatter both the CLI and the TUI render
// through. A diverged workspace carries its identity in DisplayBranch and the
// live branch in HeadHint, so the two never restate each other.
func TestFormatBranchLabel(t *testing.T) {
	tests := []struct {
		name          string
		displayBranch string
		headHint      string
		want          string
	}{
		{"plain branch", "chore/frontend-arch", "", "chore/frontend-arch"},
		{"main on default", "main", "", "main"},
		{"main diverged", "main", "on docs/rbac-epic", "main (on docs/rbac-epic)"},
		{"session divergence", "feat", "on other", "feat (on other)"},
		{"detached with identity", "main", "detached", "main (detached)"},
		{"detached without identity", "", "detached", "(detached)"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBranchLabel(tt.displayBranch, tt.headHint); got != tt.want {
				t.Errorf("FormatBranchLabel(%q, %q) = %q, want %q", tt.displayBranch, tt.headHint, got, tt.want)
			}
			// Workspace.BranchLabel is the same formatter over the struct.
			ws := Workspace{DisplayBranch: tt.displayBranch, HeadHint: tt.headHint}
			if got := ws.BranchLabel(); got != tt.want {
				t.Errorf("Workspace.BranchLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// workspaceHerdIn is workspaceHerd but reusing an existing tmpDir so a ListFn
// closure can reference the clone path.
func workspaceHerdIn(t *testing.T, tmpDir string, g *fakeGit, f *fakeTmux) (*Herd, string) {
	t.Helper()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	return New(cfg, nil, Deps{Tmux: f, Git: g}), tmpDir
}

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
