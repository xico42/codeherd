package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := config.Load(filepath.Join(dir, "config.toml"), "")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("Load() = nil, want non-nil")
	}
}

func TestLoad_parsesProfileMetaFields(t *testing.T) {
	// With profiles_enabled=false the TOML is parsed into *Config verbatim,
	// so this is where we assert the three profile-meta fields round-trip.
	// (Profile-mode resolution is covered in profile_test.go.)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `[defaults]
profiles_enabled = false
profiles_dir = "/custom/profiles"
main_profile = "personal"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Defaults.ProfilesEnabled {
		t.Error("ProfilesEnabled = true, want false")
	}
	if cfg.Defaults.ProfilesDir != "/custom/profiles" {
		t.Errorf("ProfilesDir = %q, want /custom/profiles", cfg.Defaults.ProfilesDir)
	}
	if cfg.Defaults.MainProfile != "personal" {
		t.Errorf("MainProfile = %q, want personal", cfg.Defaults.MainProfile)
	}
}

func TestLoad_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _, err := config.Load(path, "")
	if err == nil {
		t.Fatal("Load() error = nil, want error for corrupt file")
	}
}

func TestConfig_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml") // Save must create subdirectory

	cfg, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Defaults.ProjectsDir = "/tmp/myprojects"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg2, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if cfg2.Defaults.ProjectsDir != "/tmp/myprojects" {
		t.Errorf("ProjectsDir = %q, want %q", cfg2.Defaults.ProjectsDir, "/tmp/myprojects")
	}
}

func TestLoad_Projects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[projects.myapp]
repo = "git@github.com:user/myapp.git"
default_branch = "main"

[projects.api]
repo = "git@github.com:user/api.git"
default_branch = "develop"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("len(Projects) = %d, want 2", len(cfg.Projects))
	}
	myapp := cfg.Projects["myapp"]
	if myapp.Repo != "git@github.com:user/myapp.git" {
		t.Errorf("myapp.Repo = %q, want expected", myapp.Repo)
	}
	if myapp.DefaultBranch != "main" {
		t.Errorf("myapp.DefaultBranch = %q, want %q", myapp.DefaultBranch, "main")
	}
	api := cfg.Projects["api"]
	if api.DefaultBranch != "develop" {
		t.Errorf("api.DefaultBranch = %q, want %q", api.DefaultBranch, "develop")
	}
}

func TestLoad_ProjectsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := config.Load(filepath.Join(dir, "config.toml"), "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Projects != nil {
		t.Errorf("Projects = %v, want nil for missing file", cfg.Projects)
	}
}

func TestLoad_ProjectsDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[defaults]
projects_dir = "~/projects"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	want := home + "/projects"
	if cfg.Defaults.ProjectsDir != want {
		t.Errorf("ProjectsDir = %q, want %q", cfg.Defaults.ProjectsDir, want)
	}
}

func TestLoad_ProjectsDirDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := config.Load(filepath.Join(dir, "config.toml"), "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	want := home + "/projects"
	if cfg.Defaults.ProjectsDir != want {
		t.Errorf("ProjectsDir = %q, want %q (default)", cfg.Defaults.ProjectsDir, want)
	}
}

// TestLoad_ReadError exercises the non-ErrNotExist read error branch by
// making the config path a directory.
func TestLoad_ReadError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(cfgPath, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	_, _, err := config.Load(cfgPath, "")
	if err == nil {
		t.Fatal("Load() on directory = nil, want error")
	}
}

func TestConfig_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg, _, _ := config.Load(path, "")
	if cfg.Path() != path {
		t.Errorf("Path() = %q, want %q", cfg.Path(), path)
	}
}

func TestLoad_HooksAndFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[projects.myapp]
repo = "git@github.com:user/myapp.git"
files = ["CLAUDE.md", "~/.config/prompts/safety.md:RULES.md"]

[projects.myapp.hooks]
pre-clone = "echo preparing"
post-clone = "make deps"
pre-worktree = "echo wt"
post-worktree = "npm install"
pre-copy = "echo copy"
post-copy = "chmod 600 .env"
pre-template = "echo tmpl"
post-template = "echo done"
pre-session = "docker compose up -d"
post-session = "curl http://example.com"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, _, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	p := cfg.Projects["myapp"]

	if len(p.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(p.Files))
	}
	if p.Files[0] != "CLAUDE.md" {
		t.Errorf("Files[0] = %q", p.Files[0])
	}

	if p.Hooks.PreClone != "echo preparing" {
		t.Errorf("PreClone = %q", p.Hooks.PreClone)
	}
	if p.Hooks.PostClone != "make deps" {
		t.Errorf("PostClone = %q", p.Hooks.PostClone)
	}
	if p.Hooks.PreWorktree != "echo wt" {
		t.Errorf("PreWorktree = %q", p.Hooks.PreWorktree)
	}
	if p.Hooks.PostWorktree != "npm install" {
		t.Errorf("PostWorktree = %q", p.Hooks.PostWorktree)
	}
	if p.Hooks.PreCopy != "echo copy" {
		t.Errorf("PreCopy = %q", p.Hooks.PreCopy)
	}
	if p.Hooks.PostCopy != "chmod 600 .env" {
		t.Errorf("PostCopy = %q", p.Hooks.PostCopy)
	}
	if p.Hooks.PreTemplate != "echo tmpl" {
		t.Errorf("PreTemplate = %q", p.Hooks.PreTemplate)
	}
	if p.Hooks.PostTemplate != "echo done" {
		t.Errorf("PostTemplate = %q", p.Hooks.PostTemplate)
	}
	if p.Hooks.PreSession != "docker compose up -d" {
		t.Errorf("PreSession = %q", p.Hooks.PreSession)
	}
	if p.Hooks.PostSession != "curl http://example.com" {
		t.Errorf("PostSession = %q", p.Hooks.PostSession)
	}
}
