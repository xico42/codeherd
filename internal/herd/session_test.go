package herd

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// sessionHerd builds a Herd wired to the given tmux fake, with one configured
// project "myapp" and a default "claude" agent. It returns the projects_dir so
// tests can materialize the worktree path Launch derives (and stats) from the
// Ref.
func sessionHerd(t *testing.T, f *fakeTmux) (*Herd, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir, Agent: "claude"},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
		Agents: map[string]config.AgentConfig{"claude": {Cmd: "claude"}},
	}
	return New(cfg, nil, Deps{Tmux: f, Git: &fakeGit{}}), dir
}

// mkMyappWorktree creates the on-disk worktree path Launch derives for
// project "myapp" and the given branch, so the os.Stat check passes.
func mkMyappWorktree(t *testing.T, dir, branch string) {
	t.Helper()
	p := filepath.Join(dir, "github.com", "user", "myapp__worktrees", semconv.FlattenBranch(branch))
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// newSessionEnv extracts the KEY=VALUE pairs passed via -e flags to the
// new-session call, or nil if no such call was recorded.
func newSessionEnv(calls [][]string) map[string]string {
	for _, c := range calls {
		if len(c) == 0 || c[0] != "new-session" {
			continue
		}
		env := map[string]string{}
		for i := 0; i < len(c)-1; i++ {
			if c[i] != "-e" {
				continue
			}
			kv := c[i+1]
			eq := strings.Index(kv, "=")
			if eq < 0 {
				continue
			}
			env[kv[:eq]] = kv[eq+1:]
		}
		return env
	}
	return nil
}

func TestLaunch_OK(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	handle, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if handle.ID != "$1" {
		t.Errorf("Launch() ID = %q, want $1", handle.ID)
	}
	if handle.Type != SessionTypeAgent {
		t.Errorf("Launch() Type = %q, want agent", handle.Type)
	}
	if handle.Ref != (Ref{Project: "myapp", Branch: "feature"}) {
		t.Errorf("Launch() Ref = %+v", handle.Ref)
	}
}

func TestLaunch_StampsBranchOption(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature/login")

	if _, err := h.Launch(h.Ref("myapp", "feature/login"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !f.called("set-option", semconv.TmuxOptionBranch, "feature/login") {
		t.Errorf("expected set-option %s feature/login; calls=%v", semconv.TmuxOptionBranch, f.Calls)
	}
}

// Launch stamps the project so Sessions can rebuild a complete Ref.
func TestLaunch_stampsProjectOption(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feat"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Launch(h.Ref("myapp", "feat"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !f.called("set-option", semconv.TmuxOptionProject, "myapp") {
		t.Errorf("@codeherd_project was not stamped; calls=%v", f.Calls)
	}
}

func TestLaunch_DuplicateSession(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "running", Branch: "feature", Project: "myapp"},
	}}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if !errors.Is(err, ErrSessionExists) {
		t.Errorf("error = %v, want ErrSessionExists", err)
	}
}

func TestLaunch_DuplicateSession_Prefixed(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "⚡ myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "waiting", Branch: "feature", Project: "myapp"},
	}}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if !errors.Is(err, ErrSessionExists) {
		t.Errorf("error = %v, want ErrSessionExists", err)
	}
}

func TestLaunch_MissingPath(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f) // worktree path deliberately not created

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("error = %v, want ErrPathNotFound", err)
	}
}

func TestLaunch_StatError(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	// A NUL byte in the branch yields a worktree path that os.Stat rejects
	// with EINVAL, not IsNotExist — a different error path than ErrPathNotFound.
	_, err := h.Launch(h.Ref("myapp", "\x00invalid"), LaunchOpts{})
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
	if errors.Is(err, ErrPathNotFound) {
		t.Error("got ErrPathNotFound, expected a different error for invalid path")
	}
}

func TestLaunch_ListError(t *testing.T) {
	f := &fakeTmux{RunFn: func(_ ...string) (string, string, int, error) {
		return "", "", -1, errors.New("tmux exec failed")
	}}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestLaunch_newSessionError(t *testing.T) {
	f := &fakeTmux{RunFn: func(args ...string) (string, string, int, error) {
		switch args[0] {
		case "list-sessions":
			return "", "", 1, nil // no sessions
		case "new-session":
			return "", "tmux failed", 1, nil
		}
		return "", "", 0, nil
	}}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if err == nil {
		t.Fatal("expected error when new-session fails")
	}
}

func TestLaunch_noDefaultAgent(t *testing.T) {
	f := &fakeTmux{}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir}, // no default agent
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, nil, Deps{Tmux: f, Git: &fakeGit{}})
	mkMyappWorktree(t, dir, "feature")

	_, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{})
	if err == nil || !strings.Contains(err.Error(), "no agent specified") {
		t.Errorf("error = %v, want 'no agent specified'", err)
	}
}

