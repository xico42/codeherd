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

func SessionName(project, branch string) string {
	return project + "-" + FlattenBranch(branch)
}

func ShellSessionName(project, branch string) string {
	return SessionName(project, branch) + "~sh"
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
