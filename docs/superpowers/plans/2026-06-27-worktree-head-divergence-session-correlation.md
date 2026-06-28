# Worktree HEAD-Divergence Session Correlation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep a codeherd worktree's tmux session visible in the TUI (and `ch list worktree`) when its HEAD diverges from the branch it was created for — a rebase in progress (detached HEAD) or a different branch checked out.

**Architecture:** Correlate worktrees to sessions by a *stable identity branch* instead of the live git branch. For `__worktrees/<branch>` directories that identity is the folder base name (which equals the flattened branch); for the main clone dir it is `config.DefaultBranch`. This reproduces the session's frozen `@codeherd_canonical_name` exactly and retroactively. Display the identity branch plus a live-state hint (`detached` / `on <branch>`), using a new `@codeherd_branch` tmux option for the exact pretty name with a folder/config fallback.

**Tech Stack:** Go, tmux (via `internal/tmux` typed wrapper), Bubble Tea v2 TUI.

## Global Constraints

- Aggregate test coverage must stay ≥ 80%; `make check` (coverage → integration → lint → build) must pass before completion.
- Every closed-set string already has a typed Go const; reuse existing `semconv` consts, do not introduce raw string literals for tmux options.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- `FlattenBranch` (slash → dash) is idempotent; rely on that rather than reversing it.
- Run a single package's tests with `go test ./internal/<pkg>/...`.

---

### Task 1: `semconv` — identity-branch helper and `@codeherd_branch` const

**Files:**
- Modify: `internal/semconv/semconv.go` (const block at lines 8-27; helpers near `FlattenBranch` at line 59)
- Test: `internal/semconv/semconv_test.go`

**Interfaces:**
- Produces: `semconv.TmuxOptionBranch = "@codeherd_branch"` (string const)
- Produces: `semconv.WorktreeIdentityBranch(path, cloneDir, defaultBranch, liveBranch string) string`

- [ ] **Step 1: Write the failing test**

Add to `internal/semconv/semconv_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/semconv/...`
Expected: FAIL — `undefined: semconv.WorktreeIdentityBranch` and `semconv.TmuxOptionBranch`.

- [ ] **Step 3: Write minimal implementation**

In `internal/semconv/semconv.go`, add the const inside the existing `const (...)` block (after `TmuxOptionProfile`):

```go
	TmuxOptionProfile       = "@codeherd_profile"
	TmuxOptionBranch        = "@codeherd_branch"
```

Then add the helper just below `FlattenBranch` (after line 61). `path/filepath` is already imported:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/semconv/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/semconv/semconv.go internal/semconv/semconv_test.go
git commit -m "feat(semconv): add WorktreeIdentityBranch helper and @codeherd_branch option"
```

---

### Task 2: `worktree` — parse detached HEAD and correlate by identity branch

**Files:**
- Modify: `internal/worktree/worktree.go` (`WorktreeInfo` lines 28-32; `ListEntry` lines 34-40; `parseWorktreePorcelain` lines 223-246; `Service.List` loop lines 585-599)
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Consumes: `semconv.WorktreeIdentityBranch` (Task 1)
- Produces: `WorktreeInfo.Detached bool`, `ListEntry.Detached bool`

- [ ] **Step 1: Write the failing tests**

Add to `internal/worktree/worktree_test.go`:

```go
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

