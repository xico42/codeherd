package herd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// projectHerd builds a Herd (profiles off) for the project-domain tests,
// returning the Herd and the fakeGit its Clone calls land on.
func projectHerd(t *testing.T, projectsDir string, projects map[string]config.ProjectConfig) (*Herd, *fakeGit) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Defaults.ProjectsDir = projectsDir
	cfg.Projects = projects
	g := &fakeGit{}
	return New(cfg, nil, Deps{Git: g}), g
}

func TestList_SortedByName(t *testing.T) {
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"zebra": {Repo: "git@github.com:user/zebra.git", DefaultBranch: "main"},
		"alpha": {Repo: "git@github.com:user/alpha.git", DefaultBranch: "develop"},
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	entries := h.Projects()

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Name != "alpha" || entries[1].Name != "myapp" || entries[2].Name != "zebra" {
		t.Errorf("wrong order: %v", []string{entries[0].Name, entries[1].Name, entries[2].Name})
	}
}

func TestList_PathDerivedFromRepo(t *testing.T) {
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	entries := h.Projects()

	want := "/home/user/projects/github.com/user/myapp"
	if entries[0].Path != want {
		t.Errorf("Path = %q, want %q", entries[0].Path, want)
	}
}

func TestList_ClonedAlwaysFalse(t *testing.T) {
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	entries := h.Projects()
	if entries[0].Cloned {
		t.Error("Projects should not check filesystem; Cloned should be false")
	}
}

func TestList_Empty(t *testing.T) {
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{})
	entries := h.Projects()
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestShow_ValidProject(t *testing.T) {
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	e, err := h.Project("myapp")
	if err != nil {
		t.Fatalf("Project() error = %v", err)
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
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{})
	_, err := h.Project("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestShow_ClonedTrue_WhenPathExists(t *testing.T) {
	dir := t.TempDir()
	h, _ := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	// Create the expected path so Cloned=true
	expectedPath := dir + "/github.com/user/myapp"
	if err := os.MkdirAll(expectedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	e, err := h.Project("myapp")
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if !e.Cloned {
		t.Error("Cloned should be true when path exists")
	}
}

func TestClone_HappyPath(t *testing.T) {
	dir := t.TempDir()
	h, g := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	if err := h.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	want := dir + "/github.com/user/myapp"
	if !g.called("Clone", "git@github.com:user/myapp.git", want, "main") {
		t.Errorf("clone did not target %s with branch main; calls=%v", want, g.Calls)
	}
}

func TestClone_NoBranch(t *testing.T) {
	dir := t.TempDir()
	h, g := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	if err := h.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	// The recorded call ends with an empty branch field.
	if len(g.Calls) != 1 || g.Calls[0] != "Clone git@github.com:user/myapp.git "+dir+"/github.com/user/myapp " {
		t.Errorf("Branch should be empty when default_branch not set; calls=%v", g.Calls)
	}
}

func TestClone_AlreadyCloned(t *testing.T) {
	dir := t.TempDir()
	h, g := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	// Pre-create the target path
	targetPath := dir + "/github.com/user/myapp"
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	err := h.Clone("myapp")
	if !errors.Is(err, ErrAlreadyCloned) {
		t.Fatalf("want ErrAlreadyCloned, got %v", err)
	}
	if g.called("Clone") {
		t.Error("git should not be called when path already exists")
	}
	var ace *AlreadyClonedError
	if !errors.As(err, &ace) {
		t.Fatal("want *AlreadyClonedError")
	}
	if ace.Path != targetPath {
		t.Errorf("AlreadyClonedError.Path = %q, want %q", ace.Path, targetPath)
	}
}

func TestClone_UnknownProject(t *testing.T) {
	h, _ := projectHerd(t, "/tmp", map[string]config.ProjectConfig{})
	err := h.Clone("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
}

func TestClone_GitFailure(t *testing.T) {
	dir := t.TempDir()
	h, g := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	g.CloneFn = func(repo, path, branch string) error {
		return errors.New("repository not found")
	}
	err := h.Clone("myapp")
	if err == nil {
		t.Fatal("expected error on git failure")
	}
}

// ── AlreadyClonedError ────────────────────────────────────────────────────────

func TestAlreadyClonedError_ErrorString(t *testing.T) {
	err := &AlreadyClonedError{Path: "/some/path"}
	want := "/some/path already exists, skipping"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestAlreadyClonedError_Unwrap(t *testing.T) {
	err := &AlreadyClonedError{Path: "/some/path"}
	if err.Unwrap() != ErrAlreadyCloned {
		t.Errorf("Unwrap() = %v, want ErrAlreadyCloned", err.Unwrap())
	}
}

// ── Show with bad repo URL ────────────────────────────────────────────────────

func TestShow_BadRepoURL(t *testing.T) {
	// An https URL with no host triggers RepoPath's "no host" error.
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"badrepo": {Repo: "https:///no-host/repo.git"},
	})
	_, err := h.Project("badrepo")
	if err == nil {
		t.Fatal("Project() with bad repo URL = nil, want error")
	}
}

// ── Clone with bad repo URL ───────────────────────────────────────────────────

func TestClone_BadRepoURL(t *testing.T) {
	// An https URL with no host triggers RepoPath's "no host" error.
	h, _ := projectHerd(t, "/home/user/projects", map[string]config.ProjectConfig{
		"badrepo": {Repo: "https:///no-host/repo.git"},
	})
	err := h.Clone("badrepo")
	if err == nil {
		t.Fatal("Clone() with bad repo URL = nil, want error")
	}
}

// ── Hook integration ──────────────────────────────────────────────────────────

func TestClone_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	h, _ := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	hookMock := &mockHook{}
	withHook(h, hookMock)
	if err := h.Clone("myapp"); err != nil {
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
	h, g := projectHerd(t, dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	hookMock := &mockHook{failOn: semconv.HookPreClone}
	withHook(h, hookMock)
	err := h.Clone("myapp")
	if err == nil {
		t.Error("expected error when pre-clone hook fails")
	}
	if g.called("Clone") {
		t.Error("git clone should not be called when pre-clone hook fails")
	}
}

// Clone is reachable through the same Herd the rest of the domain uses, and it
// clones through the active profile's config.
func TestClone_underProfile_usesProfileConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "trunk"},
		},
	}
	g := &fakeGit{}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Git: g})

	if err := h.Clone("myapp"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	want := filepath.Join(dir, "github.com", "user", "myapp")
	if !g.called("Clone", "git@github.com:user/myapp.git", want, "trunk") {
		t.Errorf("clone did not target %s; calls=%v", want, g.Calls)
	}
}