func TestLaunch_TriggersHooks(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")
	hookMock := &mockHook{}
	withHook(h, hookMock)

	if _, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if len(hookMock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookMock.calls))
	}
	if hookMock.calls[0].name != semconv.HookPreSession {
		t.Errorf("first hook = %q, want %q", hookMock.calls[0].name, semconv.HookPreSession)
	}
	if hookMock.calls[1].name != semconv.HookPostSession {
		t.Errorf("second hook = %q, want %q", hookMock.calls[1].name, semconv.HookPostSession)
	}
}

func TestLaunch_writesProfileOption_whenSet(t *testing.T) {
	f := &fakeTmux{}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir, Agent: "claude"},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
		Agents:   map[string]config.AgentConfig{"claude": {Cmd: "claude"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})
	mkMyappWorktree(t, dir, "feature")

	if _, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !f.called("new-session", "-s", "work-myapp-feature") {
		t.Errorf("expected new-session on work-myapp-feature; got %v", f.Calls)
	}
	if !f.called("set-option", "work-myapp-feature", semconv.TmuxOptionProfile, "work") {
		t.Errorf("expected set-option @codeherd_profile work; got %v", f.Calls)
	}
}

func TestLaunch_emptyProfile_noProfileOptionWritten(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	if _, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !f.called("new-session", "-s", "myapp-feature") {
		t.Errorf("expected new-session on myapp-feature (no prefix); got %v", f.Calls)
	}
	for _, c := range f.Calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "set-option") && strings.Contains(joined, semconv.TmuxOptionProfile) {
			t.Errorf("unexpected set-option @codeherd_profile call: %v", c)
		}
	}
}

func TestLaunch_StampsCodeherdEnvVars(t *testing.T) {
	f := &fakeTmux{}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir, Agent: "claude"},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
		Agents:   map[string]config.AgentConfig{"claude": {Cmd: "claude", Env: map[string]string{"USER_VAR": "user-value"}}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})
	mkMyappWorktree(t, dir, "feature/x")

	if _, err := h.Launch(h.Ref("myapp", "feature/x"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	env := newSessionEnv(f.Calls)
	if env == nil {
		t.Fatalf("no new-session call recorded; calls=%v", f.Calls)
	}
	wantClone := filepath.Join(dir, "github.com", "user", "myapp")
	wantPath := filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feature-x")
	want := map[string]string{
		"USER_VAR":                   "user-value",
		semconv.SessionEnvVar:        "work-myapp-feature-x",
		semconv.HookAttrProject:      "myapp",
		semconv.HookAttrBranch:       "feature/x",
		semconv.HookAttrWorktreePath: wantPath,
		semconv.HookAttrCloneDir:     wantClone,
		semconv.EnvProfile:           "work",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, env[k], v)
		}
	}
}

func TestLaunch_CodeherdEnvWinsOverUserEnv(t *testing.T) {
	f := &fakeTmux{}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir, Agent: "claude"},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
		Agents: map[string]config.AgentConfig{"claude": {Cmd: "claude", Env: map[string]string{
			semconv.HookAttrProject:      "evil-project",
			semconv.HookAttrBranch:       "evil-branch",
			semconv.HookAttrWorktreePath: "/evil/path",
			semconv.HookAttrCloneDir:     "/evil/clone",
			semconv.SessionEnvVar:        "evil-session",
		}}},
	}
	h := New(cfg, nil, Deps{Tmux: f, Git: &fakeGit{}})
	mkMyappWorktree(t, dir, "feature")

	if _, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	env := newSessionEnv(f.Calls)
	checks := map[string]string{
		semconv.HookAttrProject:      "myapp",
		semconv.HookAttrBranch:       "feature",
		semconv.HookAttrWorktreePath: filepath.Join(dir, "github.com", "user", "myapp__worktrees", "feature"),
		semconv.HookAttrCloneDir:     filepath.Join(dir, "github.com", "user", "myapp"),
		semconv.SessionEnvVar:        "myapp-feature",
	}
	for k, want := range checks {
		if got := env[k]; got != want {
			t.Errorf("env[%q] = %q, want %q (user env must not shadow codeherd)", k, got, want)
		}
	}
}

