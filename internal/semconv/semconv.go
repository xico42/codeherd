package semconv

import (
	"path/filepath"
	"strings"
)

const (
	SessionEnvVar = "CODEHERD_SESSION"

	TmuxOptionStatus        = "@codeherd_status"
	TmuxOptionAnnotation    = "@codeherd_annotation"
	TmuxOptionStartedAt     = "@codeherd_started_at"
	TmuxOptionCanonicalName = "@codeherd_canonical_name"
	TmuxOptionSessionType   = "@codeherd_session_type"
	TmuxOptionProfile       = "@codeherd_profile"

	StatusRunning = "running"
	StatusWaiting = "waiting"

	SessionTypeAgent = "agent"
	SessionTypeShell = "shell"

	StatusPrefix = "⚡ "

	CodeherdSessionName = "codeherd"
)

// Hook lifecycle names.
const (
	HookPreClone     = "pre-clone"
	HookPostClone    = "post-clone"
	HookPreWorktree  = "pre-worktree"
	HookPostWorktree = "post-worktree"
	HookPreCopy      = "pre-copy"
	HookPostCopy     = "post-copy"
	HookPreTemplate  = "pre-template"
	HookPostTemplate = "post-template"
	HookPreSession   = "pre-session"
	HookPostSession  = "post-session"
)

// Hook attributes — environment variable names passed to hook commands.
const (
	HookAttrProject      = "CODEHERD_PROJECT"
	HookAttrBranch       = "CODEHERD_BRANCH"
	HookAttrRepo         = "CODEHERD_REPO"
	HookAttrCloneDir     = "CODEHERD_CLONE_DIR"
	HookAttrWorktreePath = "CODEHERD_WORKTREE_PATH"
	HookAttrSessionName  = "CODEHERD_SESSION_NAME"
)

func FlattenBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// SessionName returns the canonical tmux session name.
// profile == "" gives "<project>-<branch>" (backward-compatible).
// profile != "" gives "<profile>-<project>-<branch>" for tmux uniqueness.
func SessionName(profile, project, branch string) string {
	if profile == "" {
		return project + "-" + FlattenBranch(branch)
	}
	return profile + "-" + project + "-" + FlattenBranch(branch)
}

// ShellSessionName returns the tmux session name for the shell variant,
// carrying the same profile prefix as SessionName.
func ShellSessionName(profile, project, branch string) string {
	return SessionName(profile, project, branch) + "~sh"
}

func CloneDir(projectsDir, repoPath string) string {
	return filepath.Join(projectsDir, repoPath)
}

func WorktreesRoot(projectsDir, repoPath string) string {
	return CloneDir(projectsDir, repoPath) + "__worktrees"
}

func WorktreePath(projectsDir, repoPath, branch string) string {
	return filepath.Join(WorktreesRoot(projectsDir, repoPath), FlattenBranch(branch))
}
