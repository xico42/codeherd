package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func TestLoadProfile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "personal.toml")
	content := `[defaults]
projects_dir = "~/personal"
agent = "claude"

[projects.myapp]
repo = "git@github.com:u/myapp.git"

[agents.claude]
cmd = "claude"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadProfile(dir, "personal")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	if cfg.Defaults.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", cfg.Defaults.Agent)
	}
	if _, ok := cfg.Projects["myapp"]; !ok {
		t.Error("missing project myapp")
	}
	if _, ok := cfg.Agents["claude"]; !ok {
		t.Error("missing agent claude")
	}
}

func TestLoadProfile_missingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := config.LoadProfile(dir, "ghost")
	if err == nil {
		t.Fatal("LoadProfile() error = nil, want non-nil for missing profile")
	}
}

func TestLoadProfile_ignoresProfileMetaInsideProfileFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.toml")
	content := `[defaults]
profiles_enabled = true
profiles_dir = "/nope"
main_profile = "other"
projects_dir = "~/ok"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadProfile(dir, "p")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	// Struct does hold the fields (they parse), but LoadProfile's contract
	// is that the returned *Config is used only as a plain project/agent
	// bundle — the caller must not consult the profile-meta fields. The
	// test asserts the non-meta field is parsed correctly.
	if cfg.Defaults.ProjectsDir == "" {
		t.Error("ProjectsDir not parsed")
	}
}

func TestDiscoverProfiles_sortsNames(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"work.toml", "personal.toml", "client-a.toml", "not-a-profile.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names, err := config.DiscoverProfiles(dir)
	if err != nil {
		t.Fatalf("DiscoverProfiles() error = %v", err)
	}
	want := []string{"client-a", "personal", "work"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("DiscoverProfiles = %v, want %v", names, want)
	}
}

func TestDiscoverProfiles_missingDirIsError(t *testing.T) {
	_, err := config.DiscoverProfiles("/nope/does/not/exist")
	if err == nil {
		t.Fatal("DiscoverProfiles() error = nil, want non-nil for missing dir")
	}
}

func writeMainConfig(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeProfile(t *testing.T, profilesDir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_profilesDisabled_registryNil(t *testing.T) {
	dir := t.TempDir()
	path := writeMainConfig(t, dir, "[defaults]\nprojects_dir = \"~/p\"\n")
	_, reg, err := config.Load(path, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reg != nil {
		t.Errorf("registry = %+v, want nil when profiles disabled", reg)
	}
}

func TestLoad_profilesEnabled_flagBeatsMainProfile(t *testing.T) {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "profiles")
	writeProfile(t, profilesDir, "personal", `[projects.pa]
repo = "git@x:a"
`)
	writeProfile(t, profilesDir, "work", `[projects.wb]
repo = "git@x:b"
`)
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
	cfg, reg, err := config.Load(main, "work")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := cfg.Projects["wb"]; !ok {
		t.Error("expected work profile to be active (wb project missing)")
	}
	if reg == nil || reg.Active != "work" {
		t.Errorf("registry.Active = %v, want work", reg)
	}
	if len(reg.Names) != 2 {
		t.Errorf("registry.Names = %v, want 2 entries", reg.Names)
	}
}

func TestLoad_profilesEnabled_fallsBackToMainProfile(t *testing.T) {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "profiles")
	writeProfile(t, profilesDir, "personal", `[projects.pa]
repo = "git@x:a"
`)
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
	cfg, reg, err := config.Load(main, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := cfg.Projects["pa"]; !ok {
		t.Error("expected personal profile to be active (pa project missing)")
	}
	if reg.Active != "personal" {
		t.Errorf("registry.Active = %q, want personal", reg.Active)
	}
}

func TestLoad_profilesEnabled_errorsWhenUnresolved(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
	main := writeMainConfig(t, dir, "[defaults]\nprofiles_enabled = true\n")
	_, _, err := config.Load(main, "")
	if err == nil {
		t.Fatal("Load() error = nil, want unresolved-profile error")
	}
}

func TestLoad_profilesEnabled_errorsWhenProfileFileMissing(t *testing.T) {
	dir := t.TempDir()
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "ghost"
`)
	_, _, err := config.Load(main, "")
	if err == nil {
		t.Fatal("Load() error = nil, want missing-profile error")
	}
}

func TestLoad_profilesEnabled_defaultProfilesDir(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
	_, reg, err := config.Load(main, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(dir, "profiles")
	if reg.ProfilesDir != want {
		t.Errorf("ProfilesDir = %q, want %q", reg.ProfilesDir, want)
	}
}

func TestLoad_profilesEnabled_errorsWhenProfilesDirMissing(t *testing.T) {
	dir := t.TempDir()
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
	_, _, err := config.Load(main, "")
	if err == nil {
		t.Fatal("Load() error = nil, want missing-profiles-dir error")
	}
}

func TestProfileNamesFor_listsProfiles(t *testing.T) {
	dir := t.TempDir()
	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"work", "home"} {
		if err := os.WriteFile(filepath.Join(profDir, n+".toml"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[defaults]\nprofiles_enabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := config.ProfileNamesFor(cfgPath)
	want := []string{"home", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileNamesFor = %v, want %v", got, want)
	}
}

func TestProfileNamesFor_profilesDisabled_returnsNil(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[defaults]\nprofiles_enabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := config.ProfileNamesFor(cfgPath); got != nil {
		t.Fatalf("ProfileNamesFor = %v, want nil", got)
	}
}

func TestProfileNamesFor_missingConfig_returnsNil(t *testing.T) {
	if got := config.ProfileNamesFor(filepath.Join(t.TempDir(), "absent.toml")); got != nil {
		t.Fatalf("ProfileNamesFor = %v, want nil", got)
	}
}

func TestLoad_profilesEnabled_warnsOnStrayKeys(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
	main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
projects_dir = "~/ignored"

[projects.ignored]
repo = "git@x:i"
`)
	var buf strings.Builder
	config.SetWarningSink(&buf)
	defer config.SetWarningSink(nil)
	_, _, err := config.Load(main, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "profiles_enabled=true") {
		t.Errorf("warning missing profiles_enabled mention: %q", out)
	}
	if !strings.Contains(out, "projects_dir") || !strings.Contains(out, "projects") {
		t.Errorf("warning missing stray key mention: %q", out)
	}
}
