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
	TmuxOptionBranch        = "@codeherd_branch"

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

// Environment variable names exposed to hook commands and to the agent/shell
// command running inside a codeherd tmux session. The HookAttr* aliases are
// kept for callers that pass these as hook attributes; agent-session code
// references them directly as env var names.
const (
	HookAttrProject      = "CODEHERD_PROJECT"
	HookAttrBranch       = "CODEHERD_BRANCH"
	HookAttrRepo         = "CODEHERD_REPO"
	HookAttrCloneDir     = "CODEHERD_CLONE_DIR"
	HookAttrWorktreePath = "CODEHERD_WORKTREE_PATH"
	HookAttrSessionName  = "CODEHERD_SESSION_NAME"

	// EnvProfile is only exported in session env, not as a hook attribute.
	EnvProfile = "CODEHERD_PROFILE"
)

func FlattenBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// WorktreeIdentityBranch returns the stable identity branch for a worktree —
// the branch the worktree was created for, regardless of where HEAD points now.
// Feeding it into SessionName recovers the session's frozen canonical name.
//
// Worktrees under "<clone>__worktrees/" are named FlattenBranch(branch), so the
// directory base name is the (flattened) identity. The main clone dir is named
// after the repo rather than a branch, so its identity is the configured default
// branch, falling back to the live branch when no default is configured.
func WorktreeIdentityBranch(path, cloneDir, defaultBranch, liveBranch string) string {
	if path == cloneDir {
		if defaultBranch != "" {
			return defaultBranch
		}
		return liveBranch
	}
	return filepath.Base(path)
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