func TestService_List_cloneDirDetachedUsesDefaultBranch(t *testing.T) {
	// The clone-dir worktree is detached (e.g. mid-rebase): no live branch.
	// Correlation must fall back to config DefaultBranch ("main") so the
	// session is still found and Detached is surfaced.
	git := &mockGit{
		listResult: []WorktreeInfo{
			{Path: "", Branch: "", Detached: true}, // Path filled in below
		},
	}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{exitCode: 0}) // exit 0 = session exists
	git.listResult[0].Path = cloneDirPath(tmpDir)
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Session == "" {
		t.Errorf("expected session populated via DefaultBranch identity, got empty")
	}
	if !entries[0].Detached {
		t.Errorf("expected Detached=true on entry")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/worktree/...`
Expected: FAIL — `WorktreeInfo` has no field `Detached`; `ListEntry` has no field `Detached`.

- [ ] **Step 3: Implement the struct fields and parsing**

In `internal/worktree/worktree.go`, extend `WorktreeInfo` (lines 28-32):

```go
// WorktreeInfo holds data from a single git worktree entry.
type WorktreeInfo struct {
	Path     string
	Branch   string // empty if detached HEAD
	Detached bool   // true when HEAD is detached (e.g. rebase in progress)
}
```

Extend `ListEntry` (lines 34-40):

```go
// ListEntry is one row in the worktree list output.
type ListEntry struct {
	Project  string
	Branch   string
	Path     string
	Session  string // "<name>-<branch> (running)" or ""
	Detached bool   // true when the worktree's HEAD is detached
}
```

In `parseWorktreePorcelain`, add a case for the `detached` line (inside the `switch` at lines 229-240, alongside the `branch ` case):

```go
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			current.Detached = true
```

- [ ] **Step 4: Implement the `Service.List` identity correlation**

Replace the loop body in `Service.List` (lines 585-599) with:

```go
		for _, wt := range worktrees {
			identity := semconv.WorktreeIdentityBranch(wt.Path, cd, p.DefaultBranch, wt.Branch)
			session := ""
			if identity != "" {
				candidate := semconv.SessionName("", name, identity)
				if running, _ := s.tmux.HasSession(candidate); running {
					session = candidate + " (running)"
				}
			}
			entries = append(entries, ListEntry{
				Project:  name,
				Branch:   wt.Branch,
				Path:     wt.Path,
				Session:  session,
				Detached: wt.Detached,
			})
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/worktree/...`
Expected: PASS (new tests pass; `TestParseWorktreePorcelain`, `TestService_List_withRunningSession`, and the other existing List tests still pass — for `__worktrees/` paths the identity equals the branch, so behavior is unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): track detached HEAD and correlate sessions by identity branch"
```

---

### Task 3: `tmux` — expose the raw branch from `ListSessions`

**Files:**
- Modify: `internal/tmux/client.go` (`SessionRecord` lines 9-19; `ListSessions` lines 227-260)
- Test: `internal/tmux/client_test.go`

**Interfaces:**
- Produces: `tmux.SessionRecord.Branch string` (populated from `@codeherd_branch`, `""` when the option is unset)

- [ ] **Step 1: Write the failing test**

Add to `internal/tmux/client_test.go`:

```go
func TestClient_ListSessions_readsBranchOption(t *testing.T) {
	// 9-field line: branch populated as the final field.
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-01-01T00:00:00Z\twork\tfeature/login\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Branch != "feature/login" {
		t.Errorf("Branch = %q, want feature/login", records[0].Branch)
	}
}

func TestClient_ListSessions_missingBranchIsEmpty(t *testing.T) {
	// Old 8-field line (pre-upgrade session): Branch must default to "".
	line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-01-01T00:00:00Z\twork\n"
	r := &mockRunner{exitCode: 0, stdout: line}
	c := tmux.NewClient(r)
	records, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if records[0].Branch != "" {
		t.Errorf("Branch = %q, want empty", records[0].Branch)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/...`
Expected: FAIL — `records[0].Branch` undefined (`SessionRecord` has no `Branch` field).

- [ ] **Step 3: Implement the field and parsing**

In `internal/tmux/client.go`, add to `SessionRecord` (after `Profile`, line 18):

```go
	Profile       string // @codeherd_profile, "" when unset
	Branch        string // @codeherd_branch — raw branch the session was created for, "" when unset
}
```

In `ListSessions`, append `@codeherd_branch` to the format string (line 228):

```go
	format := "#{session_id}\t#{session_name}\t#{@codeherd_canonical_name}\t#{@codeherd_session_type}\t#{@codeherd_status}\t#{@codeherd_annotation}\t#{@codeherd_started_at}\t#{@codeherd_profile}\t#{@codeherd_branch}"
```

Change the split width from 8 to 9 (lines 244-247):

```go
		fields := strings.SplitN(line, "\t", 9)
		for len(fields) < 9 {
			fields = append(fields, "")
		}
```

Add `Branch` to the appended record (after `Profile`, line 256):

```go
			Profile:       fields[7],
			Branch:        fields[8],
		})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/...`
Expected: PASS (new tests pass; existing `TestClient_ListSessions_ok` and the profile tests still pass — their 8-field lines parse with `Branch == ""`).

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): surface @codeherd_branch in SessionRecord"
```

---

### Task 4: `session` — stamp `@codeherd_branch` at session start

**Files:**
- Modify: `internal/session/session.go` (`SetOption` block lines 144-150)
- Test: `internal/session/session_test.go` (`TestStart_OK` at lines 96-125)

**Interfaces:**
- Consumes: `semconv.TmuxOptionBranch` (Task 1)
- Produces: a `set-option @codeherd_branch <req.Branch>` tmux call during `Start`

- [ ] **Step 1: Write the failing test**

Add to `internal/session/session_test.go`:

```go
func TestStart_StampsBranchOption(t *testing.T) {
	r := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1},                 // list-sessions → empty
		{exitCode: 0, stdout: "$1\n"}, // new-session → ok
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0},                 // set-option branch
	}}
	tc := tmux.NewClient(r)
	svc := session.NewService(tc, &mockHook{})

	if _, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature/login",
		Path:    t.TempDir(),
		Cmd:     "claude",
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	found := false
	for _, call := range r.calls {
		// SetOption runs: ("set-option", "-t", <name>, <option>, <value>)
		if len(call) >= 5 && call[0] == "set-option" &&
			call[3] == semconv.TmuxOptionBranch && call[4] == "feature/login" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a set-option %s feature/login call, calls: %v",
			semconv.TmuxOptionBranch, r.calls)
	}
}
```

Add the imports this test needs to the existing `import (...)` block in `internal/session/session_test.go` if missing: `"github.com/xico42/codeherd/internal/semconv"`. (The `tmux` and `session` packages are already imported by the file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run TestStart_StampsBranchOption -v`
Expected: FAIL — no `set-option @codeherd_branch` call is made.