func TestLaunch_ProfileEnvOmittedWhenEmpty(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	if _, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	env := newSessionEnv(f.Calls)
	if _, ok := env[semconv.EnvProfile]; ok {
		t.Errorf("CODEHERD_PROFILE must be absent when no profile is active; got %q", env[semconv.EnvProfile])
	}
}

func TestLaunch_ShellRunsShellCommand(t *testing.T) {
	f := &fakeTmux{}
	h, dir := sessionHerd(t, f)
	mkMyappWorktree(t, dir, "feature")

	handle, err := h.Launch(h.Ref("myapp", "feature"), LaunchOpts{Type: SessionTypeShell})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if handle.Type != SessionTypeShell {
		t.Errorf("Type = %q, want shell", handle.Type)
	}
	if !f.called("new-session", "-s", "myapp-feature~sh") {
		t.Errorf("expected new-session on myapp-feature~sh; got %v", f.Calls)
	}
}

func TestResolve_OK(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "running", StartedAt: "2024-01-01T00:00:00Z",
			Branch: "feature", Project: "myapp"},
	}}
	h, _ := sessionHerd(t, f)

	info, err := h.Resolve(h.Ref("myapp", "feature"), SessionTypeAgent)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.TmuxName != "myapp-feature" {
		t.Errorf("TmuxName = %q, want myapp-feature", info.TmuxName)
	}
	if info.ID != "$1" {
		t.Errorf("ID = %q, want $1", info.ID)
	}
	if info.Status != StatusRunning {
		t.Errorf("Status = %q, want running", info.Status)
	}
	if info.StartedAt.IsZero() {
		t.Error("StartedAt should be non-zero")
	}
}

func TestResolve_WaitingSession(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$2", Name: "⚡ myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "waiting", Annotation: "need input",
			Branch: "feature", Project: "myapp"},
	}}
	h, _ := sessionHerd(t, f)

	info, err := h.Resolve(h.Ref("myapp", "feature"), SessionTypeAgent)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.TmuxName != "⚡ myapp-feature" {
		t.Errorf("TmuxName = %q, want ⚡ myapp-feature", info.TmuxName)
	}
	if info.ID != "$2" {
		t.Errorf("ID = %q, want $2", info.ID)
	}
}

func TestResolve_NotFound(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	_, err := h.Resolve(h.Ref("nonexistent", "branch"), SessionTypeAgent)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestResolve_RunnerError(t *testing.T) {
	f := &fakeTmux{RunFn: func(_ ...string) (string, string, int, error) {
		return "", "", -1, errors.New("tmux exec failed")
	}}
	h, _ := sessionHerd(t, f)

	_, err := h.Resolve(h.Ref("myapp", "feature"), SessionTypeAgent)
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestResolve_ShellType(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-main~sh", Canonical: "myapp-main",
			Type: "shell", Status: "running", Branch: "main", Project: "myapp"},
	}}
	h, _ := sessionHerd(t, f)

	info, err := h.Resolve(h.Ref("myapp", "main"), SessionTypeShell)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.Type != SessionTypeShell {
		t.Fatalf("Type = %q, want shell", info.Type)
	}
	// Agent-type Resolve must miss a shell-only session.
	if _, err := h.Resolve(h.Ref("myapp", "main"), SessionTypeAgent); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("agent Resolve for shell-only session: want ErrSessionNotFound, got %v", err)
	}
}

func TestSessions_Empty(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestSessions_RunnerError(t *testing.T) {
	f := &fakeTmux{RunFn: func(_ ...string) (string, string, int, error) {
		return "", "", -1, errors.New("tmux exec failed")
	}}
	h, _ := sessionHerd(t, f)

	_, err := h.Sessions()
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestSessions_includesAgentAndShell(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "app-main", Canonical: "app-main",
			Type: "agent", Status: "running", Branch: "main", Project: "app"},
		{ID: "$2", Name: "app-main~sh", Canonical: "app-main",
			Type: "shell", Status: "running", Branch: "main", Project: "app"},
	}}
	h, _ := sessionHerd(t, f)

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (agent + shell)", len(got))
	}
	var sawAgent, sawShell bool
	for _, s := range got {
		switch s.Type {
		case SessionTypeAgent:
			sawAgent = true
		case SessionTypeShell:
			sawShell = true
		}
	}
	if !sawAgent || !sawShell {
		t.Fatalf("Sessions missing a type: agent=%v shell=%v", sawAgent, sawShell)
	}
}

