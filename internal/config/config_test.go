package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

// TestLoad_EmptyPath verifies that Load("") uses the XDG default path
// and returns non-nil defaults (file may or may not exist).
func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("Load(\"\") = nil, want non-nil")
	}
	// Default image is always set.
	if cfg.Defaults.Image == "" {
		t.Error("Load(\"\") Defaults.Image is empty, want default")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.Defaults.Image != "ubuntu-24-04-x64" {
		t.Errorf("Defaults.Image = %q, want %q", cfg.Defaults.Image, "ubuntu-24-04-x64")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const content = `
[defaults]
token = "mytoken"
region = "nyc3"
size = "s-2vcpu-4gb"

[profiles.large]
size = "s-4vcpu-8gb"
region = "sfo3"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Defaults.Token != "mytoken" {
		t.Errorf("Token = %q, want %q", cfg.Defaults.Token, "mytoken")
	}
	if cfg.Defaults.Region != "nyc3" {
		t.Errorf("Region = %q, want %q", cfg.Defaults.Region, "nyc3")
	}
}

func TestLoad_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error for corrupt file")
	}
}

func TestConfig_Profile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const content = `
[profiles.large]
size = "s-4vcpu-8gb"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name    string
		profile string
		wantErr bool
	}{
		{"existing", "large", false},
		{"missing", "nonexistent", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfg.Profile(tt.profile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Profile(%q) error = %v, wantErr %v", tt.profile, err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml") // Save must create subdirectory

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Defaults.Token = "saved-token"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg2, err := config.Load(path)
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if cfg2.Defaults.Token != "saved-token" {
		t.Errorf("Token = %q, want %q", cfg2.Defaults.Token, "saved-token")
	}
}

func TestConfig_ApplyEnv(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	t.Setenv("DIGITALOCEAN_TOKEN", "env-token")
	t.Setenv("CODEHERD_REGION", "ams3")
	t.Setenv("CODEHERD_SIZE", "s-4vcpu-8gb")
	t.Setenv("CODEHERD_IMAGE", "ubuntu-22-04-x64")
	t.Setenv("TAILSCALE_AUTH_KEY", "tskey-env")
	cfg.ApplyEnv()
	if cfg.Defaults.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Defaults.Token)
	}
	if cfg.Defaults.Region != "ams3" {
		t.Errorf("Region = %q, want ams3", cfg.Defaults.Region)
	}
	if cfg.Defaults.Size != "s-4vcpu-8gb" {
		t.Errorf("Size = %q, want s-4vcpu-8gb", cfg.Defaults.Size)
	}
	if cfg.Defaults.Image != "ubuntu-22-04-x64" {
		t.Errorf("Image = %q, want ubuntu-22-04-x64", cfg.Defaults.Image)
	}
	if cfg.Defaults.TailscaleAuthKey != "tskey-env" {
		t.Errorf("TailscaleAuthKey = %q, want tskey-env", cfg.Defaults.TailscaleAuthKey)
	}
}

