package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ProjectConfig holds per-project settings.
type ProjectConfig struct {
	Repo          string      `toml:"repo"           validate:"omitempty"`
	DefaultBranch string      `toml:"default_branch" validate:"omitempty"`
	Files         []string    `toml:"files"          validate:"omitempty"`
	Hooks         HooksConfig `toml:"hooks"`
}

// HooksConfig holds per-project lifecycle hook commands.
type HooksConfig struct {
	PreClone     string `toml:"pre-clone"`
	PostClone    string `toml:"post-clone"`
	PreWorktree  string `toml:"pre-worktree"`
	PostWorktree string `toml:"post-worktree"`
	PreCopy      string `toml:"pre-copy"`
	PostCopy     string `toml:"post-copy"`
	PreTemplate  string `toml:"pre-template"`
	PostTemplate string `toml:"post-template"`
	PreSession   string `toml:"pre-session"`
	PostSession  string `toml:"post-session"`
}

// RepoPath parses a git remote URL and returns the directory path.
// Examples:
//
//	"git@github.com:user/myapp.git"       -> "github.com/user/myapp"
//	"git@gitlab.com:corp/group/api.git"   -> "gitlab.com/corp/group/api"
//	"https://github.com/user/myapp.git"   -> "github.com/user/myapp"
//	"ssh://git@github.com/user/myapp.git" -> "github.com/user/myapp"
func RepoPath(repoURL string) (string, error) {
	if repoURL == "" {
		return "", fmt.Errorf("empty repo URL")
	}

	var host, path string

	// SCP-style: git@host:path (no scheme, has colon but not ://)
	if strings.Contains(repoURL, ":") && !strings.Contains(repoURL, "://") {
		at := strings.Index(repoURL, ":")
		hostPart := repoURL[:at]
		path = repoURL[at+1:]

		// Extract host from user@host
		if idx := strings.Index(hostPart, "@"); idx >= 0 {
			host = hostPart[idx+1:]
		} else {
			host = hostPart
		}
	} else {
		u, err := url.Parse(repoURL)
		if err != nil {
			return "", fmt.Errorf("parsing repo URL %q: %w", repoURL, err)
		}
		if u.Host == "" {
			return "", fmt.Errorf("repo URL %q has no host", repoURL)
		}
		host = u.Hostname()
		path = u.Path
	}

	// Clean up path
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	if path == "" {
		return "", fmt.Errorf("repo URL %q has no path", repoURL)
	}

	return host + "/" + path, nil
}
