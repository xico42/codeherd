package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

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

// TestNewRealWorktreeRunner verifies the constructor returns a non-nil runner.
func TestNewRealWorktreeRunner(t *testing.T) {
	r := NewRealWorktreeRunner()
	if r == nil {
		t.Fatal("NewRealWorktreeRunner() returned nil")
	}
}

func TestFlattenBranch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"feature", "feature"},
		{"feature/login", "feature-login"},
		{"fix/123/auth", "fix-123-auth"},
		{"main", "main"},
	}
	for _, tc := range cases {
		got := semconv.FlattenBranch(tc.in)
		if got != tc.want {
			t.Errorf("semconv.FlattenBranch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	input := `worktree /home/user/projects/myapp
HEAD abc123
branch refs/heads/main

worktree /home/user/projects/myapp__worktrees/feature
HEAD def456
branch refs/heads/feature

worktree /home/user/projects/myapp__worktrees/detached
HEAD ghi789
detached

`
	got := parseWorktreePorcelain(input)

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].Path != "/home/user/projects/myapp" || got[0].Branch != "main" {
		t.Errorf("entry 0: %+v", got[0])
	}
	if got[1].Path != "/home/user/projects/myapp__worktrees/feature" || got[1].Branch != "feature" {
		t.Errorf("entry 1: %+v", got[1])
	}
	if got[2].Branch != "" {
		t.Errorf("entry 2 should have empty branch for detached HEAD, got %q", got[2].Branch)
	}
}

func TestParseWorktreePorcelain_empty(t *testing.T) {
	got := parseWorktreePorcelain("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// TestParseWorktreePorcelain_noTrailingNewline exercises the tail-append path
// where the last entry is not followed by a blank line.
func TestParseWorktreePorcelain_noTrailingNewline(t *testing.T) {
	input := "worktree /home/user/projects/myapp\nHEAD abc123\nbranch refs/heads/main"
	got := parseWorktreePorcelain(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Path != "/home/user/projects/myapp" || got[0].Branch != "main" {
		t.Errorf("unexpected entry: %+v", got[0])
	}
}

func TestParseRemoteBranches(t *testing.T) {
	input := "origin/main\norigin/HEAD\norigin/feature/login\nupstream/bugfix\n"
	got := parseRemoteBranches(input)
	want := []RemoteBranch{
		{Remote: "origin", Branch: "main", Ref: "origin/main"},
		{Remote: "origin", Branch: "feature/login", Ref: "origin/feature/login"},
		{Remote: "upstream", Branch: "bugfix", Ref: "upstream/bugfix"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFreshenStartPoint_localBranchPreferred(t *testing.T) {
	git := &mockGit{hasLocalBranch: true}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "main")
	if got != "main" {
		t.Errorf("start point = %q, want %q", got, "main")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "main"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	if len(git.fastForwardCalls) != 1 || git.fastForwardCalls[0] != [2]string{"origin", "main"} {
		t.Errorf("fast-forward calls = %v", git.fastForwardCalls)
	}
}

func TestFreshenStartPoint_noLocalBranchUsesRemoteTracking(t *testing.T) {
	git := &mockGit{hasLocalBranch: false}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "feat-x")
	if got != "origin/feat-x" {
		t.Errorf("start point = %q, want %q", got, "origin/feat-x")
	}
	if len(git.fastForwardCalls) != 0 {
		t.Errorf("did not expect fast-forward, got %v", git.fastForwardCalls)
	}
}

func TestFreshenStartPoint_fetchFailsFallsBackToRaw(t *testing.T) {
	git := &mockGit{fetchErr: fmt.Errorf("no such branch")}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "v1.2.3")
	if got != "v1.2.3" {
		t.Errorf("start point = %q, want %q", got, "v1.2.3")
	}
}

func TestFreshenStartPoint_explicitRemoteRef(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin", "upstream"}}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "upstream/feat-x")
	if got != "upstream/feat-x" {
		t.Errorf("start point = %q, want %q", got, "upstream/feat-x")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"upstream", "feat-x"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
}

func TestParseRef(t *testing.T) {
	remotes := []string{"origin", "upstream"}
	cases := []struct {
		ref            string
		remote, branch string
		explicit       bool
	}{
		{"feat-x", "origin", "feat-x", false},
		{"feature/login", "origin", "feature/login", false},
		{"origin/feat-x", "origin", "feat-x", true},
		{"upstream/feature/login", "upstream", "feature/login", true},
		{"notaremote/x", "origin", "notaremote/x", false},
	}
	for _, tc := range cases {
		gotR, gotB, gotE := parseRef(remotes, tc.ref)
		if gotR != tc.remote || gotB != tc.branch || gotE != tc.explicit {
			t.Errorf("parseRef(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.ref, gotR, gotB, gotE, tc.remote, tc.branch, tc.explicit)
		}
	}
}

// mockGit records calls and controls return values.
type mockGit struct {
	addErr               error
	addNewErr            error
	addNewFromErr        error
	addNewFromCalled     bool
	addNewFromStartPoint string
	removeErr            error
	listResult           []WorktreeInfo
	listErr              error
	addCalled            bool
	addNewCalled         bool

	fetchErr          error
	fetchCalls        [][2]string // {remote, branch}
	fetchAllErr       error
	fetchAllCalled    bool
	fastForwardErr    error
	fastForwardCalls  [][2]string
	remotesResult     []string
	remotesErr        error
	listRemoteResult  []RemoteBranch
	listRemoteErr     error
	addTrackingErr    error
	addTrackingCalled bool
	addTrackingBranch string
	addTrackingRef    string
	hasLocalBranch    bool
	hasLocalBranchErr error
}

func (m *mockGit) Add(cloneDir, worktreePath, branch string) error {
	m.addCalled = true
	return m.addErr
}
func (m *mockGit) AddNewBranch(cloneDir, worktreePath, branch string) error {
	m.addNewCalled = true
	return m.addNewErr
}
func (m *mockGit) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	m.addNewFromCalled = true
	m.addNewFromStartPoint = startPoint
	return m.addNewFromErr
}
func (m *mockGit) Remove(cloneDir, worktreePath string) error { return m.removeErr }
func (m *mockGit) List(cloneDir string) ([]WorktreeInfo, error) {
	return m.listResult, m.listErr
}
func (m *mockGit) Fetch(cloneDir, remote, branch string) error {
	m.fetchCalls = append(m.fetchCalls, [2]string{remote, branch})
	return m.fetchErr
}
func (m *mockGit) FetchAll(cloneDir string) error {
	m.fetchAllCalled = true
	return m.fetchAllErr
}
func (m *mockGit) FastForward(cloneDir, remote, branch string) error {
	m.fastForwardCalls = append(m.fastForwardCalls, [2]string{remote, branch})
	return m.fastForwardErr
}
func (m *mockGit) Remotes(cloneDir string) ([]string, error) {
	return m.remotesResult, m.remotesErr
}
func (m *mockGit) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	return m.listRemoteResult, m.listRemoteErr
}
func (m *mockGit) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	m.addTrackingCalled = true
	m.addTrackingBranch = branch
	m.addTrackingRef = remoteRef
	return m.addTrackingErr
}
func (m *mockGit) HasLocalBranch(cloneDir, branch string) (bool, error) {
	return m.hasLocalBranch, m.hasLocalBranchErr
}

// mockTmuxRunner controls tmux subprocess results.
type mockTmuxRunner struct {
	exitCode int
	stdout   string
}

func (m *mockTmuxRunner) Run(args ...string) (string, string, int, error) {
	return m.stdout, "", m.exitCode, nil
}

// mockTmuxRunnerWithError returns an error on Run.
type mockTmuxRunnerWithError struct {
	err error
}

func (m *mockTmuxRunnerWithError) Run(args ...string) (string, string, int, error) {
	return "", "", -1, m.err
}

// makeService creates a Service backed by mocks with a temp projects dir.
// Returns the Service and the temp dir.
func makeService(t *testing.T, git WorktreeRunner, tmuxRunner tmux.Runner) (*Service, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	tc := tmux.NewClient(tmuxRunner)
	return NewService(cfg, git, tc, &mockHook{}), tmpDir
}

// cloneDir returns the expected clone path for "myapp" in tmpDir.
func cloneDirPath(tmpDir string) string {
	return filepath.Join(tmpDir, "github.com", "user", "myapp")
}

func TestService_New_notCloned(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.New("myapp", "feature")
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestService_New_worktreeExists(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	clone := cloneDirPath(tmpDir)
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create the worktree path
	worktreePath := clone + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "feature")
	if !errors.Is(err, ErrWorktreeExists) {
		t.Errorf("expected ErrWorktreeExists, got %v", err)
	}
}

func TestService_New_success(t *testing.T) {
	git := &mockGit{}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.New("myapp", "feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addCalled {
		t.Error("expected git.Add to be called")
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
	}
}

func TestService_New_branchNotFound_createsFromFreshDefault(t *testing.T) {
	git := &mockGit{addErr: fmt.Errorf("invalid reference"), hasLocalBranch: false}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.New("myapp", "new-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addNewFromCalled {
		t.Error("expected AddNewBranchFrom to be called on Add failure")
	}
	if git.addNewFromStartPoint != "origin/main" {
		t.Errorf("start point = %q, want %q", git.addNewFromStartPoint, "origin/main")
	}
	if result.Branch != "new-feature" {
		t.Errorf("branch = %q, want %q", result.Branch, "new-feature")
	}
}

func TestService_New_withFromBranch(t *testing.T) {
	git := &mockGit{hasLocalBranch: false}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewFrom("myapp", "my-feature", "feature-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addNewFromCalled {
		t.Error("expected AddNewBranchFrom to be called")
	}
	if git.addNewFromStartPoint != "origin/feature-auth" {
		t.Errorf("start point = %q, want %q", git.addNewFromStartPoint, "origin/feature-auth")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "feature-auth"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/my-feature"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
	}
}

func TestService_NewTracking_success(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewTracking("myapp", "", "feat-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "feat-x"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	if !git.addTrackingCalled || git.addTrackingBranch != "feat-x" || git.addTrackingRef != "origin/feat-x" {
		t.Errorf("AddTracking branch=%q ref=%q", git.addTrackingBranch, git.addTrackingRef)
	}
	if result.Branch != "feat-x" {
		t.Errorf("branch = %q, want feat-x", result.Branch)
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/feat-x"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
	}
}

func TestService_NewTracking_overrideName(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin", "upstream"}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewTracking("myapp", "review", "upstream/feat-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if git.addTrackingBranch != "review" || git.addTrackingRef != "upstream/feat-x" {
		t.Errorf("AddTracking branch=%q ref=%q", git.addTrackingBranch, git.addTrackingRef)
	}
	if result.Branch != "review" {
		t.Errorf("branch = %q, want review", result.Branch)
	}
}

func TestService_NewTracking_localBranchExists(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}, hasLocalBranch: true}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewTracking("myapp", "", "feat-x")
	if !errors.Is(err, ErrLocalBranchExists) {
		t.Errorf("expected ErrLocalBranchExists, got %v", err)
	}
	if git.addTrackingCalled {
		t.Error("AddTracking should not be called when the branch already exists")
	}
}

func TestService_NewTracking_fetchFails(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}, fetchErr: fmt.Errorf("no such ref")}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewTracking("myapp", "", "feat-x")
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}
	if git.addTrackingCalled {
		t.Error("AddTracking should not run after a failed fetch")
	}
}

func TestService_NewTracking_notCloned(t *testing.T) {
	svc, _ := makeService(t, &mockGit{remotesResult: []string{"origin"}}, &mockTmuxRunner{})
	_, err := svc.NewTracking("myapp", "", "feat-x")
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestService_RemoteBranches_fetchesThenLists(t *testing.T) {
	git := &mockGit{listRemoteResult: []RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := svc.RemoteBranches("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.fetchAllCalled {
		t.Error("expected FetchAll to be called")
	}
	if len(got) != 1 || got[0].Ref != "origin/feat-x" {
		t.Errorf("branches = %+v", got)
	}
}

func TestService_ListRemoteBranches_noFetch(t *testing.T) {
	git := &mockGit{listRemoteResult: []RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListRemoteBranches("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if git.fetchAllCalled {
		t.Error("ListRemoteBranches must not fetch")
	}
	if len(got) != 1 {
		t.Errorf("branches = %+v", got)
	}
}

func TestService_NewFrom_notCloned(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.NewFrom("myapp", "feature", "main")
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestService_NewFrom_worktreeExists(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	clone := cloneDirPath(tmpDir)
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	worktreePath := clone + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewFrom("myapp", "feature", "main")
	if !errors.Is(err, ErrWorktreeExists) {
		t.Errorf("expected ErrWorktreeExists, got %v", err)
	}
}

func TestService_NewFrom_gitError(t *testing.T) {
	git := &mockGit{addNewFromErr: fmt.Errorf("invalid start point")}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewFrom("myapp", "feature", "nonexistent")
	if err == nil {
		t.Fatal("expected error when AddNewBranchFrom fails")
	}
}

func TestService_NewFrom_unknownProject(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.NewFrom("unknown", "feature", "main")
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestService_New_bothAddsFail(t *testing.T) {
	git := &mockGit{
		addErr:        fmt.Errorf("invalid reference"),
		addNewFromErr: fmt.Errorf("already exists"),
	}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "new-feature")
	if err == nil {
		t.Fatal("expected error when both Add and AddNewBranchFrom fail")
	}
}

func TestService_New_branchFlattened(t *testing.T) {
	git := &mockGit{}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.New("myapp", "feature/login")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result.Path, "feature-login") {
		t.Errorf("expected flattened path, got %q", result.Path)
	}
}

func TestService_List_allProjects(t *testing.T) {
	git := &mockGit{
		listResult: []WorktreeInfo{
			{Path: "/tmp/myapp", Branch: "main"},
			{Path: "/tmp/myapp__worktrees/feature", Branch: "feature"},
		},
	}
	// tmux exit 1 = no session
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 1})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Project != "myapp" {
		t.Errorf("expected project myapp, got %q", entries[0].Project)
	}
}

func TestService_List_withRunningSession(t *testing.T) {
	git := &mockGit{
		listResult: []WorktreeInfo{
			{Path: "/tmp/myapp__worktrees/feature", Branch: "feature"},
		},
	}
	// tmux exit 0 = session exists
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 0})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
	if entries[0].Session == "" {
		t.Errorf("expected session name to be populated, got empty")
	}
}

func TestService_List_skipUncloned(t *testing.T) {
	git := &mockGit{}
	svc, _ := makeService(t, git, &mockTmuxRunner{exitCode: 1})
	// cloneDir does NOT exist — project should be skipped

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries for uncloned project, got %d", len(entries))
	}
}

func TestService_List_singleProject_notConfigured(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.List("nonexistent")
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestService_Delete_notFound(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{exitCode: 1})
	// worktree dir does not exist
	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature"})
	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("expected ErrWorktreeNotFound, got %v", err)
	}
}

func TestService_Delete_sessionRunning_noForce(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{exitCode: 0}) // session exists
	// Create worktree dir so stat check passes
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature", Force: false})
	if !errors.Is(err, ErrSessionRunning) {
		t.Errorf("expected ErrSessionRunning, got %v", err)
	}
}

func TestService_Delete_sessionRunning_force(t *testing.T) {
	git := &mockGit{}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 0}) // session exists
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature", Force: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// git.Remove should have been called (no removeErr set means it succeeded)
}

func TestService_Delete_success(t *testing.T) {
	git := &mockGit{}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 1}) // no session
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestService_New_unknownProject(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.New("unknown", "feature")
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestService_Delete_tmuxError(t *testing.T) {
	tmuxErr := fmt.Errorf("tmux not running")
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunnerWithError{err: tmuxErr})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature"})
	if err == nil {
		t.Fatal("expected error from tmux HasSession")
	}
	if !strings.Contains(err.Error(), "checking tmux session") {
		t.Errorf("expected 'checking tmux session' in error, got: %v", err)
	}
}

func TestService_Delete_gitRemoveError(t *testing.T) {
	removeErr := fmt.Errorf("git worktree remove failed")
	git := &mockGit{removeErr: removeErr}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 1}) // no session
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature"})
	if err == nil {
		t.Fatal("expected error from git Remove")
	}
	if !strings.Contains(err.Error(), "removing worktree") {
		t.Errorf("expected 'removing worktree' in error, got: %v", err)
	}
}

func TestService_Delete_unknownProject(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	err := svc.Delete(DeleteRequest{Project: "unknown", Branch: "feature"})
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestService_resolvePaths_invalidRepo(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"badrepo": {Repo: "not-a-valid-repo-url"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})

	_, err := svc.New("badrepo", "feature")
	if err == nil {
		t.Fatal("expected error for invalid repo URL")
	}
}

func TestService_WorktreePath_unknownProject(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.WorktreePath("unknown", "feature")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestService_WorktreePath_notCloned(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.WorktreePath("myapp", "feature")
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestService_WorktreePath_worktreeNotFound(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	// worktree dir does not exist

	_, err := svc.WorktreePath("myapp", "feature")
	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("expected ErrWorktreeNotFound, got %v", err)
	}
}

func TestService_WorktreePath_ok(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := svc.WorktreePath("myapp", "feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != worktreePath {
		t.Errorf("path = %q, want %q", path, worktreePath)
	}
}

func TestService_New_preHookFailure(t *testing.T) {
	git := &mockGit{}
	hook := &mockHook{failOn: semconv.HookPreWorktree}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, git, tc, hook)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "feature")
	if err == nil {
		t.Fatal("expected error from pre-worktree hook")
	}
	if !strings.Contains(err.Error(), "pre-worktree hook") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestService_New_postHookFailure(t *testing.T) {
	git := &mockGit{}
	hook := &mockHook{failOn: semconv.HookPostWorktree}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, git, tc, hook)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "feature")
	if err == nil {
		t.Fatal("expected error from post-worktree hook")
	}
	if !strings.Contains(err.Error(), "post-worktree hook") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestService_NewFrom_preHookFailure(t *testing.T) {
	git := &mockGit{}
	hook := &mockHook{failOn: semconv.HookPreWorktree}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, git, tc, hook)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewFrom("myapp", "feature", "main")
	if err == nil {
		t.Fatal("expected error from pre-worktree hook")
	}
	if !strings.Contains(err.Error(), "pre-worktree hook") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestService_NewFrom_postHookFailure(t *testing.T) {
	git := &mockGit{}
	hook := &mockHook{failOn: semconv.HookPostWorktree}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, git, tc, hook)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewFrom("myapp", "feature", "main")
	if err == nil {
		t.Fatal("expected error from post-worktree hook")
	}
	if !strings.Contains(err.Error(), "post-worktree hook") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestService_List_gitListError(t *testing.T) {
	git := &mockGit{listErr: fmt.Errorf("git error")}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 1})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// git.List error should be silently skipped
	if len(entries) != 0 {
		t.Errorf("expected 0 entries when git.List fails, got %d", len(entries))
	}
}

func TestService_List_invalidRepoSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"badrepo": {Repo: "not-a-valid-url"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{exitCode: 1})
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for invalid repo, got %d", len(entries))
	}
}

func TestService_Delete_forceKillSessionError(t *testing.T) {
	// Use a tmux runner that returns exit 0 (session exists) for has-session
	// but errors on kill-session
	runner := &mockTmuxRunnerKillFails{}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(runner)
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature", Force: true})
	if err == nil {
		t.Fatal("expected error when kill-session fails")
	}
	if !strings.Contains(err.Error(), "killing session") {
		t.Errorf("unexpected error: %v", err)
	}
}

// mockTmuxRunnerKillFails returns success for has-session but error for kill-session.
type mockTmuxRunnerKillFails struct{}

func (m *mockTmuxRunnerKillFails) Run(args ...string) (string, string, int, error) {
	for _, a := range args {
		if a == "kill-session" {
			return "", "error", 1, fmt.Errorf("kill failed")
		}
	}
	// has-session returns exit 0 = session exists
	return "", "", 0, nil
}

// mockTmuxRunnerPerSession simulates tmux with per-session state. Sessions in
// activeSessions are considered running (has-session exits 0). Killed session
// names are collected in killedSessions.
type mockTmuxRunnerPerSession struct {
	activeSessions map[string]bool
	killedSessions []string
}

func (m *mockTmuxRunnerPerSession) Run(args ...string) (string, string, int, error) {
	// Identify command and target from args like ["has-session", "-t", "<name>"]
	// or ["kill-session", "-t", "<name>"].
	if len(args) < 3 {
		return "", "", 1, nil
	}
	cmd, target := args[0], args[2]
	switch cmd {
	case "has-session":
		if m.activeSessions[target] {
			return "", "", 0, nil
		}
		return "", "", 1, nil
	case "kill-session":
		m.killedSessions = append(m.killedSessions, target)
		return "", "", 0, nil
	}
	return "", "", 1, nil
}

func TestService_Delete_Force_KillsBothSessionTypes(t *testing.T) {
	agentSession := semconv.SessionName("", "myapp", "feature")
	shellSession := semconv.ShellSessionName("", "myapp", "feature")

	runner := &mockTmuxRunnerPerSession{
		activeSessions: map[string]bool{
			agentSession: true,
			shellSession: true,
		},
	}
	git := &mockGit{}
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	tc := tmux.NewClient(runner)
	svc := NewService(cfg, git, tc, &mockHook{})

	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(DeleteRequest{Project: "myapp", Branch: "feature", Force: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	killed := make(map[string]bool, len(runner.killedSessions))
	for _, s := range runner.killedSessions {
		killed[s] = true
	}
	if !killed[agentSession] {
		t.Errorf("expected agent session %q to be killed, killed: %v", agentSession, runner.killedSessions)
	}
	if !killed[shellSession] {
		t.Errorf("expected shell session %q to be killed, killed: %v", shellSession, runner.killedSessions)
	}
}

func TestNew_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	mock := &mockGit{}
	hookMock := &mockHook{}
	cfg := &config.Config{
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	cfg.Defaults.ProjectsDir = dir
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, mock, tc, hookMock)

	_, err := svc.New("myapp", "feature")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	hookNames := make([]string, len(hookMock.calls))
	for i, c := range hookMock.calls {
		hookNames[i] = c.name
	}
	if hookNames[0] != semconv.HookPreWorktree {
		t.Errorf("first hook = %q, want %q", hookNames[0], semconv.HookPreWorktree)
	}
	if hookNames[1] != semconv.HookPostWorktree {
		t.Errorf("second hook = %q, want %q", hookNames[1], semconv.HookPostWorktree)
	}
}