func TestSessions_parsesStartedAt(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-main", Canonical: "myapp-main",
			Type: "agent", Status: "running", StartedAt: "2024-01-15T10:00:00Z",
			Branch: "main", Project: "myapp"},
	}}
	h, _ := sessionHerd(t, f)

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}
	if len(got) != 1 || got[0].StartedAt.IsZero() {
		t.Errorf("StartedAt should be non-zero when timestamp is provided; got %+v", got)
	}
}

// Sessions rebuilds a complete Ref from the tmux options, so a handle from a
// list can be fed straight back into Teardown.
func TestSessions_rebuildsCompleteRef(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d handles, want 1", len(got))
	}
	want := Ref{Profile: "work", Project: "myapp", Branch: "feat"}
	if got[0].Ref != want {
		t.Errorf("Ref = %+v, want %+v", got[0].Ref, want)
	}
}

// Sessions is profile-scoped: another profile's sessions are not ours.
func TestSessions_filtersByActiveProfile(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "home-myapp-feat", Canonical: "home-myapp-feat",
			Type: "agent", Profile: "home", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	got, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "$1" {
		t.Errorf("got %+v, want only the work-profile session", got)
	}
}

func TestStopSessions_OK(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "running", Branch: "feature", Project: "myapp"},
	}}
	h, _ := sessionHerd(t, f)

	stopped, err := h.StopSessions(h.Ref("myapp", "feature"), StopOpts{Type: SessionTypeAgent})
	if err != nil {
		t.Fatalf("StopSessions() error = %v", err)
	}
	if len(stopped) != 1 {
		t.Fatalf("stopped %d, want 1", len(stopped))
	}
	if killed := f.killed(); len(killed) != 1 || killed[0] != "$1" {
		t.Errorf("killed = %v, want [$1] (by ID)", killed)
	}
}

func TestStopSessions_KillError(t *testing.T) {
	f := &fakeTmux{RunFn: func(args ...string) (string, string, int, error) {
		switch args[0] {
		case "list-sessions":
			row := sessionRow{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
				Type: "agent", Status: "running", Branch: "feature", Project: "myapp"}
			return row.format(), "", 0, nil
		case "kill-session":
			return "", "kill failed", 1, nil
		}
		return "", "", 0, nil
	}}
	h, _ := sessionHerd(t, f)

	if _, err := h.StopSessions(h.Ref("myapp", "feature"), StopOpts{Type: SessionTypeAgent}); err == nil {
		t.Fatal("expected error when kill fails")
	}
}

func TestStopSessions_RunnerError(t *testing.T) {
	f := &fakeTmux{RunFn: func(_ ...string) (string, string, int, error) {
		return "", "", -1, errors.New("tmux exec failed")
	}}
	h, _ := sessionHerd(t, f)

	if _, err := h.StopSessions(h.Ref("myapp", "feature"), StopOpts{All: true}); err == nil {
		t.Fatal("expected error when runner fails")
	}
}

// Under an active profile, sessions are named <profile>-<project>-<branch>.
// session.Service.Stop hardcoded an empty profile via SessionName("", …), so
// it searched for myapp-feat and missed work-myapp-feat entirely. Here the
// profile rides on the Ref and there is no parameter to omit.
func TestStopSessions_underProfile_matchesProfileScopedSession(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{Type: SessionTypeAgent})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 1 {
		t.Fatalf("stopped %d sessions, want 1", len(stopped))
	}
	if killed := f.killed(); len(killed) != 1 || killed[0] != "$1" {
		t.Errorf("killed = %v, want [$1] — the session was addressed by name, not ID", killed)
	}
}

// StopOpts.All is what Teardown uses: both types die, addressed by ID.
func TestStopSessions_all_stopsBothTypesByID(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Profile: "work", Branch: "feat", Project: "myapp"},
		{ID: "$2", Name: "work-myapp-feat~sh", Canonical: "work-myapp-feat",
			Type: "shell", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{All: true})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("stopped %d sessions, want 2", len(stopped))
	}
	killed := f.killed()
	sort.Strings(killed)
	if len(killed) != 2 || killed[0] != "$1" || killed[1] != "$2" {
		t.Errorf("killed = %v, want [$1 $2]", killed)
	}
}

// Stopping a session that isn't running is not an error: Teardown calls this
// unconditionally, and a worktree with no sessions is the common case.
func TestStopSessions_noneRunning_isNotAnError(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	stopped, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{All: true})
	if err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want empty", stopped)
	}
}

