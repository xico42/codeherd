package git

import "testing"

func TestParseWorktreePorcelain(t *testing.T) {
	input := `worktree /home/user/projects/myapp
HEAD abc123
branch refs/heads/main

worktree /home/user/projects/myapp__worktrees/feature
HEAD def456
branch refs/heads/feature

worktree /home/user/projects/myapp__worktrees/detached
HEAD ghi789
detached

`
	got := parseWorktreePorcelain(input)

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].Path != "/home/user/projects/myapp" || got[0].Branch != "main" {
		t.Errorf("entry 0: %+v", got[0])
	}
	if got[1].Path != "/home/user/projects/myapp__worktrees/feature" || got[1].Branch != "feature" {
		t.Errorf("entry 1: %+v", got[1])
	}
	if got[2].Branch != "" {
		t.Errorf("entry 2 should have empty branch for detached HEAD, got %q", got[2].Branch)
	}
}

func TestParseWorktreePorcelain_empty(t *testing.T) {
	got := parseWorktreePorcelain("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// TestParseWorktreePorcelain_noTrailingNewline exercises the tail-append path
// where the last entry is not followed by a blank line.
func TestParseWorktreePorcelain_noTrailingNewline(t *testing.T) {
	input := "worktree /home/user/projects/myapp\nHEAD abc123\nbranch refs/heads/main"
	got := parseWorktreePorcelain(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Path != "/home/user/projects/myapp" || got[0].Branch != "main" {
		t.Errorf("unexpected entry: %+v", got[0])
	}
}

func TestParseWorktreePorcelain_detachedFlag(t *testing.T) {
	input := "worktree /p/myapp__worktrees/detached\nHEAD ghi789\ndetached\n\n"
	got := parseWorktreePorcelain(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if !got[0].Detached {
		t.Errorf("expected Detached=true for detached HEAD entry")
	}
	if got[0].Branch != "" {
		t.Errorf("expected empty Branch for detached HEAD, got %q", got[0].Branch)
	}
}

func TestParseRemoteBranches(t *testing.T) {
	input := "origin/main\norigin/HEAD\norigin/feature/login\nupstream/bugfix\n"
	got := parseRemoteBranches(input)
	want := []RemoteBranch{
		{Remote: "origin", Branch: "main", Ref: "origin/main"},
		{Remote: "origin", Branch: "feature/login", Ref: "origin/feature/login"},
		{Remote: "upstream", Branch: "bugfix", Ref: "upstream/bugfix"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRef(t *testing.T) {
	remotes := []string{"origin", "upstream"}
	cases := []struct {
		ref            string
		remote, branch string
		explicit       bool
	}{
		{"feat-x", "origin", "feat-x", false},
		{"feature/login", "origin", "feature/login", false},
		{"origin/feat-x", "origin", "feat-x", true},
		{"upstream/feature/login", "upstream", "feature/login", true},
		{"notaremote/x", "origin", "notaremote/x", false},
	}
	for _, tc := range cases {
		gotR, gotB, gotE := ParseRef(remotes, tc.ref)
		if gotR != tc.remote || gotB != tc.branch || gotE != tc.explicit {
			t.Errorf("ParseRef(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.ref, gotR, gotB, gotE, tc.remote, tc.branch, tc.explicit)
		}
	}
}