func TestConfig_ApplyFlags(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.ApplyFlags("flag-token")
	if cfg.Defaults.Token != "flag-token" {
		t.Errorf("Token = %q, want flag-token", cfg.Defaults.Token)
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
	cfg, err := config.Load(path)
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
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
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
	cfg, err := config.Load(path)
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
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
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
	// Make a directory where the config file would be.
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(cfgPath, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	_, err := config.Load(cfgPath)
	if err == nil {
		t.Fatal("Load() on directory = nil, want error")
	}
}

// TestLoad_DefaultImage exercises the branch where a loaded config has an
// empty image (so the default must be applied after parsing).
func TestLoad_DefaultImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// Write a valid config with no image field.
	const content = `
[defaults]
token = "tok"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Defaults.Image != "ubuntu-24-04-x64" {
		t.Errorf("Image = %q, want default %q", cfg.Defaults.Image, "ubuntu-24-04-x64")
	}
}

func TestConfig_Validate_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Empty config is valid — nothing is required at file level.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on empty config = %v, want nil", err)
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"do_pat_v1_abcdefghijklmnopqrstuvwxyz1234", "do_pat_v1****1234"},
		{"short", "****"},
		{"exactly12ch!", "****"},
		{"", "****"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := config.Redact(tt.input)
			if got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConfig_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg, _ := config.Load(path)
	if cfg.Path() != path {
		t.Errorf("Path() = %q, want %q", cfg.Path(), path)
	}
}

func TestIsValidKeyPath(t *testing.T) {
	tests := []struct {
		path  string
		valid bool
	}{
		{"defaults.token", true},
		{"defaults.region", true},
		{"defaults.ssh_key_id", true},
		{"defaults.foo", false},
		{"profiles.heavy.size", true},
		{"profiles.heavy.region", true},
		{"profiles.heavy.unknown", false},
		{"projects.myapp.repo", true},
		{"projects.myapp.default_branch", true},
		{"projects.myapp.nope", false},
		{"unknown.key", false},
		{"defaults", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := config.IsValidKeyPath(tt.path)
			if got != tt.valid {
				t.Errorf("IsValidKeyPath(%q) = %v, want %v", tt.path, got, tt.valid)
			}
		})
	}
}

func TestConfig_SetKey_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// Write initial config with a comment that must survive the edit.
	content := "# my important comment\n[defaults]\nregion = \"nyc3\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)

	if err := cfg.SetKey("defaults.region", "sfo3"); err != nil {
		t.Fatalf("SetKey() error = %v", err)
	}

	// Value updated
	cfg2, _ := config.Load(path)
	if cfg2.Defaults.Region != "sfo3" {
		t.Errorf("Region = %q, want %q", cfg2.Defaults.Region, "sfo3")
	}

	// Comment preserved
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# my important comment") {
		t.Error("SetKey() clobbered the comment in the file")
	}
}

func TestConfig_SetKey_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(filepath.Join(dir, "config.toml"))
	err := cfg.SetKey("defaults.nonexistent", "val")
	if err == nil {
		t.Fatal("SetKey() with unknown key = nil, want error")
	}
}

func TestConfig_SetKey_CreatesNewProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg, _ := config.Load(path)

	if err := cfg.SetKey("profiles.heavy.size", "s-8vcpu-16gb"); err != nil {
		t.Fatalf("SetKey() error = %v", err)
	}

	cfg2, _ := config.Load(path)
	p, err := cfg2.Profile("heavy")
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if p.Size != "s-8vcpu-16gb" {
		t.Errorf("Size = %q, want %q", p.Size, "s-8vcpu-16gb")
	}
}

func TestConfig_DeleteSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[defaults]\nregion = \"nyc3\"\n\n[profiles.heavy]\nsize = \"s-8vcpu-16gb\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, _ := config.Load(path)

	if err := cfg.DeleteSection("profiles.heavy"); err != nil {
		t.Fatalf("DeleteSection() error = %v", err)
	}

	cfg2, _ := config.Load(path)
	if _, err := cfg2.Profile("heavy"); err == nil {
		t.Error("Profile heavy still exists after DeleteSection")
	}
	// defaults must be untouched
	if cfg2.Defaults.Region != "nyc3" {
		t.Errorf("Region = %q after delete, want nyc3", cfg2.Defaults.Region)
	}
}

func TestConfig_DeleteSection_NonExistent(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(filepath.Join(dir, "config.toml"))
	// Must not error on missing section.
	if err := cfg.DeleteSection("profiles.ghost"); err != nil {
		t.Errorf("DeleteSection non-existent = %v, want nil", err)
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

	cfg, err := config.Load(path)
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

func TestLoad_GitIdentityFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[defaults]
git_identity_file = "~/.ssh/id_ed25519"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	want := home + "/.ssh/id_ed25519"
	if cfg.Defaults.GitIdentityFile != want {
		t.Errorf("GitIdentityFile = %q, want %q", cfg.Defaults.GitIdentityFile, want)
	}
}