func TestSetStatus_Running(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "⚡ myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "waiting"},
	}}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("myapp-feature", StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	// Running + prefixed name → rename drops the ⚡ prefix.
	if !f.called("rename-session", "⚡ myapp-feature", "myapp-feature") {
		t.Errorf("expected rename dropping prefix; calls=%v", f.Calls)
	}
}

func TestSetStatus_Waiting(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "myapp-feature", Canonical: "myapp-feature",
			Type: "agent", Status: "running"},
	}}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("myapp-feature", StatusWaiting, "Claude needs input"); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	if !f.called("rename-session", "myapp-feature", semconv.StatusPrefix+"myapp-feature") {
		t.Errorf("expected rename adding prefix; calls=%v", f.Calls)
	}
}

func TestSetStatus_EmptyName(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("", StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus() on empty name error = %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(f.Calls))
	}
}

func TestSetStatus_InvalidStatus(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("myapp-feature", Status("invalid"), ""); err != nil {
		t.Fatalf("SetStatus() should suppress errors: %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("expected 0 calls for invalid status, got %d", len(f.Calls))
	}
}

func TestSetStatus_SuppressesError(t *testing.T) {
	f := &fakeTmux{RunFn: func(_ ...string) (string, string, int, error) {
		return "", "", -1, errors.New("tmux failed")
	}}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("any-session", StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus() should suppress errors: %v", err)
	}
}

func TestSetStatus_SessionNotFound(t *testing.T) {
	f := &fakeTmux{}
	h, _ := sessionHerd(t, f)

	if err := h.SetStatus("myapp-feature", StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus() should suppress not-found: %v", err)
	}
	// Only list-sessions was called; no set-option/rename.
	if len(f.Calls) != 1 {
		t.Errorf("expected 1 call (list only), got %d: %v", len(f.Calls), f.Calls)
	}
}

// A session created before @codeherd_project existed has a correct stored
// canonical name but an empty Project. It must still be found and killed —
// this is the exact orphan the collapse reintroduced.
func TestStopSessions_preUpgradeSession_matchedByStoredCanonical(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: ""},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	if _, err := h.Resolve(h.Ref("myapp", "feat"), SessionTypeAgent); err != nil {
		t.Fatalf("Resolve found nothing for a pre-upgrade session: %v", err)
	}
	if _, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{}); err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if got := f.killed(); len(got) != 1 || got[0] != "$1" {
		t.Errorf("killed = %v, want [$1] — the pre-upgrade session was not killed", got)
	}
}

func TestResolveProject(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{
		"myapp": {}, "other": {},
	}}
	tests := []struct {
		name            string
		profile, branch string
		canonical       string
		wantProj        string
		wantOK          bool
	}{
		{"under profile", "work", "feat", "work-myapp-feat", "myapp", true},
		{"no profile", "", "feat", "myapp-feat", "myapp", true},
		{"flattened slash branch", "work", "feat/login", "work-myapp-feat-login", "myapp", true},
		{"no configured match", "work", "feat", "work-nope-feat", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveProject(cfg, tt.profile, tt.branch, tt.canonical)
			if got != tt.wantProj || ok != tt.wantOK {
				t.Errorf("resolveProject = (%q, %v), want (%q, %v)", got, ok, tt.wantProj, tt.wantOK)
			}
		})
	}
}

// A pre-upgrade session's project is recovered for display and re-stamped on
// the live session, so it heals to first-class on first observation.
func TestSessions_preUpgradeSession_recoversAndHealsProject(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: ""},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	sessions, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Ref.Project != "myapp" {
		t.Fatalf("Ref.Project = %q, want %q", sessions[0].Ref.Project, "myapp")
	}
	if !f.called("set-option", "@codeherd_project", "myapp") {
		t.Errorf("project was not re-stamped; calls=%v", f.Calls)
	}
}

// A session that already carries @codeherd_project is never re-stamped.
func TestSessions_stampedSession_isNotHealed(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	if _, err := h.Sessions(); err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if f.called("set-option", "@codeherd_project") {
		t.Errorf("an already-stamped session was healed again; calls=%v", f.Calls)
	}
}

func TestSessionExistsError(t *testing.T) {
	err := &SessionExistsError{
		Ref:  Ref{Project: "myapp", Branch: "feature"},
		Type: SessionTypeAgent,
	}
	want := "session already exists: myapp/feature (agent)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrSessionExists) {
		t.Error("errors.Is(err, ErrSessionExists) = false, want true")
	}
}
