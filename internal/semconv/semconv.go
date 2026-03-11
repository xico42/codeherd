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
