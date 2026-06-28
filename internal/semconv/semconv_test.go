package semconv_test

import (
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/semconv"
)

func TestFlattenBranch(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"main", "main"},
		{"feature/login", "feature-login"},
		{"fix/auth/token", "fix-auth-token"},
		{"no-slash", "no-slash"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := semconv.FlattenBranch(tt.input); got != tt.want {
			t.Errorf("FlattenBranch(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSessionName(t *testing.T) {
	tests := []struct {
		project, branch, want string
	}{
		{"myapp", "feature", "myapp-feature"},
		{"myapp", "feature/login", "myapp-feature-login"},
		{"api", "fix/auth/token", "api-fix-auth-token"},
	}
	for _, tt := range tests {
		if got := semconv.SessionName("", tt.project, tt.branch); got != tt.want {
			t.Errorf("SessionName(%q, %q) = %q, want %q", tt.project, tt.branch, got, tt.want)
		}
	}
}

func TestSessionName_withProfile(t *testing.T) {
	cases := []struct {
		profile string
		project string
		branch  string
		want    string
	}{
		{"", "myapp", "main", "myapp-main"},
		{"", "myapp", "feat/x", "myapp-feat-x"},
		{"personal", "myapp", "main", "personal-myapp-main"},
		{"work", "myapp", "feat/x", "work-myapp-feat-x"},
	}
	for _, tc := range cases {
		got := semconv.SessionName(tc.profile, tc.project, tc.branch)
		if got != tc.want {
			t.Errorf("SessionName(%q, %q, %q) = %q, want %q",
				tc.profile, tc.project, tc.branch, got, tc.want)
		}
	}
}

func TestShellSessionName_withProfile(t *testing.T) {
	cases := []struct {
		profile string
		project string
		branch  string
		want    string
	}{
		{"", "myapp", "main", "myapp-main~sh"},
		{"work", "myapp", "main", "work-myapp-main~sh"},
	}
	for _, tc := range cases {
		got := semconv.ShellSessionName(tc.profile, tc.project, tc.branch)
		if got != tc.want {
			t.Errorf("ShellSessionName(%q, %q, %q) = %q, want %q",
				tc.profile, tc.project, tc.branch, got, tc.want)
		}
	}
}

func TestTmuxOptionProfile_constant(t *testing.T) {
	if semconv.TmuxOptionProfile != "@codeherd_profile" {
		t.Errorf("TmuxOptionProfile = %q, want @codeherd_profile", semconv.TmuxOptionProfile)
	}
}

func TestCloneDir(t *testing.T) {
	got := semconv.CloneDir("/home/user/projects", "github.com/user/myapp")
	want := "/home/user/projects/github.com/user/myapp"
	if got != want {
		t.Errorf("CloneDir() = %q, want %q", got, want)
	}
}

func TestWorktreesRoot(t *testing.T) {
	got := semconv.WorktreesRoot("/home/user/projects", "github.com/user/myapp")
	want := "/home/user/projects/github.com/user/myapp__worktrees"
	if got != want {
		t.Errorf("WorktreesRoot() = %q, want %q", got, want)
	}
}

func TestWorktreePath(t *testing.T) {
	got := semconv.WorktreePath("/home/user/projects", "github.com/user/myapp", "feature/login")
	want := "/home/user/projects/github.com/user/myapp__worktrees/feature-login"
	if got != want {
		t.Errorf("WorktreePath() = %q, want %q", got, want)
	}
}

func TestShellSessionName(t *testing.T) {
	tests := []struct {
		project, branch, want string
	}{
		{"myapp", "feature", "myapp-feature~sh"},
		{"myapp", "feature/login", "myapp-feature-login~sh"},
		{"api", "fix/auth/token", "api-fix-auth-token~sh"},
	}
	for _, tt := range tests {
		if got := semconv.ShellSessionName("", tt.project, tt.branch); got != tt.want {
			t.Errorf("ShellSessionName(%q, %q) = %q, want %q", tt.project, tt.branch, got, tt.want)
		}
	}
}

func TestTmuxOptionConstants(t *testing.T) {
	// Verify constants have @ prefix (required for tmux user options).
	for _, opt := range []string{
		semconv.TmuxOptionStatus,
		semconv.TmuxOptionAnnotation,
		semconv.TmuxOptionStartedAt,
	} {
		if !strings.HasPrefix(opt, "@") {
			t.Errorf("tmux option %q must start with @", opt)
		}
	}
}

func TestConstants(t *testing.T) {
	if semconv.SessionEnvVar != "CODEHERD_SESSION" {
		t.Errorf("SessionEnvVar = %q, want CODEHERD_SESSION", semconv.SessionEnvVar)
	}
}

func TestHookConstants_NotEmpty(t *testing.T) {
	hooks := []string{
		semconv.HookPreClone, semconv.HookPostClone,
		semconv.HookPreWorktree, semconv.HookPostWorktree,
		semconv.HookPreCopy, semconv.HookPostCopy,
		semconv.HookPreTemplate, semconv.HookPostTemplate,
		semconv.HookPreSession, semconv.HookPostSession,
	}
	for _, h := range hooks {
		if h == "" {
			t.Errorf("hook constant is empty")
		}
	}
}

func TestHookAttrConstants_HavePrefix(t *testing.T) {
	attrs := []string{
		semconv.HookAttrProject, semconv.HookAttrBranch,
		semconv.HookAttrRepo, semconv.HookAttrCloneDir,
		semconv.HookAttrWorktreePath, semconv.HookAttrSessionName,
	}
	for _, a := range attrs {
		if !strings.HasPrefix(a, "CODEHERD_") {
			t.Errorf("attribute %q missing CODEHERD_ prefix", a)
		}
	}
}

func TestWorktreeIdentityBranch(t *testing.T) {
	tests := []struct {
		name, path, cloneDir, defaultBranch, liveBranch, want string
	}{
		{"worktree dir uses folder name", "/p/github.com/u/app__worktrees/feature-x", "/p/github.com/u/app", "main", "", "feature-x"},
		{"worktree dir ignores live branch", "/p/github.com/u/app__worktrees/feature-x", "/p/github.com/u/app", "main", "other", "feature-x"},
		{"clone dir uses default branch", "/p/github.com/u/app", "/p/github.com/u/app", "main", "", "main"},
		{"clone dir falls back to live branch when default unset", "/p/github.com/u/app", "/p/github.com/u/app", "", "develop", "develop"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := semconv.WorktreeIdentityBranch(tc.path, tc.cloneDir, tc.defaultBranch, tc.liveBranch)
			if got != tc.want {
				t.Errorf("WorktreeIdentityBranch(%q,%q,%q,%q) = %q, want %q",
					tc.path, tc.cloneDir, tc.defaultBranch, tc.liveBranch, got, tc.want)
			}
		})
	}
}

func TestTmuxOptionBranch_constant(t *testing.T) {
	if semconv.TmuxOptionBranch != "@codeherd_branch" {
		t.Errorf("TmuxOptionBranch = %q, want @codeherd_branch", semconv.TmuxOptionBranch)
	}
}

func TestNewSemconvConstants(t *testing.T) {
	if semconv.TmuxOptionCanonicalName != "@codeherd_canonical_name" {
		t.Errorf("TmuxOptionCanonicalName = %q", semconv.TmuxOptionCanonicalName)
	}
	if semconv.TmuxOptionSessionType != "@codeherd_session_type" {
		t.Errorf("TmuxOptionSessionType = %q", semconv.TmuxOptionSessionType)
	}
	if semconv.SessionTypeAgent != "agent" {
		t.Errorf("SessionTypeAgent = %q", semconv.SessionTypeAgent)
	}
	if semconv.SessionTypeShell != "shell" {
		t.Errorf("SessionTypeShell = %q", semconv.SessionTypeShell)
	}
}