If the failure is instead an argument-index mismatch in the assertion, print `r.calls`, read the real `SetOption` arg order, and correct the indices in the test before proceeding.

- [ ] **Step 3: Implement the stamp**

In `internal/session/session.go`, add one `SetOption` call after the `session_type` option (line 147), before the profile block:

```go
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionSessionType, req.Type)
	_ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionBranch, req.Branch)
	if req.Profile != "" {
```

- [ ] **Step 4: Update the existing `TestStart_OK` call-count expectation**

`TestStart_OK` (lines 96-125) now makes one extra tmux call. Add a `{exitCode: 0}` response for the new branch set-option and bump the expected count.

Change the `responses` slice to include the branch set-option:

```go
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1},                 // list-sessions → no sessions (exit 1 = empty)
		{exitCode: 0, stdout: "$1\n"}, // new-session → ok, returns session_id via -P -F
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0},                 // set-option branch
	}}
```

Change the count assertion (line 122-124):

```go
	if len(r2.calls) != 7 {
		t.Errorf("expected 7 tmux calls, got %d: %v", len(r2.calls), r2.calls)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/...`
Expected: PASS (both `TestStart_StampsBranchOption` and the updated `TestStart_OK`).

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): stamp @codeherd_branch on session start"
```

---

### Task 5: `tui` — correlate by identity branch and compute the head hint

**Files:**
- Modify: `internal/tui/items.go` (`Item` lines 18-32; `refreshResult` lines 41-49; `wtEntry` lines 51-55; `buildItems` lines 68-100)
- Test: `internal/tui/items_test.go`

**Interfaces:**
- Consumes: `semconv.WorktreeIdentityBranch`, `semconv.SessionName`, `semconv.FlattenBranch` (Task 1 and existing)
- Produces: `Item.HeadHint string`; `wtEntry.detached bool`; `refreshResult.defaultBranches map[string]string`; `refreshResult.sessionBranch map[string]string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/items_test.go`:

```go
func TestBuildItems_detachedWorktreeStillCorrelates(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "", path: "/p/myapp__worktrees/feature", detached: true},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$1", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{"myapp-feature": "feature"},
	}

	items := buildItems(data)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$1" {
		t.Errorf("detached worktree lost its agent session: %+v", it)
	}
	if it.HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature (from @codeherd_branch)", it.Branch)
	}
}

