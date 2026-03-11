package project_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/semconv"
)

// mockGitRunner records Clone calls and returns controlled errors.
type mockGitRunner struct {
	calls  []cloneCall
	errors map[string]error // keyed by repo
}

type cloneCall struct{ Repo, Path, Branch string }

func (m *mockGitRunner) Clone(repo, path, branch string) error {
	m.calls = append(m.calls, cloneCall{repo, path, branch})
	if m.errors != nil {
		return m.errors[repo]
	}
	return nil
}

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

func makeConfig(projectsDir string, projects map[string]config.ProjectConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Defaults.ProjectsDir = projectsDir
	cfg.Projects = projects
	return cfg
}

func TestList_SortedByName(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"zebra": {Repo: "git@github.com:user/zebra.git", DefaultBranch: "main"},
		"alpha": {Repo: "git@github.com:user/alpha.git", DefaultBranch: "develop"},
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	entries := svc.List()

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Name != "alpha" || entries[1].Name != "myapp" || entries[2].Name != "zebra" {
		t.Errorf("wrong order: %v", []string{entries[0].Name, entries[1].Name, entries[2].Name})
	}
}

func TestList_PathDerivedFromRepo(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	entries := svc.List()

	want := "/home/user/projects/github.com/user/myapp"
	if entries[0].Path != want {
		t.Errorf("Path = %q, want %q", entries[0].Path, want)
	}
}

func TestList_ClonedAlwaysFalse(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	entries := svc.List()
	if entries[0].Cloned {
		t.Error("List should not check filesystem; Cloned should be false")
	}
}

func TestList_Empty(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	entries := svc.List()
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestShow_ValidProject(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	e, err := svc.Show("myapp")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if e.Name != "myapp" {
		t.Errorf("Name = %q, want %q", e.Name, "myapp")
	}
	if e.Path != "/home/user/projects/github.com/user/myapp" {
		t.Errorf("Path = %q", e.Path)
	}
	// Cloned=false because path doesn't exist on this machine in tests
}

func TestShow_UnknownProject(t *testing.T) {
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	_, err := svc.Show("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestShow_ClonedTrue_WhenPathExists(t *testing.T) {
	dir := t.TempDir()
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	// Create the expected path so Cloned=true
	expectedPath := dir + "/github.com/user/myapp"
	if err := os.MkdirAll(expectedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	e, err := svc.Show("myapp")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if !e.Cloned {
		t.Error("Cloned should be true when path exists")
	}
}

func TestClone_HappyPath(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	svc := project.NewService(cfg, mock, &mockHook{})
	if err := svc.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 git call, got %d", len(mock.calls))
	}
	call := mock.calls[0]
	if call.Repo != "git@github.com:user/myapp.git" {
		t.Errorf("Repo = %q", call.Repo)
	}
	if call.Path != dir+"/github.com/user/myapp" {
		t.Errorf("Path = %q", call.Path)
	}
	if call.Branch != "main" {
		t.Errorf("Branch = %q, want %q", call.Branch, "main")
	}
}

func TestClone_NoBranch(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, mock, &mockHook{})
	if err := svc.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if mock.calls[0].Branch != "" {
		t.Errorf("Branch should be empty when default_branch not set")
	}
}

func TestClone_AlreadyCloned(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	// Pre-create the target path
	targetPath := dir + "/github.com/user/myapp"
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := project.NewService(cfg, mock, &mockHook{})
	err := svc.Clone("myapp")
	if !errors.Is(err, project.ErrAlreadyCloned) {
		t.Fatalf("want ErrAlreadyCloned, got %v", err)
	}
	if len(mock.calls) != 0 {
		t.Error("git should not be called when path already exists")
	}
	var ace *project.AlreadyClonedError
	if !errors.As(err, &ace) {
		t.Fatal("want *AlreadyClonedError")
	}
	if ace.Path != targetPath {
		t.Errorf("AlreadyClonedError.Path = %q, want %q", ace.Path, targetPath)
	}
}

func TestClone_UnknownProject(t *testing.T) {
	cfg := makeConfig("/tmp", map[string]config.ProjectConfig{})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	err := svc.Clone("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestClone_GitFailure(t *testing.T) {
	dir := t.TempDir()
	gitErr := fmt.Errorf("repository not found")
	mock := &mockGitRunner{errors: map[string]error{
		"git@github.com:user/myapp.git": gitErr,
	}}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, mock, &mockHook{})
	err := svc.Clone("myapp")
	if err == nil {
		t.Fatal("expected error on git failure")
	}
}

func TestCloneAll_MixedResults(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{errors: map[string]error{
		"git@github.com:user/fail.git": fmt.Errorf("auth failed"),
	}}
	// Pre-create path for "existing" project
	if err := os.MkdirAll(dir+"/github.com/user/existing", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"alpha":    {Repo: "git@github.com:user/alpha.git"},
		"existing": {Repo: "git@github.com:user/existing.git"},
		"fail":     {Repo: "git@github.com:user/fail.git"},
	})
	svc := project.NewService(cfg, mock, &mockHook{})
	results := svc.CloneAll()

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	// Results must be sorted: alpha, existing, fail
	if results[0].Name != "alpha" || results[0].Err != nil {
		t.Errorf("alpha: got name=%q err=%v", results[0].Name, results[0].Err)
	}
	if results[1].Name != "existing" || !errors.Is(results[1].Err, project.ErrAlreadyCloned) {
		t.Errorf("existing: got name=%q err=%v", results[1].Name, results[1].Err)
	}
	if results[2].Name != "fail" || results[2].Err == nil {
		t.Errorf("fail: got name=%q err=%v", results[2].Name, results[2].Err)
	}
	// Only "alpha" and "fail" should have triggered git calls
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 git calls, got %d", len(mock.calls))
	}
}

