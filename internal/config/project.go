package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ProjectConfig holds per-project settings.
type ProjectConfig struct {
	Repo          string      `toml:"repo"`
	DefaultBranch string      `toml:"default_branch"`
	Files         []string    `toml:"files"`
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

	host, path, err := splitRepoURL(repoURL)
	if err != nil {
		return "", err
	}

	// Clean up path
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	if path == "" {
		return "", fmt.Errorf("repo URL %q has no path", repoURL)
	}

	return host + "/" + path, nil
}

// splitRepoURL returns the host and path of an SCP-style remote
// (git@host:path — a colon but no scheme) or of a scheme://host/path URL.
func splitRepoURL(repoURL string) (host, path string, err error) {
	if strings.Contains(repoURL, ":") && !strings.Contains(repoURL, "://") {
		hostPart, path, _ := strings.Cut(repoURL, ":")
		// Drop the user from user@host.
		if _, after, ok := strings.Cut(hostPart, "@"); ok {
			return after, path, nil
		}
		return hostPart, path, nil
	}

	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("parsing repo URL %q: %w", repoURL, err)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("repo URL %q has no host", repoURL)
	}
	return u.Hostname(), u.Path, nil
}
