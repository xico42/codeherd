package herd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
)

// h.Ref takes no profile argument, so the shortest path is the correct one.
// This is the whole point of the collapse: the profile-blind Ref cannot be
// spelled without visibly hand-building the struct.
func TestRef_carriesActiveProfile(t *testing.T) {
	h := New(&config.Config{}, &config.ProfileRegistry{Active: "work"}, Deps{})
	ref := h.Ref("myapp", "feat")

	if ref.Profile != "work" {
		t.Errorf("Profile = %q, want %q", ref.Profile, "work")
	}
	if got := ref.CanonicalName(); got != "work-myapp-feat" {
		t.Errorf("CanonicalName() = %q, want %q", got, "work-myapp-feat")
	}
}

// A nil registry is what config.Load returns when profiles are off. New must
// not panic on it — the spec's own §8.1 sample did.
func TestNew_nilRegistryMeansNoProfile(t *testing.T) {
	h := New(&config.Config{}, nil, Deps{})
	ref := h.Ref("myapp", "feat")

	if ref.Profile != "" {
		t.Errorf("Profile = %q, want empty", ref.Profile)
	}
	if got := ref.CanonicalName(); got != "myapp-feat" {
		t.Errorf("CanonicalName() = %q, want %q", got, "myapp-feat")
	}
}

func TestRef_tmuxNameDiffersByType(t *testing.T) {
	h := New(&config.Config{}, &config.ProfileRegistry{Active: "work"}, Deps{})
	ref := h.Ref("myapp", "feat/login")

	if got := ref.tmuxName(SessionTypeAgent); got != "work-myapp-feat-login" {
		t.Errorf("agent tmuxName = %q", got)
	}
	if got := ref.tmuxName(SessionTypeShell); got != "work-myapp-feat-login~sh" {
		t.Errorf("shell tmuxName = %q", got)
	}
}

func TestWithProfile_swapsConfigAndProfile(t *testing.T) {
	dir := t.TempDir()
	toml := "[projects.myapp]\nrepo = \"git@github.com:user/other.git\"\n"
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &config.ProfileRegistry{Active: "work", Names: []string{"work", "home"}, ProfilesDir: dir}
	h := New(&config.Config{}, reg, Deps{})

	next, err := h.WithProfile("home")
	if err != nil {
		t.Fatalf("WithProfile: %v", err)
	}
	if next.Ref("myapp", "feat").Profile != "home" {
		t.Error("new Herd did not adopt the home profile")
	}
	if next.Config().Projects["myapp"].Repo != "git@github.com:user/other.git" {
		t.Error("new Herd did not adopt the home config")
	}
	if h.Ref("myapp", "feat").Profile != "work" {
		t.Error("WithProfile mutated the receiver; it must return a new Herd")
	}
}

func TestWithProfile_errorsWhenProfilesDisabled(t *testing.T) {
	h := New(&config.Config{}, nil, Deps{})
	if _, err := h.WithProfile("work"); err == nil {
		t.Fatal("want error when profiles are disabled, got nil")
	}
}

// hookFor must be total (never nil) AND thread the right project's hook
// config. A "!= nil" assertion can never fail here — hooks.New returns a
// non-nil *Service for any input, including a zero HooksConfig — so this
// overrides h.newHook with a capturing func and asserts on what was passed.
func TestHookFor_defaultsToConfiguredHooks(t *testing.T) {
	myappHooks := config.HooksConfig{PreClone: "echo hi"}
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{
		"myapp": {Hooks: myappHooks},
	}}
	h := New(cfg, nil, Deps{})

	var got []config.HooksConfig
	h.newHook = func(hc config.HooksConfig) hooks.Hook {
		got = append(got, hc)
		return &hooks.NoOp{}
	}

	if hf := h.hookFor("myapp"); hf == nil {
		t.Error("hookFor returned nil for a configured project")
	}
	if hf := h.hookFor("nonexistent"); hf == nil {
		t.Error("hookFor returned nil for an unconfigured project; it must be total")
	}

	if len(got) != 2 {
		t.Fatalf("newHook called %d times, want 2", len(got))
	}
	if got[0] != myappHooks {
		t.Errorf("hookFor(%q) passed %+v, want %+v", "myapp", got[0], myappHooks)
	}
	if got[1] != (config.HooksConfig{}) {
		t.Errorf("hookFor(%q) passed %+v, want zero value", "nonexistent", got[1])
	}
}