func TestCloneAll_Empty(t *testing.T) {
	cfg := makeConfig("/tmp", map[string]config.ProjectConfig{})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	results := svc.CloneAll()
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

// ── AlreadyClonedError ────────────────────────────────────────────────────────

func TestAlreadyClonedError_ErrorString(t *testing.T) {
	err := &project.AlreadyClonedError{Path: "/some/path"}
	want := "/some/path already exists, skipping"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestAlreadyClonedError_Unwrap(t *testing.T) {
	err := &project.AlreadyClonedError{Path: "/some/path"}
	if err.Unwrap() != project.ErrAlreadyCloned {
		t.Errorf("Unwrap() = %v, want ErrAlreadyCloned", err.Unwrap())
	}
}

// ── NewRealGitRunner ──────────────────────────────────────────────────────────

func TestNewRealGitRunner_NotNil(t *testing.T) {
	r := project.NewRealGitRunner()
	if r == nil {
		t.Error("NewRealGitRunner() = nil, want non-nil")
	}
}

// ── Show with bad repo URL ────────────────────────────────────────────────────

func TestShow_BadRepoURL(t *testing.T) {
	// An https URL with no host triggers RepoPath's "no host" error.
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"badrepo": {Repo: "https:///no-host/repo.git"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	_, err := svc.Show("badrepo")
	if err == nil {
		t.Fatal("Show() with bad repo URL = nil, want error")
	}
}

// ── Clone with bad repo URL ───────────────────────────────────────────────────

func TestClone_BadRepoURL(t *testing.T) {
	// An https URL with no host triggers RepoPath's "no host" error.
	cfg := makeConfig("/home/user/projects", map[string]config.ProjectConfig{
		"badrepo": {Repo: "https:///no-host/repo.git"},
	})
	svc := project.NewService(cfg, &mockGitRunner{}, &mockHook{})
	err := svc.Clone("badrepo")
	if err == nil {
		t.Fatal("Clone() with bad repo URL = nil, want error")
	}
}

// ── Hook integration ──────────────────────────────────────────────────────────

func TestClone_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	hookMock := &mockHook{}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	svc := project.NewService(cfg, mock, hookMock)
	if err := svc.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if len(hookMock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookMock.calls))
	}
	if hookMock.calls[0].name != semconv.HookPreClone {
		t.Errorf("first hook = %q, want %q", hookMock.calls[0].name, semconv.HookPreClone)
	}
	if hookMock.calls[1].name != semconv.HookPostClone {
		t.Errorf("second hook = %q, want %q", hookMock.calls[1].name, semconv.HookPostClone)
	}
	if hookMock.calls[0].attrs[semconv.HookAttrProject] != "myapp" {
		t.Errorf("project attr = %q", hookMock.calls[0].attrs[semconv.HookAttrProject])
	}
}

func TestClone_PreHookFailure_StopsClone(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	hookMock := &mockHook{failOn: semconv.HookPreClone}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, mock, hookMock)
	err := svc.Clone("myapp")
	if err == nil {
		t.Error("expected error when pre-clone hook fails")
	}
	if len(mock.calls) != 0 {
		t.Error("git clone should not be called when pre-clone hook fails")
	}
}