func TestBuildItems_otherBranchCheckedOut(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "hotfix", path: "/p/myapp__worktrees/feature"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$2", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{}, // pre-upgrade session: no raw branch
	}

	items := buildItems(data)
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$2" {
		t.Errorf("worktree with other branch lost its session: %+v", it)
	}
	if it.HeadHint != "on hotfix" {
		t.Errorf("HeadHint = %q, want 'on hotfix'", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature (folder fallback)", it.Branch)
	}
}

func TestBuildItems_cloneDirCorrelatesViaDefaultBranch(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "", path: "/p/myapp", detached: true},
		},
		agentSessions: map[string]agentInfo{
			"myapp-main": {sessionID: "$3", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{},
	}

	items := buildItems(data)
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$3" {
		t.Errorf("clone-dir worktree lost its session: %+v", it)
	}
	if it.HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", it.HeadHint)
	}
	if !it.IsMain {
		t.Errorf("expected IsMain=true for clone dir")
	}
	if it.Branch != "main" {
		t.Errorf("Branch = %q, want main (from config default)", it.Branch)
	}
}

func TestBuildItems_normalWorktreeNoHint(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "feature", path: "/p/myapp__worktrees/feature"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$4", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{"myapp-feature": "feature"},
	}

	items := buildItems(data)
	it := items[0].(Item)
	if it.HeadHint != "" {
		t.Errorf("HeadHint = %q, want empty for non-diverged worktree", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature", it.Branch)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestBuildItems -v`
Expected: FAIL — `wtEntry` has no field `detached`; `refreshResult` has no `defaultBranches` / `sessionBranch`; `Item` has no `HeadHint`.

- [ ] **Step 3: Add the new struct fields**

In `internal/tui/items.go`, add to `Item` (after `IsMain`, line 31):

```go
	IsMain   bool   // true for the main worktree (clone dir)
	HeadHint string // "detached" / "on <branch>" when HEAD diverged from identity, else ""
}
```

Add to `refreshResult` (lines 41-49):

```go
type refreshResult struct {
	worktrees       []wtEntry
	agentSessions   map[string]agentInfo // keyed by canonical session name
	shellSessions   map[string]string    // canonical session name → tmux session_id
	sessionBranch   map[string]string    // canonical session name → raw branch (@codeherd_branch)
	projects        []projEntry
	cloneDirs       map[string]string // project name -> clone dir path
	defaultBranches map[string]string // project name -> config.DefaultBranch
	profile         string            // active profile, "" when profile mode is off
}
```

Add to `wtEntry` (lines 51-55):

```go
type wtEntry struct {
	project  string
	branch   string
	path     string
	detached bool
}
```

- [ ] **Step 4: Rewrite the `buildItems` worktree loop**

Replace the worktree loop body in `buildItems` (lines 74-100) with:

```go
	for _, wt := range data.worktrees {
		projectHasWorktree[wt.project] = true

		cloneDir := data.cloneDirs[wt.project]
		isMain := cloneDir == wt.path
		defaultBranch := data.defaultBranches[wt.project]

		identity := semconv.WorktreeIdentityBranch(wt.path, cloneDir, defaultBranch, wt.branch)
		sessionName := semconv.SessionName(data.profile, wt.project, identity)

		// Determine whether HEAD has diverged from the identity branch.
		identityFlat := semconv.FlattenBranch(identity)
		displayBranch := wt.branch
		headHint := ""
		switch {
		case wt.detached:
			headHint = "detached"
		case wt.branch != "" && semconv.FlattenBranch(wt.branch) != identityFlat:
			headHint = "on " + wt.branch
		}
		if headHint != "" {
			// Diverged: prefer the session's recorded raw branch, then config
			// (clone dir), then the folder name.
			if raw := data.sessionBranch[sessionName]; raw != "" {
				displayBranch = raw
			} else if isMain && defaultBranch != "" {
				displayBranch = defaultBranch
			} else {
				displayBranch = identityFlat
			}
		}

		shellID := data.shellSessions[sessionName]
		item := Item{
			Project:        wt.project,
			Branch:         displayBranch,
			Path:           wt.path,
			HasShell:       shellID != "",
			ShellSessionID: shellID,
			IsMain:         isMain,
			HeadHint:       headHint,
		}

		if agent, ok := data.agentSessions[sessionName]; ok {
			item.Group = groupAgent
			item.HasAgent = true
			item.AgentStatus = agent.status
			item.AgentSessionID = agent.sessionID
			item.Annotation = agent.annotation
		} else {
			item.Group = groupWorktree
		}

		items = append(items, item)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run TestBuildItems -v`
Expected: PASS (the four new tests, plus the existing `TestBuildItems_groupOrdering` — its `cloneDirs`/`defaultBranches` maps are nil, which read as empty, so non-main worktrees still correlate by folder name and the live branch equals the identity).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/items.go internal/tui/items_test.go
git commit -m "feat(tui): correlate worktrees to sessions by identity branch with head hint"
```

---

### Task 6: `tui` — feed real divergence data into the refresh

**Files:**
- Modify: `internal/tui/model.go` (`refreshCmd` lines 413-491)

**Interfaces:**
- Consumes: `worktree.ListEntry.Detached` (Task 2), `tmux.SessionRecord.Branch` (Task 3), `refreshResult.{detached fields, defaultBranches, sessionBranch}` (Task 5), `cfg.Projects[name].DefaultBranch`

- [ ] **Step 1: Initialize the new maps**

In `refreshCmd` (lines 414-418), extend the `refreshResult` literal:

```go
		data := refreshResult{
			agentSessions:   make(map[string]agentInfo),
			shellSessions:   make(map[string]string),
			sessionBranch:   make(map[string]string),
			defaultBranches: make(map[string]string),
			profile:         activeProfile,
		}
```

- [ ] **Step 2: Carry the detached flag from worktree entries**

In the worktrees loop (lines 424-430), add `detached`:

```go
				for _, e := range entries {
					data.worktrees = append(data.worktrees, wtEntry{
						project:  e.Project,
						branch:   e.Branch,
						path:     e.Path,
						detached: e.Detached,
					})
				}
```

- [ ] **Step 3: Record each session's raw branch**

In the sessions loop, populate `sessionBranch` for both session types. Add this line immediately after the `switch r.SessionType {` block closes (after line 455, inside the `for _, r := range records` loop):

```go
					switch r.SessionType {
					case semconv.SessionTypeShell:
						data.shellSessions[r.CanonicalName] = r.ID
					case semconv.SessionTypeAgent:
						data.agentSessions[r.CanonicalName] = agentInfo{
							sessionID:  r.ID,
							status:     r.Status,
							annotation: r.Annotation,
						}
					}
					data.sessionBranch[r.CanonicalName] = r.Branch
```

- [ ] **Step 4: Populate default branches alongside clone dirs**

In the clone-dirs block (lines 476-483), also record `DefaultBranch`:

```go
		if cfg != nil {
			data.cloneDirs = make(map[string]string)
			for name, p := range cfg.Projects {
				data.defaultBranches[name] = p.DefaultBranch
				if rp, err := config.RepoPath(p.Repo); err == nil {
					data.cloneDirs[name] = semconv.CloneDir(cfg.Defaults.ProjectsDir, rp)
				}
			}
		}
```

- [ ] **Step 5: Build and run the package tests**

Run: `go build ./... && go test ./internal/tui/...`
Expected: PASS — compiles, and all `internal/tui` tests pass. (`refreshCmd` builds a `tea.Cmd` closure over live services and is covered indirectly; the correlation logic it feeds is unit-tested in Task 5.)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): feed detached state, default branches, and raw branch into refresh"
```

---

### Task 7: `tui` — render the head hint in the list delegate

**Files:**
- Modify: `internal/tui/delegate.go` (line-1 construction, lines 44-58)
- Test: `internal/tui/delegate_test.go`

**Interfaces:**
- Consumes: `Item.HeadHint` (Task 5)

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/delegate_test.go`:

```go
func TestDelegate_Render_headHint(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{
		Item{Project: "myapp", Branch: "feature", Group: groupAgent, HasAgent: true,
			AgentStatus: semconv.StatusRunning, HeadHint: "detached"},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "feature") {
		t.Errorf("render missing branch, got: %q", out)
	}
	if !strings.Contains(out, "detached") {
		t.Errorf("render missing head hint, got: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestDelegate_Render_headHint -v`
Expected: FAIL — output does not contain `detached`.

- [ ] **Step 3: Append the hint to line 1**

In `internal/tui/delegate.go`, extend the line-1 construction (lines 45-50) to add the hint before styling:

```go
	// Line 1: project / branch (+ head-state hint when HEAD diverged)
	var line1 string
	if item.Branch != "" {
		line1 = item.Project + " / " + item.Branch
	} else {
		line1 = item.Project
	}
	if item.HeadHint != "" {
		line1 += " (" + item.HeadHint + ")"
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestDelegate_Render_headHint -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/delegate.go internal/tui/delegate_test.go
git commit -m "feat(tui): render HEAD-divergence hint in the worktree list"
```

---

### Task 8: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full check suite**

Run: `make check`
Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds.

- [ ] **Step 2: If coverage dropped below 80%**

Identify the uncovered new lines with:

```bash
go test ./... -coverprofile=/tmp/cover.out && go tool cover -func=/tmp/cover.out | grep -E "items.go|worktree.go|client.go|session.go|delegate.go"
```

Add focused unit tests for any uncovered branch in the code added by Tasks 1-7 (e.g. the `isMain && defaultBranch == ""` fallback path in `buildItems`, or the clone-dir-no-default identity path in `WorktreeIdentityBranch`). Re-run `make check`.

- [ ] **Step 3: Final commit (only if Step 2 added tests)**

```bash
git add -A
git commit -m "test: cover remaining HEAD-divergence correlation branches"
```

---

## Self-Review

**Spec coverage:**
- Root cause (live-branch correlation key) → Tasks 2, 5 replace it with identity-branch correlation. ✓
- Folder-name correlation for `__worktrees/` → `WorktreeIdentityBranch` (Task 1), used in Tasks 2 and 5. ✓
- Clone-dir correlation via `config.DefaultBranch` → Task 1 helper + Task 5/6 wiring; tested in Tasks 2 and 5. ✓
- Detached-HEAD parsing → Task 2 (`WorktreeInfo.Detached`, porcelain `detached` line). ✓
- `@codeherd_branch` stamped at creation → Task 4; read back → Task 3; used for display → Task 5. ✓
- Live-state hint (`detached` / `on <branch>`) → Task 5 logic + Task 7 render. ✓
- Display fallback order (raw branch → config → folder) → Task 5 `buildItems`. ✓
- `ch list worktree` consistency → Task 2 `Service.List`. ✓
- Retroactivity (pre-upgrade sessions, missing options) → Tasks 3 (missing-option test) and 5 (`sessionBranch` empty → folder fallback). ✓
- Item action safety (attach by session ID, resolve by `WorktreePath`) → unchanged; no task alters those paths. ✓
- Coverage ≥ 80% / `make check` → Task 8. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code. ✓

**Type consistency:** `WorktreeIdentityBranch(path, cloneDir, defaultBranch, liveBranch)` signature identical across Tasks 1, 2, 5. `Item.HeadHint`, `wtEntry.detached`, `refreshResult.sessionBranch`/`defaultBranches`, `SessionRecord.Branch`, `ListEntry.Detached`, `WorktreeInfo.Detached` referenced consistently. `semconv.TmuxOptionBranch` used in Tasks 1, 4. ✓
