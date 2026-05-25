//go:build integration

package cmd_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func setupProfilesTree(t *testing.T) (mainPath string) {
	t.Helper()
	root := t.TempDir()
	personalProjects := filepath.Join(root, "personal-projects")
	workProjects := filepath.Join(root, "work-projects")

	profilesDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	personal := `[defaults]
projects_dir = "` + personalProjects + `"
agent = "test-agent"

[agents.test-agent]
cmd = "sleep"
args = ["300"]

[projects.myapp]
repo = "git@github.com:u/myapp.git"
`
	work := `[defaults]
projects_dir = "` + workProjects + `"
agent = "test-agent"

[agents.test-agent]
cmd = "sleep"
args = ["300"]

[projects.other]
repo = "git@github.com:u/other.git"
`
	for name, body := range map[string]string{"personal.toml": personal, "work.toml": work} {
		if err := os.WriteFile(filepath.Join(profilesDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := `[defaults]
profiles_enabled = true
profiles_dir = "` + profilesDir + `"
main_profile = "personal"
`
	mainPath = filepath.Join(root, "config.toml")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	return mainPath
}

// captureStdout redirects os.Stdout during f and returns what was written.
// Not safe for t.Parallel — mutates the package-global os.Stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	w.Close()
	os.Stdout = orig
	return <-done
}

func TestProfiles_listProjectScopedToActiveProfile(t *testing.T) {
	main := setupProfilesTree(t)

	out := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
	})
	if !strings.Contains(out, "myapp") || strings.Contains(out, "other") {
		t.Errorf("personal list project out = %q, want myapp only", out)
	}

	out = captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "--profile", "work", "list", "project")
	})
	if !strings.Contains(out, "other") || strings.Contains(out, "myapp") {
		t.Errorf("work list project out = %q, want other only", out)
	}
}

func TestProfiles_defaultProfileMatchesMainProfile(t *testing.T) {
	main := setupProfilesTree(t)

	defaulted := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "list", "project")
	})
	explicit := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
	})
	if defaulted != explicit {
		t.Errorf("default output != explicit -p personal\ndefault=%q\nexplicit=%q", defaulted, explicit)
	}
}

func TestProfiles_noMainProfile_errors(t *testing.T) {
	main := setupProfilesTree(t)
	t.Setenv(config.EnvProfileForTest(), "") // clear any ambient CODEHERD_PROFILE
	content := `[defaults]
profiles_enabled = true
profiles_dir = "` + filepath.Join(filepath.Dir(main), "profiles") + `"
`
	if err := os.WriteFile(main, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, "--config", main, "list", "project")
	if err == nil {
		t.Fatal("runCmd() error = nil, want error when no profile resolved")
	}
	if !strings.Contains(err.Error(), "main_profile") && !strings.Contains(err.Error(), "-p") {
		t.Errorf("error = %v, want message mentioning main_profile or -p", err)
	}
}

func TestProfiles_sessionIsolationAcrossProfiles(t *testing.T) {
	useIsolatedTmux(t)

	main := setupProfilesTree(t)
	root := filepath.Dir(main)
	personalProjects := filepath.Join(root, "personal-projects")
	workProjects := filepath.Join(root, "work-projects")
	initBareRepo(t, filepath.Join(personalProjects, "github.com", "u", "myapp"))
	initBareRepo(t, filepath.Join(workProjects, "github.com", "u", "other"))

	if err := runCmd(t, "--config", main, "--profile", "personal", "create", "session", "myapp", "feat"); err != nil {
		t.Fatalf("personal create session: %v", err)
	}
	if err := runCmd(t, "--config", main, "--profile", "work", "create", "session", "other", "feat"); err != nil {
		t.Fatalf("work create session: %v", err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "--profile", "personal", "list", "session")
	})
	if !strings.Contains(out, "myapp") || strings.Contains(out, "other") {
		t.Errorf("personal list session out = %q, want myapp only", out)
	}

	tmuxOut, _ := tmuxCmd(t, "ls").Output()
	if !strings.Contains(string(tmuxOut), "personal-myapp-feat") {
		t.Errorf("tmux ls did not show personal-myapp-feat:\n%s", tmuxOut)
	}
	if !strings.Contains(string(tmuxOut), "work-other-feat") {
		t.Errorf("tmux ls did not show work-other-feat:\n%s", tmuxOut)
	}
}

func TestProfiles_strayKeysWarning(t *testing.T) {
	main := setupProfilesTree(t)
	f, err := os.OpenFile(main, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n[projects.foo]\nrepo = \"git@x:foo\"\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Intercept the warning via the explicit sink (set in Chunk 1 Task 1.3).
	var buf bytes.Buffer
	config.SetWarningSink(&buf)
	defer config.SetWarningSink(nil)

	err = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
	if err != nil {
		t.Fatalf("runCmd() error = %v, want nil (warning, not failure)", err)
	}
	if !strings.Contains(buf.String(), "profiles_enabled=true") {
		t.Errorf("warning sink = %q, want stray-keys warning", buf.String())
	}
}

func TestProfiles_envSelectsProfile(t *testing.T) {
	main := setupProfilesTree(t)
	t.Setenv(config.EnvProfileForTest(), "work")

	// No --profile flag: CODEHERD_PROFILE should override main_profile=personal.
	out := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "list", "project")
	})
	if !strings.Contains(out, "other") || strings.Contains(out, "myapp") {
		t.Errorf("env-selected list project out = %q, want other only", out)
	}
}

func TestProfiles_flagBeatsEnv(t *testing.T) {
	main := setupProfilesTree(t)
	t.Setenv(config.EnvProfileForTest(), "work")

	// --profile personal must win over CODEHERD_PROFILE=work.
	out := captureStdout(t, func() {
		_ = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
	})
	if !strings.Contains(out, "myapp") || strings.Contains(out, "other") {
		t.Errorf("flag-beats-env list project out = %q, want myapp only", out)
	}
}

func TestProfiles_envMissingProfile_errors(t *testing.T) {
	main := setupProfilesTree(t)
	t.Setenv(config.EnvProfileForTest(), "ghost")

	err := runCmd(t, "--config", main, "list", "project")
	if err == nil {
		t.Fatal("runCmd() error = nil, want error for missing env profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'profile not found'", err)
	}
}

func TestProfiles_envIgnoredWhenDisabled(t *testing.T) {
	// A plain config with profiles_enabled unset: CODEHERD_PROFILE must be ignored.
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config.toml")
	body := `[defaults]
projects_dir = "` + filepath.Join(root, "projects") + `"

[projects.plain]
repo = "git@github.com:u/plain.git"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvProfileForTest(), "work")

	out := captureStdout(t, func() {
		_ = runCmd(t, "--config", cfgPath, "list", "project")
	})
	if !strings.Contains(out, "plain") {
		t.Errorf("profiles-disabled list project out = %q, want plain (env ignored)", out)
	}
}
