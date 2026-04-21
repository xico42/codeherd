package config_test

import (
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func TestRepoPath(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "scp-style SSH",
			url:  "git@github.com:user/myapp.git",
			want: "github.com/user/myapp",
		},
		{
			name: "scp-style SSH without .git",
			url:  "git@github.com:user/myapp",
			want: "github.com/user/myapp",
		},
		{
			name: "scp-style SSH nested path",
			url:  "git@gitlab.com:corp/group/api.git",
			want: "gitlab.com/corp/group/api",
		},
		{
			name: "SSH URL-style",
			url:  "ssh://git@github.com/user/myapp.git",
			want: "github.com/user/myapp",
		},
		{
			name: "SSH URL-style without .git",
			url:  "ssh://git@github.com/user/myapp",
			want: "github.com/user/myapp",
		},
		{
			name: "HTTPS",
			url:  "https://github.com/user/myapp.git",
			want: "github.com/user/myapp",
		},
		{
			name: "HTTPS without .git",
			url:  "https://github.com/user/myapp",
			want: "github.com/user/myapp",
		},
		{
			name: "HTTPS nested path",
			url:  "https://gitlab.com/corp/group/api.git",
			want: "gitlab.com/corp/group/api",
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "plain string no host",
			url:     "not-a-url",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.RepoPath(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RepoPath(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("RepoPath(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// TestRepoPath_NoAtSign exercises the SCP-style branch where there is no "@"
// in the host part (host = hostPart directly, not after "@").
func TestRepoPath_NoAtSign(t *testing.T) {
	// "github.com:user/myapp.git" — SCP-style without user@
	got, err := config.RepoPath("github.com:user/myapp.git")
	if err != nil {
		t.Fatalf("RepoPath() error = %v", err)
	}
	if got != "github.com/user/myapp" {
		t.Errorf("RepoPath() = %q, want %q", got, "github.com/user/myapp")
	}
}

// TestRepoPath_EmptyPathAfterHost exercises the "no path" error branch via a
// URL that has a host but an empty path (after stripping / and .git).
func TestRepoPath_EmptyPathAfterHost(t *testing.T) {
	// An https URL with only a host and a .git suffix that empties the path.
	_, err := config.RepoPath("https://github.com/.git")
	if err == nil {
		t.Fatal("RepoPath() = nil, want error for empty path")
	}
}

// TestRepoPath_NoHost exercises the "no host" error branch for an https URL
// that parses successfully but has no host component.
func TestRepoPath_NoHost(t *testing.T) {
	// A URL with a scheme but no host (e.g. "https:///path/to/repo").
	_, err := config.RepoPath("https:///path/to/repo")
	if err == nil {
		t.Fatal("RepoPath() = nil, want error for URL with no host")
	}
}

// TestRepoPath_ParseError exercises the url.Parse error branch.
func TestRepoPath_ParseError(t *testing.T) {
	// "://foo" triggers a parse error because it lacks a protocol scheme.
	_, err := config.RepoPath("://foo")
	if err == nil {
		t.Fatal("RepoPath() = nil, want error for unparseable URL")
	}
}
