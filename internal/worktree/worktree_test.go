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

func TestService_New_branchNotFound_fallsBackToAddNew(t *testing.T) {
	git := &mockGit{addErr: fmt.Errorf("invalid reference")}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "new-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addNewCalled {
		t.Error("expected AddNewBranch to be called on Add failure")
	}
}

func TestService_New_withFromBranch(t *testing.T) {
	git := &mockGit{}
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
	if git.addNewFromStartPoint != "feature-auth" {
		t.Errorf("start point = %q, want %q", git.addNewFromStartPoint, "feature-auth")
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/my-feature"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
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

func TestService_NewFrom_withEnvTemplate(t *testing.T) {
	git := &mockGitCreatesDir{}
	tmpDir := t.TempDir()

	templatePath := filepath.Join(tmpDir, "env.template")
	if err := os.WriteFile(templatePath, []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main", EnvTemplate: templatePath},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, git, tc, &mockHook{})

	cloneDir := filepath.Join(tmpDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewFrom("myapp", "feature", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.EnvWritten {
		t.Error("expected EnvWritten=true when config template exists")
	}
}

func TestService_New_bothAddsFail(t *testing.T) {
	git := &mockGit{
		addErr:    fmt.Errorf("invalid reference"),
		addNewErr: fmt.Errorf("already exists"),
	}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.New("myapp", "new-feature")
	if err == nil {
		t.Fatal("expected error when both Add and AddNewBranch fail")
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

// mockGitCreatesDir is a WorktreeRunner that creates the worktree directory on Add.
type mockGitCreatesDir struct{ addErr error }

func (m *mockGitCreatesDir) Add(cloneDir, worktreePath, branch string) error {
	if m.addErr != nil {
		return m.addErr
	}
	return os.MkdirAll(worktreePath, 0o755)
}
func (m *mockGitCreatesDir) AddNewBranch(cloneDir, worktreePath, branch string) error {
	return os.MkdirAll(worktreePath, 0o755)
}
func (m *mockGitCreatesDir) AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error {
	return os.MkdirAll(worktreePath, 0o755)
}
func (m *mockGitCreatesDir) Remove(cloneDir, worktreePath string) error { return nil }
func (m *mockGitCreatesDir) List(cloneDir string) ([]WorktreeInfo, error) {
	return nil, nil
}

func TestService_New_withEnvTemplate(t *testing.T) {
	git := &mockGitCreatesDir{}
	var svc *Service
	_, tmpDir := makeService(t, git, &mockTmuxRunner{})
	cloneDir := cloneDirPath(tmpDir)
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// We need to write the template to where git.Add will place the worktree
	// The mock creates the dir on Add, so we write the template after Add would create it.
	// We set up a config-level env_template instead (simpler to pre-create):
	templatePath := filepath.Join(tmpDir, "env.template")
	if err := os.WriteFile(templatePath, []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override cfg to use config-level template
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main", EnvTemplate: templatePath},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc = NewService(cfg, git, tc, &mockHook{})

	result, err := svc.New("myapp", "feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.EnvWritten {
		t.Error("expected EnvWritten=true when config template exists")
	}
}

func TestService_Env_unknownProject(t *testing.T) {
	svc, _ := makeService(t, &mockGit{}, &mockTmuxRunner{})
	_, err := svc.Env("unknown", "feature", false)
	if err == nil {
		t.Fatal("expected error for unconfigured project")
	}
}

func TestService_Env_badRepoURL(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"badrepo": {Repo: ":::invalid:::"},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})

	_, err := svc.Env("badrepo", "feature", false)
	if err == nil {
		t.Fatal("expected error for project with invalid repo URL")
	}
}

func TestService_Env_worktreeNotFound(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	// Create clone dir but NOT the worktree dir
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Env("myapp", "feature", false)
	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("expected ErrWorktreeNotFound, got %v", err)
	}
}

func TestService_Env_templateReadError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a worktree path
	worktreePath := filepath.Join(tmpDir, "github.com", "user", "myapp__worktrees", "feature")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Point config template at a directory (unreadable as file)
	badTemplatePath := filepath.Join(tmpDir, "badtemplate")
	if err := os.MkdirAll(badTemplatePath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {
				Repo:        "git@github.com:user/myapp.git",
				EnvTemplate: badTemplatePath,
			},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})

	_, err := svc.Env("myapp", "feature", false)
	if err == nil {
		t.Fatal("expected error when config template is a directory")
	}
}

func TestService_Env_invalidTemplate(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a template with invalid syntax
	if err := os.WriteFile(filepath.Join(worktreePath, ".env.template"), []byte("PORT={{ .Invalid }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// envtemplate.Process won't error on .Invalid (it's a valid template action);
	// use a syntax error instead
	if err := os.WriteFile(filepath.Join(worktreePath, ".env.template"), []byte("{{ invalid template syntax {{{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Env("myapp", "feature", false)
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
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

func TestService_Env_noTemplate(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Env("myapp", "feature", false)
	if err == nil {
		t.Fatal("expected error when no template found")
	}
}

func TestService_Env_repoLocalTemplate(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a repo-local .env.template
	if err := os.WriteFile(filepath.Join(worktreePath, ".env.template"), []byte("PORT={{ port \"web\" }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Env("myapp", "feature", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != "repo-local" {
		t.Errorf("expected source repo-local, got %q", result.Source)
	}
	// .env file should be written
	if _, statErr := os.Stat(filepath.Join(worktreePath, ".env")); statErr != nil {
		t.Error("expected .env to be written")
	}
}

func TestService_Env_dryRun(t *testing.T) {
	svc, tmpDir := makeService(t, &mockGit{}, &mockTmuxRunner{})
	worktreePath := cloneDirPath(tmpDir) + "__worktrees/feature"
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".env.template"), []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Env("myapp", "feature", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DryRun != true {
		t.Error("expected DryRun=true")
	}
	// .env file should NOT be written
	if _, statErr := os.Stat(filepath.Join(worktreePath, ".env")); statErr == nil {
		t.Error("expected .env NOT to be written in dry-run mode")
	}
}

func TestService_Env_configTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	// Write template to a separate file
	templatePath := filepath.Join(tmpDir, "my.env.template")
	if err := os.WriteFile(templatePath, []byte("DB_PORT={{ port \"db\" }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: tmpDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {
				Repo:        "git@github.com:user/myapp.git",
				EnvTemplate: templatePath,
			},
		},
	}
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, &mockGit{}, tc, &mockHook{})

	worktreePath := filepath.Join(tmpDir, "github.com", "user", "myapp__worktrees", "feature")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Env("myapp", "feature", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != templatePath {
		t.Errorf("expected source %q, got %q", templatePath, result.Source)
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
