# Checkout a Remote Branch into a New Worktree — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users fetch and check out a remote branch into a new tracking worktree (for PR review), and make every worktree *creation* start from an up-to-date source.

**Architecture:** Extend `internal/worktree`'s `WorktreeRunner` with git fetch / remote-listing / tracking-add primitives, add `Service` methods (`NewTracking`, `RemoteBranches`, `ListRemoteBranches`) plus a `freshenStartPoint` helper that all creation paths use. Surface it on the CLI via a `--track <ref>` flag on `create worktree`, and in the TUI via a new remote-branch picker (`r`) that feeds the existing create-worktree form.

**Tech Stack:** Go, Cobra (CLI), Bubble Tea v2 + Bubbles `list` + Huh v2 (TUI), git via `os/exec`.

## Global Constraints

- Go module `github.com/xico42/codeherd`; build/test via the `Makefile`.
- New code must keep aggregate coverage ≥ 80%. Final gate: `make check` (coverage → integration tests → lint → build).
- Follow existing patterns: struct-per-command CLI, `WorktreeRunner` interface mocked in tests, pure parse helpers tested directly (mirror `parseWorktreePorcelain`).
- Default remote is `origin`, hardcoded (no config knob).
- `--track` and `--from` are mutually exclusive.
- Fetch/fast-forward during creation is **best-effort**: failures are non-fatal and never abort creation (except `NewTracking`'s primary fetch of the requested branch, which is fatal).
- The new worktree must never lose un-pushed local commits: fast-forward is `--ff-only`, and when a local source branch exists the new branch is based off that (possibly fast-forwarded) local branch, not the remote-tracking ref.

---

## File Structure

- `internal/worktree/worktree.go` — extend `WorktreeRunner`, `RealWorktreeRunner`, add `RemoteBranch` type, `NewResult.Branch`, `ErrLocalBranchExists`, pure helpers `parseRef`/`parseRemoteBranches`, `freshenStartPoint`, and `Service` methods `NewTracking`/`RemoteBranches`/`ListRemoteBranches`. Rewire `New`/`NewFrom`.
- `internal/worktree/worktree_test.go` — extend `mockGit`; update affected tests; add new unit tests.
- `cmd/worktree.go` — `--track` flag, optional positional, mutual exclusion, route to `NewTracking`, use `NewResult.Branch`.
- `cmd/worktree_test.go` (or existing cmd tests) — CLI wiring tests.
- `cmd/completion.go` — `completeRemoteBranches` + `completionRemoteBrancher` var.
- `cmd/completion_test.go` — completion test.
- `internal/tui/keys.go` — replace the `Refresh` binding with a `Remote` binding (`r`).
- `internal/tui/remote_picker.go` (new) — `remotePickerModel`, `remoteBranchItem`.
- `internal/tui/remote_picker_test.go` (new) — picker tests.
- `internal/tui/model.go` — `screenRemotePicker`, `remoteBranchesMsg`, `remotePicker` field, `startRemotePicker`/`fetchRemoteBranchesCmd`/`updateRemotePicker`/`showTrackForm`, View + key dispatch.
- `internal/tui/form.go` — `formContext.tracksRef`/`branch`, `formModel.tracksRef`, prefill + `NewTracking` routing.
- `internal/tui/form_test.go`, `internal/tui/model_test.go` — TUI tests.
- `internal/worktree/integration_test.go` (new) — end-to-end `//go:build integration` test.

---

## Task 1: Extend WorktreeRunner with fetch / remote / tracking primitives

Adds the git primitives and types with no behavior change to existing flows. Existing tests must still compile and pass (so `mockGit` gains the new methods returning zero values).

**Files:**
- Modify: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Produces:
  - `type RemoteBranch struct { Remote, Branch, Ref string }`
  - `NewResult` gains field `Branch string`
  - `var ErrLocalBranchExists = errors.New("local branch already exists")`
  - `WorktreeRunner` methods: `Fetch(cloneDir, remote, branch string) error`, `FetchAll(cloneDir string) error`, `FastForward(cloneDir, remote, branch string) error`, `Remotes(cloneDir string) ([]string, error)`, `ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)`, `AddTracking(cloneDir, worktreePath, branch, remoteRef string) error`, `HasLocalBranch(cloneDir, branch string) (bool, error)`
  - pure func `parseRemoteBranches(output string) []RemoteBranch`

- [ ] **Step 1: Write the failing test for `parseRemoteBranches`**

Add to `internal/worktree/worktree_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/worktree/ -run TestParseRemoteBranches`
Expected: FAIL — `undefined: parseRemoteBranches`.

- [ ] **Step 3: Add types, sentinel, interface methods, real implementations, and `parseRemoteBranches`**

In `internal/worktree/worktree.go`, add to the sentinel `var (...)` block (around line 20):

```go
	ErrLocalBranchExists = errors.New("local branch already exists")
```

Add the `Branch` field to `NewResult` (around line 42):

```go
// NewResult is the result of a successful worktree creation.
type NewResult struct {
	Path   string
	Branch string // resolved local branch name (derived for --track)
}
```

Add the `RemoteBranch` type near `WorktreeInfo`:

```go
// RemoteBranch is one remote-tracking branch (e.g. origin/feature-x).
type RemoteBranch struct {
	Remote string
	Branch string
	Ref    string // "<remote>/<branch>"
}
```

Extend the `WorktreeRunner` interface (replace the block at lines 47-53):

```go
// WorktreeRunner abstracts git worktree operations for testability.
type WorktreeRunner interface {
	Add(cloneDir, worktreePath, branch string) error
	AddNewBranch(cloneDir, worktreePath, branch string) error
	AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint string) error
	Remove(cloneDir, worktreePath string) error
	List(cloneDir string) ([]WorktreeInfo, error)

	Fetch(cloneDir, remote, branch string) error
	FetchAll(cloneDir string) error
	FastForward(cloneDir, remote, branch string) error
	Remotes(cloneDir string) ([]string, error)
	ListRemoteBranches(cloneDir string) ([]RemoteBranch, error)
	AddTracking(cloneDir, worktreePath, branch, remoteRef string) error
	HasLocalBranch(cloneDir, branch string) (bool, error)
}
```

Add the real implementations after `List` (after line 109) and the parser after `parseWorktreePorcelain`:

```go
func (r *RealWorktreeRunner) Fetch(cloneDir, remote, branch string) error {
	cmd := exec.Command("git", "fetch", remote, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch %s %s: %w\n%s", remote, branch, err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) FetchAll(cloneDir string) error {
	cmd := exec.Command("git", "fetch", "--all", "--prune")
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch --all: %w\n%s", err, out)
	}
	return nil
}

// FastForward advances the local branch to <remote>/<branch> without losing
// local commits. It first tries a non-checkout ref update (works when the
// branch is not checked out); if that fails it falls back to a fast-forward-only
// merge (for the clone's currently checked-out branch). Either failure is
// reported but treated as best-effort by callers.
func (r *RealWorktreeRunner) FastForward(cloneDir, remote, branch string) error {
	cmd := exec.Command("git", "fetch", remote, branch+":"+branch)
	cmd.Dir = cloneDir
	if _, err := cmd.CombinedOutput(); err == nil {
		return nil
	}
	cmd = exec.Command("git", "merge", "--ff-only", remote+"/"+branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fast-forward %s: %w\n%s", branch, err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) Remotes(cloneDir string) ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote: %w", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

func (r *RealWorktreeRunner) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	cmd.Dir = cloneDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	return parseRemoteBranches(string(out)), nil
}

func (r *RealWorktreeRunner) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	cmd := exec.Command("git", "worktree", "add", "--track", "-b", branch, worktreePath, remoteRef)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add --track: %w\n%s", err, out)
	}
	return nil
}

func (r *RealWorktreeRunner) HasLocalBranch(cloneDir, branch string) (bool, error) {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = cloneDir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, err
}

// parseRemoteBranches parses `git for-each-ref --format=%(refname:short) refs/remotes`.
// The remote name is the segment before the first slash; the rest is the branch
// (which may contain slashes). Symbolic */HEAD entries are skipped.
func parseRemoteBranches(output string) []RemoteBranch {
	var result []RemoteBranch
	for _, line := range strings.Split(output, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		idx := strings.Index(s, "/")
		if idx <= 0 {
			continue
		}
		remote, branch := s[:idx], s[idx+1:]
		if branch == "" || branch == "HEAD" {
			continue
		}
		result = append(result, RemoteBranch{Remote: remote, Branch: branch, Ref: s})
	}
	return result
}
```

- [ ] **Step 4: Extend `mockGit` so existing tests compile**

In `internal/worktree/worktree_test.go`, replace the `mockGit` struct (lines 111-122) and add the new methods after `List` (after line 140):

```go
// mockGit records calls and controls return values.
type mockGit struct {
	addErr               error
	addNewErr            error
	addNewFromErr        error
	addNewFromCalled     bool
	addNewFromStartPoint string
	removeErr            error
	listResult           []WorktreeInfo
	listErr              error
	addCalled            bool
	addNewCalled         bool

	fetchErr           error
	fetchCalls         [][2]string // {remote, branch}
	fetchAllErr        error
	fetchAllCalled     bool
	fastForwardErr     error
	fastForwardCalls   [][2]string
	remotesResult      []string
	remotesErr         error
	listRemoteResult   []RemoteBranch
	listRemoteErr      error
	addTrackingErr     error
	addTrackingCalled  bool
	addTrackingBranch  string
	addTrackingRef     string
	hasLocalBranch     bool
	hasLocalBranchErr  error
}
```

```go
func (m *mockGit) Fetch(cloneDir, remote, branch string) error {
	m.fetchCalls = append(m.fetchCalls, [2]string{remote, branch})
	return m.fetchErr
}
func (m *mockGit) FetchAll(cloneDir string) error {
	m.fetchAllCalled = true
	return m.fetchAllErr
}
func (m *mockGit) FastForward(cloneDir, remote, branch string) error {
	m.fastForwardCalls = append(m.fastForwardCalls, [2]string{remote, branch})
	return m.fastForwardErr
}
func (m *mockGit) Remotes(cloneDir string) ([]string, error) {
	return m.remotesResult, m.remotesErr
}
func (m *mockGit) ListRemoteBranches(cloneDir string) ([]RemoteBranch, error) {
	return m.listRemoteResult, m.listRemoteErr
}
func (m *mockGit) AddTracking(cloneDir, worktreePath, branch, remoteRef string) error {
	m.addTrackingCalled = true
	m.addTrackingBranch = branch
	m.addTrackingRef = remoteRef
	return m.addTrackingErr
}
func (m *mockGit) HasLocalBranch(cloneDir, branch string) (bool, error) {
	return m.hasLocalBranch, m.hasLocalBranchErr
}
```

- [ ] **Step 5: Run tests to verify the new parser passes and nothing else broke**

Run: `go test ./internal/worktree/`
Expected: PASS (existing tests unchanged this task; `TestParseRemoteBranches` passes).

- [ ] **Step 6: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): add fetch/remote/tracking primitives to WorktreeRunner"
```

---

## Task 2: Pure ref parsing — `parseRef`

**Files:**
- Modify: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Produces: `func parseRef(remotes []string, ref string) (remote, branch string, explicit bool)`

- [ ] **Step 1: Write the failing test**

```go
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
		gotR, gotB, gotE := parseRef(remotes, tc.ref)
		if gotR != tc.remote || gotB != tc.branch || gotE != tc.explicit {
			t.Errorf("parseRef(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.ref, gotR, gotB, gotE, tc.remote, tc.branch, tc.explicit)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/worktree/ -run TestParseRef`
Expected: FAIL — `undefined: parseRef`.

- [ ] **Step 3: Implement `parseRef`**

Add to `internal/worktree/worktree.go`:

```go
// parseRef splits a user-supplied ref into a remote and branch. When ref is
// "<remote>/<rest>" and <remote> matches a configured remote, it returns
// (remote, rest, true). Otherwise it defaults to ("origin", ref, false), which
// keeps branch names containing slashes (e.g. feature/login) intact.
func parseRef(remotes []string, ref string) (remote, branch string, explicit bool) {
	if idx := strings.Index(ref, "/"); idx > 0 {
		candidate := ref[:idx]
		for _, r := range remotes {
			if r == candidate {
				return candidate, ref[idx+1:], true
			}
		}
	}
	return "origin", ref, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/ -run TestParseRef`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): add parseRef ref-splitting helper"
```

---

## Task 3: Fresh source on creation — `freshenStartPoint` + rewire `New`/`NewFrom`

Adds the freshness helper and routes both existing creation paths through it. This **changes behavior** of `New` (create-new fallback) and `NewFrom`, so two existing tests are updated.

**Files:**
- Modify: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Consumes: `parseRef` (Task 2); `WorktreeRunner.Fetch/FastForward/Remotes/HasLocalBranch` (Task 1).
- Produces: `func (s *Service) freshenStartPoint(cloneDir, src string) string`; `New`/`NewFrom` now populate `NewResult.Branch` and base new branches off the freshened start point.

- [ ] **Step 1: Write failing tests for the helper and the rewired paths**

Add new tests and **replace** the two existing tests noted below.

New helper tests:

```go
func TestFreshenStartPoint_localBranchPreferred(t *testing.T) {
	git := &mockGit{hasLocalBranch: true}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "main")
	if got != "main" {
		t.Errorf("start point = %q, want %q", got, "main")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "main"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	if len(git.fastForwardCalls) != 1 || git.fastForwardCalls[0] != [2]string{"origin", "main"} {
		t.Errorf("fast-forward calls = %v", git.fastForwardCalls)
	}
}

func TestFreshenStartPoint_noLocalBranchUsesRemoteTracking(t *testing.T) {
	git := &mockGit{hasLocalBranch: false}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "feat-x")
	if got != "origin/feat-x" {
		t.Errorf("start point = %q, want %q", got, "origin/feat-x")
	}
	if len(git.fastForwardCalls) != 0 {
		t.Errorf("did not expect fast-forward, got %v", git.fastForwardCalls)
	}
}

func TestFreshenStartPoint_fetchFailsFallsBackToRaw(t *testing.T) {
	git := &mockGit{fetchErr: fmt.Errorf("no such branch")}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "v1.2.3")
	if got != "v1.2.3" {
		t.Errorf("start point = %q, want %q", got, "v1.2.3")
	}
}

func TestFreshenStartPoint_explicitRemoteRef(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin", "upstream"}}
	svc, _ := makeService(t, git, &mockTmuxRunner{})
	got := svc.freshenStartPoint("/clone", "upstream/feat-x")
	if got != "upstream/feat-x" {
		t.Errorf("start point = %q, want %q", got, "upstream/feat-x")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"upstream", "feat-x"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
}
```

**Replace** `TestService_New_branchNotFound_fallsBackToAddNew` (lines 227-241) with:

```go
func TestService_New_branchNotFound_createsFromFreshDefault(t *testing.T) {
	git := &mockGit{addErr: fmt.Errorf("invalid reference"), hasLocalBranch: false}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.New("myapp", "new-feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addNewFromCalled {
		t.Error("expected AddNewBranchFrom to be called on Add failure")
	}
	if git.addNewFromStartPoint != "origin/main" {
		t.Errorf("start point = %q, want %q", git.addNewFromStartPoint, "origin/main")
	}
	if result.Branch != "new-feature" {
		t.Errorf("branch = %q, want %q", result.Branch, "new-feature")
	}
}
```

**Replace** `TestService_New_withFromBranch` (lines 243-264) with:

```go
func TestService_New_withFromBranch(t *testing.T) {
	git := &mockGit{hasLocalBranch: false}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewFrom("myapp", "my-feature", "feature-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.addNewFromCalled {
		t.Error("expected AddNewBranchFrom to be called")
	}
	if git.addNewFromStartPoint != "origin/feature-auth" {
		t.Errorf("start point = %q, want %q", git.addNewFromStartPoint, "origin/feature-auth")
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "feature-auth"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/my-feature"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/worktree/ -run 'TestFreshenStartPoint|TestService_New_branchNotFound_createsFromFreshDefault|TestService_New_withFromBranch'`
Expected: FAIL — `freshenStartPoint` undefined and start-point/`Branch` assertions unmet.

- [ ] **Step 3: Implement `freshenStartPoint` and rewire `New`/`NewFrom`**

Add the helper to `internal/worktree/worktree.go`:

```go
// freshenStartPoint fetches updates for the source ref and returns the start
// point a new worktree branch should be based on. It prefers a fast-forwarded
// local branch (to preserve un-pushed commits), falling back to the
// remote-tracking ref, or the raw ref when the source is not on a remote
// (tags, SHAs, local-only branches). All git failures here are best-effort.
func (s *Service) freshenStartPoint(cloneDir, src string) string {
	remotes, _ := s.git.Remotes(cloneDir)
	remote, branch, explicit := parseRef(remotes, src)
	if explicit {
		_ = s.git.Fetch(cloneDir, remote, branch)
		return src
	}
	if err := s.git.Fetch(cloneDir, "origin", src); err != nil {
		return src
	}
	if has, _ := s.git.HasLocalBranch(cloneDir, src); has {
		_ = s.git.FastForward(cloneDir, "origin", src)
		return src
	}
	return "origin/" + src
}
```

In `New` (lines 203-214), replace the create block and the return:

```go
	addErr := s.git.Add(cloneDir, worktreePath, branch)
	if addErr != nil {
		src := p.DefaultBranch
		if src == "" {
			src = "main"
		}
		startPoint := s.freshenStartPoint(cloneDir, src)
		if err := s.git.AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint); err != nil {
			return NewResult{}, fmt.Errorf("failed to create worktree (add: %v; add -b from %s: %w)", addErr, startPoint, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: branch}, nil
```

In `NewFrom` (lines 253-261), replace the add + return:

```go
	startPoint := s.freshenStartPoint(cloneDir, fromBranch)
	if err := s.git.AddNewBranchFrom(cloneDir, worktreePath, branch, startPoint); err != nil {
		return NewResult{}, fmt.Errorf("creating worktree from %s: %w", startPoint, err)
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: branch}, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/worktree/`
Expected: PASS (all worktree tests, including the rewritten ones).

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): base new worktrees on a freshly-fetched source"
```

---

## Task 4: Service methods — `NewTracking`, `ListRemoteBranches`, `RemoteBranches`

**Files:**
- Modify: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

**Interfaces:**
- Consumes: `parseRef`, `WorktreeRunner.{Remotes,Fetch,HasLocalBranch,AddTracking,FetchAll,ListRemoteBranches}`, `resolvePaths`.
- Produces:
  - `func (s *Service) NewTracking(project, branch, ref string) (NewResult, error)`
  - `func (s *Service) ListRemoteBranches(project string) ([]RemoteBranch, error)`
  - `func (s *Service) RemoteBranches(project string) ([]RemoteBranch, error)`

- [ ] **Step 1: Write failing tests**

```go
func TestService_NewTracking_success(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewTracking("myapp", "", "feat-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(git.fetchCalls) != 1 || git.fetchCalls[0] != [2]string{"origin", "feat-x"} {
		t.Errorf("fetch calls = %v", git.fetchCalls)
	}
	if !git.addTrackingCalled || git.addTrackingBranch != "feat-x" || git.addTrackingRef != "origin/feat-x" {
		t.Errorf("AddTracking branch=%q ref=%q", git.addTrackingBranch, git.addTrackingRef)
	}
	if result.Branch != "feat-x" {
		t.Errorf("branch = %q, want feat-x", result.Branch)
	}
	expectedPath := cloneDirPath(tmpDir) + "__worktrees/feat-x"
	if result.Path != expectedPath {
		t.Errorf("path = %q, want %q", result.Path, expectedPath)
	}
}

func TestService_NewTracking_overrideName(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin", "upstream"}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := svc.NewTracking("myapp", "review", "upstream/feat-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if git.addTrackingBranch != "review" || git.addTrackingRef != "upstream/feat-x" {
		t.Errorf("AddTracking branch=%q ref=%q", git.addTrackingBranch, git.addTrackingRef)
	}
	if result.Branch != "review" {
		t.Errorf("branch = %q, want review", result.Branch)
	}
}

func TestService_NewTracking_localBranchExists(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}, hasLocalBranch: true}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewTracking("myapp", "", "feat-x")
	if !errors.Is(err, ErrLocalBranchExists) {
		t.Errorf("expected ErrLocalBranchExists, got %v", err)
	}
	if git.addTrackingCalled {
		t.Error("AddTracking should not be called when the branch already exists")
	}
}

func TestService_NewTracking_fetchFails(t *testing.T) {
	git := &mockGit{remotesResult: []string{"origin"}, fetchErr: fmt.Errorf("no such ref")}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.NewTracking("myapp", "", "feat-x")
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}
	if git.addTrackingCalled {
		t.Error("AddTracking should not run after a failed fetch")
	}
}

func TestService_NewTracking_notCloned(t *testing.T) {
	svc, _ := makeService(t, &mockGit{remotesResult: []string{"origin"}}, &mockTmuxRunner{})
	_, err := svc.NewTracking("myapp", "", "feat-x")
	if !errors.Is(err, ErrNotCloned) {
		t.Errorf("expected ErrNotCloned, got %v", err)
	}
}

func TestService_RemoteBranches_fetchesThenLists(t *testing.T) {
	git := &mockGit{listRemoteResult: []RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := svc.RemoteBranches("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !git.fetchAllCalled {
		t.Error("expected FetchAll to be called")
	}
	if len(got) != 1 || got[0].Ref != "origin/feat-x" {
		t.Errorf("branches = %+v", got)
	}
}

func TestService_ListRemoteBranches_noFetch(t *testing.T) {
	git := &mockGit{listRemoteResult: []RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}}}
	svc, tmpDir := makeService(t, git, &mockTmuxRunner{})
	if err := os.MkdirAll(cloneDirPath(tmpDir), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListRemoteBranches("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if git.fetchAllCalled {
		t.Error("ListRemoteBranches must not fetch")
	}
	if len(got) != 1 {
		t.Errorf("branches = %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/worktree/ -run 'NewTracking|RemoteBranches'`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement the three Service methods**

Add a small clone-dir helper and the methods to `internal/worktree/worktree.go`:

```go
// cloneDirFor returns the clone directory for a project without needing a
// branch (unlike resolvePaths, which also computes a worktree path).
func (s *Service) cloneDirFor(project string) (string, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return "", fmt.Errorf("project %q is not configured", project)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL: %w", err)
	}
	return semconv.CloneDir(s.cfg.Defaults.ProjectsDir, repoPath), nil
}

// NewTracking fetches a remote branch and creates a worktree whose local branch
// tracks it. ref is "[<remote>/]<branch>" (default remote origin). When branch
// is empty the local name is derived from the remote branch; otherwise it
// overrides the derived name.
func (s *Service) NewTracking(project, branch, ref string) (NewResult, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return NewResult{}, fmt.Errorf("project %q is not configured", project)
	}

	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return NewResult{}, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return NewResult{}, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}

	remotes, _ := s.git.Remotes(cloneDir)
	remote, remoteBranch, _ := parseRef(remotes, ref)
	localName := branch
	if localName == "" {
		localName = remoteBranch
	}
	remoteRef := remote + "/" + remoteBranch

	_, worktreesRoot, worktreePath, err := s.resolvePaths(project, localName)
	if err != nil {
		return NewResult{}, err
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return NewResult{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, project, localName)
	}

	if has, _ := s.git.HasLocalBranch(cloneDir, localName); has {
		return NewResult{}, fmt.Errorf("%w: %s", ErrLocalBranchExists, localName)
	}

	if err := s.git.Fetch(cloneDir, remote, remoteBranch); err != nil {
		return NewResult{}, fmt.Errorf("fetching %s: %w", remoteRef, err)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       localName,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: worktreePath,
	}

	if err := s.hook.Trigger(semconv.HookPreWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("pre-worktree hook: %w", err)
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return NewResult{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	if err := s.git.AddTracking(cloneDir, worktreePath, localName, remoteRef); err != nil {
		return NewResult{}, fmt.Errorf("creating tracking worktree for %s: %w", remoteRef, err)
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath, Branch: localName}, nil
}

// ListRemoteBranches returns the remote-tracking branches for a project without
// fetching (suitable for shell completion).
func (s *Service) ListRemoteBranches(project string) ([]RemoteBranch, error) {
	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	return s.git.ListRemoteBranches(cloneDir)
}

// RemoteBranches fetches all remotes (best-effort) then lists remote-tracking
// branches — used by the TUI picker so the list reflects current remote state.
func (s *Service) RemoteBranches(project string) ([]RemoteBranch, error) {
	cloneDir, err := s.cloneDirFor(project)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}
	_ = s.git.FetchAll(cloneDir)
	return s.git.ListRemoteBranches(cloneDir)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/worktree/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): add NewTracking and remote-branch listing"
```

---

## Task 5: CLI `--track` flag on `create worktree`

**Files:**
- Modify: `cmd/worktree.go`
- Test: `cmd/worktree_test.go` (create if absent; otherwise add to the existing cmd test file)

**Interfaces:**
- Consumes: `worktree.Service.NewTracking`, `worktree.NewResult.Branch` (Task 4).
- Produces: `CreateWorktreeCmd.Track` field; `--track` flag; `create worktree` accepting 1-2 positionals.

- [ ] **Step 1: Write the failing test**

Add to `cmd/worktree_test.go` (package `cmd`). These tests exercise the Cobra wiring without hitting git.

```go
package cmd

import (
	"strings"
	"testing"
)

func TestCreateWorktree_trackFlagRegistered(t *testing.T) {
	c := (&CreateWorktreeCmd{}).Cobra()
	if c.Flags().Lookup("track") == nil {
		t.Fatal("expected --track flag to be registered")
	}
}

func TestCreateWorktree_trackAndFromMutuallyExclusive(t *testing.T) {
	c := (&CreateWorktreeCmd{}).Cobra()
	c.SetArgs([]string{"myapp", "--track", "feat-x", "--from", "main"})
	c.SilenceUsage = true
	c.SilenceErrors = true
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}

func TestCreateWorktree_branchRequiredWithoutTrack(t *testing.T) {
	c := (&CreateWorktreeCmd{}).Cobra()
	c.SetArgs([]string{"myapp"})
	c.SilenceUsage = true
	c.SilenceErrors = true
	err := c.Execute()
	if err == nil {
		t.Fatal("expected error when no branch and no --track given")
	}
}
```

Note: `TestCreateWorktree_branchRequiredWithoutTrack` reaches `Run`, which reads the package `cfg` global. If `cfg` is nil in the cmd test package, guard by setting a minimal `cfg` in the test, mirroring how sibling cmd tests set it up (check existing `cmd` tests for the pattern, e.g. a `setTestConfig` helper or direct `cfg = &config.Config{...}` assignment). Use the same approach already present in the package.

- [ ] **Step 2: Run to verify failures**

Run: `go test ./cmd/ -run TestCreateWorktree`
Expected: FAIL — `track` flag missing / no mutual exclusion / branch not required.

- [ ] **Step 3: Implement the flag, args, and routing**

In `cmd/worktree.go`, update the struct (lines 67-71):

```go
type CreateWorktreeCmd struct {
	From   string
	Track  string
	Attach bool
	Agent  string
}
```

Update `Cobra()` (lines 73-88):

```go
func (c *CreateWorktreeCmd) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "worktree <project> [branch]",
		Aliases:           []string{"worktrees", "wt"},
		Short:             "Create a new worktree for a project",
		Args:              cobra.RangeArgs(1, 2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectOnly,
	}
	cmd.Flags().StringVar(&c.From, "from", "", "base branch to create worktree from")
	cmd.Flags().StringVar(&c.Track, "track", "", "remote branch to fetch and check out (e.g. feat-x or upstream/feat-x)")
	cmd.Flags().BoolVar(&c.Attach, "attach", false, "start a coding session after creation")
	cmd.Flags().StringVar(&c.Agent, "agent", "", "agent to use for the session (with --attach)")
	cmd.MarkFlagsMutuallyExclusive("from", "track")
	_ = cmd.RegisterFlagCompletionFunc("from", completeBranches)
	_ = cmd.RegisterFlagCompletionFunc("track", completeRemoteBranches)
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgents)
	return cmd
}
```

Replace the top of `Run` (lines 90-113) so it handles the optional positional and `--track`, and uses `result.Branch` afterward:

```go
func (c *CreateWorktreeCmd) Run(cmd *cobra.Command, args []string) error {
	project := args[0]
	posBranch := ""
	if len(args) > 1 {
		posBranch = args[1]
	}
	if c.Track == "" && posBranch == "" {
		return fmt.Errorf("a <branch> argument is required unless --track is given")
	}

	projCfg := cfg.Projects[project]
	h := hooks.New(projCfg.Hooks)

	var cloneDir string
	if repoPath, rpErr := config.RepoPath(projCfg.Repo); rpErr == nil {
		cloneDir = filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
	}

	svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
	var result worktree.NewResult
	var err error
	switch {
	case c.Track != "":
		fmt.Fprintf(cmd.OutOrStdout(), "Checking out %s into a new worktree...  ", c.Track)
		result, err = svc.NewTracking(project, posBranch, c.Track)
	case c.From != "":
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, posBranch)
		result, err = svc.NewFrom(project, posBranch, c.From)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "Creating worktree %s/%s...  ", project, posBranch)
		result, err = svc.New(project, posBranch)
	}
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return worktreeErr(cmd, project, posBranch, err)
	}

	branch := result.Branch
```

The remaining body (file copy, template processing, "done"/Path print, and the `--attach` block) is unchanged and already references the local `branch` variable — it now carries the resolved (possibly derived) name. Verify no other reference to the old positional `branch` remains in the function.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestCreateWorktree`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/worktree.go cmd/worktree_test.go
git commit -m "feat(cmd): add --track to create worktree for remote checkout"
```

---

## Task 6: Shell completion for `--track`

**Files:**
- Modify: `cmd/completion.go`
- Test: `cmd/completion_test.go`

**Interfaces:**
- Consumes: `worktree.Service.ListRemoteBranches`, `worktree.RemoteBranch` (Task 4).
- Produces: `var completionRemoteBrancher func(*config.Config, string) ([]worktree.RemoteBranch, error)`; `func completeRemoteBranches(...)` (referenced by Task 5).

- [ ] **Step 1: Write the failing test**

Add to `cmd/completion_test.go`:

```go
func TestCompleteRemoteBranches(t *testing.T) {
	orig := completionRemoteBrancher
	t.Cleanup(func() { completionRemoteBrancher = orig })
	completionRemoteBrancher = func(_ *config.Config, project string) ([]worktree.RemoteBranch, error) {
		return []worktree.RemoteBranch{
			{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
			{Remote: "upstream", Branch: "fix-y", Ref: "upstream/fix-y"},
		}, nil
	}

	cmd := (&CreateWorktreeCmd{}).Cobra()
	got, directive := completeRemoteBranches(cmd, []string{"myapp"}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v", directive)
	}
	want := []string{"origin/feat-x", "upstream/fix-y"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("completions = %v, want %v", got, want)
	}
}

func TestCompleteRemoteBranches_noProject(t *testing.T) {
	cmd := (&CreateWorktreeCmd{}).Cobra()
	got, _ := completeRemoteBranches(cmd, nil, "")
	if got != nil {
		t.Errorf("expected nil completions with no project, got %v", got)
	}
}
```

Match the import list to the existing `cmd/completion_test.go` (it already imports `cobra`, `config`, and `worktree` for sibling tests; add any missing ones).

- [ ] **Step 2: Run to verify failures**

Run: `go test ./cmd/ -run TestCompleteRemoteBranches`
Expected: FAIL — `completionRemoteBrancher`/`completeRemoteBranches` undefined.

- [ ] **Step 3: Implement the completion**

Add to `cmd/completion.go` (the file already imports `sort`, `cobra`, `config`, `hooks`, `tmux`, `worktree`):

```go
// completionRemoteBrancher lists a project's remote-tracking branches during
// completion (no fetch). Declared as a var so tests can stub it.
var completionRemoteBrancher = func(cfg *config.Config, project string) ([]worktree.RemoteBranch, error) {
	svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
	return svc.ListRemoteBranches(project)
}

// completeRemoteBranches completes the --track flag against the remote-tracking
// branches of the project named in args[0].
func completeRemoteBranches(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	branches, err := completionRemoteBrancher(loadCompletionConfig(cmd), args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	refs := make([]string, 0, len(branches))
	for _, b := range branches {
		refs = append(refs, b.Ref)
	}
	sort.Strings(refs)
	return refs, cobra.ShellCompDirectiveNoFileComp
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestCompleteRemoteBranches`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/completion.go cmd/completion_test.go
git commit -m "feat(cmd): complete --track against remote-tracking branches"
```

---

## Task 7: TUI remote-branch picker

Rebinds `r` from manual refresh to the remote-branch picker, and adds the picker model/screen and the async fetch+list command. Selecting a branch transitions to the create-worktree form (wired in Task 8; this task wires it to a temporary handoff and tests the picker in isolation).

**Files:**
- Modify: `internal/tui/keys.go`, `internal/tui/model.go`
- Create: `internal/tui/remote_picker.go`, `internal/tui/remote_picker_test.go`

**Interfaces:**
- Consumes: `worktree.Service.RemoteBranches`, `worktree.RemoteBranch` (Task 4).
- Produces: `screenRemotePicker` const; `remoteBranchesMsg` type; `Model.remotePicker` field; `remotePickerModel` with `setBranches`/`selected`; `Model.startRemotePicker`/`fetchRemoteBranchesCmd`/`updateRemotePicker`/`showTrackForm`; `keyMap.Remote`.

- [ ] **Step 1: Write failing tests for the picker model**

Create `internal/tui/remote_picker_test.go`:

```go
package tui

import (
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/worktree"
)

func TestRemotePicker_setAndSelect(t *testing.T) {
	p := newRemotePicker("myapp", &config.Config{}, nil)
	if !p.loading {
		t.Error("picker should start in loading state")
	}
	p.setBranches([]worktree.RemoteBranch{
		{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
		{Remote: "origin", Branch: "fix-y", Ref: "origin/fix-y"},
	})
	if p.loading {
		t.Error("picker should leave loading state after setBranches")
	}
	rb, ok := p.selected()
	if !ok || rb.Ref != "origin/feat-x" {
		t.Errorf("selected = %+v ok=%v, want origin/feat-x", rb, ok)
	}
}

func TestRemotePicker_emptySelectNone(t *testing.T) {
	p := newRemotePicker("myapp", &config.Config{}, nil)
	p.setBranches(nil)
	if _, ok := p.selected(); ok {
		t.Error("expected no selection on empty picker")
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/tui/ -run TestRemotePicker`
Expected: FAIL — `newRemotePicker` undefined.

- [ ] **Step 3: Create the picker model**

Create `internal/tui/remote_picker.go`:

```go
package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

// remoteBranchItem adapts a RemoteBranch to the bubbles list.Item interface.
type remoteBranchItem struct {
	rb worktree.RemoteBranch
}

func (i remoteBranchItem) FilterValue() string { return i.rb.Ref }
func (i remoteBranchItem) Title() string       { return i.rb.Ref }
func (i remoteBranchItem) Description() string  { return "" }

// remotePickerModel shows a filterable list of remote-tracking branches.
type remotePickerModel struct {
	list       list.Model
	project    string
	loading    bool
	errText    string
	cfg        *config.Config
	tmuxClient *tmux.Client
}

func newRemotePicker(project string, cfg *config.Config, tmuxClient *tmux.Client) *remotePickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), maxWidth, 20)
	l.Title = "Remote branches"
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	return &remotePickerModel{
		list:       l,
		project:    project,
		loading:    true,
		cfg:        cfg,
		tmuxClient: tmuxClient,
	}
}

func (p *remotePickerModel) setBranches(branches []worktree.RemoteBranch) {
	items := make([]list.Item, len(branches))
	for i, b := range branches {
		items[i] = remoteBranchItem{rb: b}
	}
	p.list.SetItems(items)
	p.loading = false
}

func (p *remotePickerModel) selected() (worktree.RemoteBranch, bool) {
	it, ok := p.list.SelectedItem().(remoteBranchItem)
	if !ok {
		return worktree.RemoteBranch{}, false
	}
	return it.rb, true
}

func (p *remotePickerModel) Update(msg tea.Msg) (*remotePickerModel, tea.Cmd) {
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p *remotePickerModel) View() string {
	switch {
	case p.loading:
		return "Fetching remote branches…"
	case p.errText != "":
		return "Error: " + p.errText + "\n\nEsc: cancel"
	case len(p.list.Items()) == 0:
		return "No remote branches found (try again after pushing/fetching).\n\nEsc: cancel"
	default:
		return p.list.View()
	}
}
```

- [ ] **Step 4: Run picker model tests to verify they pass**

Run: `go test ./internal/tui/ -run TestRemotePicker`
Expected: PASS.

- [ ] **Step 5: Write failing test for the key binding and message handling**

Add to `internal/tui/remote_picker_test.go`:

```go
func TestRemoteBranchesMsg_populatesPicker(t *testing.T) {
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp", &config.Config{}, nil)}
	updated, _ := m.Update(remoteBranchesMsg{
		project:  "myapp",
		branches: []worktree.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}},
	})
	mm := updated.(Model)
	if mm.remotePicker.loading {
		t.Error("expected picker to leave loading after branches arrive")
	}
	if got := len(mm.remotePicker.list.Items()); got != 1 {
		t.Errorf("items = %d, want 1", got)
	}
}

func TestRemoteKeyBindingPresent(t *testing.T) {
	k := defaultKeyMap()
	if len(k.Remote.Keys()) == 0 {
		t.Fatal("expected Remote binding to have keys")
	}
	if k.Remote.Keys()[0] != "r" {
		t.Errorf("Remote key = %q, want r", k.Remote.Keys()[0])
	}
}

func TestRefreshKeyBindingRemoved(t *testing.T) {
	// 'r' now opens the remote picker; manual refresh is gone (auto-refresh
	// covers it). The keyMap must no longer carry a Refresh binding — this test
	// fails to compile if the field is reintroduced, which is the intent.
	k := defaultKeyMap()
	for _, b := range k.FullHelp() {
		for _, kb := range b {
			for _, key := range kb.Keys() {
				if key == "r" && kb.Help().Desc == "refresh" {
					t.Error("manual refresh binding should be removed")
				}
			}
		}
	}
}
```

- [ ] **Step 6: Run to verify failures**

Run: `go test ./internal/tui/ -run 'TestRemoteBranchesMsg|TestRemoteKeyBindingPresent'`
Expected: FAIL — `screenRemotePicker`/`remoteBranchesMsg`/`keyMap.Remote` undefined.

- [ ] **Step 7: Wire the binding, screen, message, and handlers**

In `internal/tui/keys.go`, **remove** the `Refresh` field from `keyMap` (line 11) and **add** the `Remote` field in its place:

```go
	Remote      key.Binding
```

**Remove** the `Refresh` binding from `defaultKeyMap()` (lines 40-43) and **add** the `Remote` binding (after the `New` binding):

```go
		Remote: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "remote"),
		),
```

In `FullHelp()`, replace `k.Refresh` with `k.Remote` in the second row (line 70):

```go
		{k.New, k.Remote, k.Delete},
```

Note: `refreshCmd()` (the data refresh method) stays — it is still driven by the 3-second `tickCmd` and other callers. Only the manual-refresh *key binding* is removed.

In `internal/tui/model.go`:

Add the screen const (in the `const (...)` block at line 24):

```go
	screenRemotePicker
```

Add the message type (near the other message types, ~line 39):

```go
type remoteBranchesMsg struct {
	project  string
	branches []worktree.RemoteBranch
	err      error
}
```

Add the field to `Model` (near `agentPicker`, ~line 80):

```go
	// Remote-branch picker sub-model.
	remotePicker *remotePickerModel
```

Handle the message in the top-level `Update` switch (add a case alongside the others, before the `// Route to sub-screens.` block):

```go
	case remoteBranchesMsg:
		if m.remotePicker != nil && m.remotePicker.project == msg.project {
			if msg.err != nil {
				m.remotePicker.errText = msg.err.Error()
				m.remotePicker.loading = false
			} else {
				m.remotePicker.setBranches(msg.branches)
			}
		}
		return m, nil
```

Add routing in the screen switch (in `Update`, the `switch m.screen` block):

```go
	case screenRemotePicker:
		return m.updateRemotePicker(msg)
```

In `updateList`, **replace** the existing `Refresh` dispatch case (lines 276-277) with the `Remote` dispatch:

```go
		case key.Matches(msg, m.keys.Remote):
			return m.startRemotePicker()
```

(Removing `case key.Matches(msg, m.keys.Refresh): return m, m.refreshCmd()` — the only reference to `m.keys.Refresh`. `keys_test.go` and `model_test.go` reference only `refreshCmd`/refresh behavior, not the binding, so they are unaffected.)

Add the view branch in `View()` (in the `switch m.screen`):

```go
	case screenRemotePicker:
		if m.remotePicker != nil {
			content = m.remotePicker.View()
		}
```

Add the handlers at the end of `internal/tui/model.go`:

```go
// startRemotePicker opens the remote-branch picker for the selected item's
// project and kicks off an async fetch + list.
func (m Model) startRemotePicker() (tea.Model, tea.Cmd) {
	sel := m.selectedItem()
	if sel == nil || sel.Project == "" {
		return m, nil
	}
	m.remotePicker = newRemotePicker(sel.Project, m.cfg, m.tmuxClient)
	m.screen = screenRemotePicker
	return m, m.fetchRemoteBranchesCmd(sel.Project)
}

// fetchRemoteBranchesCmd fetches all remotes (best-effort) and lists the
// remote-tracking branches for the project.
func (m Model) fetchRemoteBranchesCmd(project string) tea.Cmd {
	cfg := m.cfg
	tmuxClient := m.tmuxClient
	return func() tea.Msg {
		wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, &hooks.NoOp{})
		branches, err := wtSvc.RemoteBranches(project)
		return remoteBranchesMsg{project: project, branches: branches, err: err}
	}
}

// updateRemotePicker handles input while the remote-branch picker is shown.
func (m Model) updateRemotePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if m.remotePicker.list.FilterState() != list.Filtering {
			switch keyMsg.String() {
			case "esc":
				m.screen = screenList
				m.remotePicker = nil
				return m, nil
			case "enter":
				if rb, ok := m.remotePicker.selected(); ok {
					project := m.remotePicker.project
					m.remotePicker = nil
					return m.showTrackForm(project, rb)
				}
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.remotePicker, cmd = m.remotePicker.Update(msg)
	return m, cmd
}
```

`showTrackForm` is added in Task 8. For this task, add a temporary stub at the end of `model.go` so the package compiles and the picker tests pass:

```go
// showTrackForm opens the create-worktree form pre-filled to track a remote
// branch. (Filled in by the form-integration task.)
func (m Model) showTrackForm(project string, rb worktree.RemoteBranch) (tea.Model, tea.Cmd) {
	m.screen = screenList
	return m, nil
}
```

Ensure `internal/tui/model.go` imports `"charm.land/bubbles/v2/list"` (already imported) and `"github.com/xico42/codeherd/internal/hooks"` (already imported).

- [ ] **Step 8: Run TUI tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/keys.go internal/tui/keys_test.go internal/tui/model.go internal/tui/remote_picker.go internal/tui/remote_picker_test.go
git commit -m "feat(tui): rebind r to remote-branch picker with async fetch"
```

---

## Task 8: TUI form — track a remote branch from the picker

Replaces the Task 7 `showTrackForm` stub with a real handoff into the existing create-worktree form, pre-filled and routed to `NewTracking`.

**Files:**
- Modify: `internal/tui/form.go`, `internal/tui/model.go`
- Test: `internal/tui/form_test.go`

**Interfaces:**
- Consumes: `worktree.Service.NewTracking` (Task 4); `remotePickerModel.selected` (Task 7).
- Produces: `formContext.tracksRef`/`formContext.branch`; `formModel.tracksRef`; real `Model.showTrackForm`.

- [ ] **Step 1: Write failing tests**

Add to `internal/tui/form_test.go`:

```go
func TestNewFormModel_trackPrefill(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	ctx := formContext{project: "myapp", tracksRef: "origin/feat-x", branch: "feat-x"}
	m := newFormModel(ctx, cfg, nil)
	if m.tracksRef != "origin/feat-x" {
		t.Errorf("tracksRef = %q, want origin/feat-x", m.tracksRef)
	}
	if m.branch != "feat-x" {
		t.Errorf("prefilled branch = %q, want feat-x", m.branch)
	}
}

func TestShowTrackForm_opensFormScreen(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}
	m := Model{cfg: cfg}
	updated, _ := m.showTrackForm("myapp", worktree.RemoteBranch{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"})
	mm := updated.(Model)
	if mm.screen != screenForm {
		t.Errorf("screen = %d, want screenForm", mm.screen)
	}
	if mm.form == nil || mm.form.tracksRef != "origin/feat-x" {
		t.Errorf("form not set up to track; form=%+v", mm.form)
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/tui/ -run 'TestNewFormModel_trackPrefill|TestShowTrackForm'`
Expected: FAIL — `formContext.tracksRef`/`formModel.tracksRef` undefined; stub `showTrackForm` doesn't set the form.

- [ ] **Step 3: Add `tracksRef` to the form and route submission**

In `internal/tui/form.go`, add to `formModel` (after `branch`, ~line 21):

```go
	tracksRef string
```

add to `formContext` (lines 34-37):

```go
type formContext struct {
	project    string
	baseBranch string
	tracksRef  string
	branch     string // optional pre-fill for the branch input
}
```

In `newFormModel`, set the prefill and tracksRef before building the group (after line 46):

```go
	m.tracksRef = ctx.tracksRef
	if ctx.branch != "" {
		m.branch = ctx.branch
	}
```

Change the Note so it reflects tracking when active (lines 51-53):

```go
		huh.NewNote().
			Title("New Worktree").
			Description(worktreeFormDescription(ctx)),
```

and add the helper at the end of `form.go`:

```go
func worktreeFormDescription(ctx formContext) string {
	if ctx.tracksRef != "" {
		return fmt.Sprintf("Project: %s\nTracks: %s", ctx.project, ctx.tracksRef)
	}
	return fmt.Sprintf("Project: %s\nBase: %s", ctx.project, ctx.baseBranch)
}
```

In `submit()` (lines 117-152), capture `tracksRef` and route to `NewTracking`. Replace the captured-vars block and the service-call block:

```go
func (f *formModel) submit() tea.Cmd {
	branch := strings.TrimSpace(f.branch)
	project := f.project
	baseBranch := f.baseBranch
	tracksRef := f.tracksRef
	attach := f.attach
	agent := f.agent
	cfg := f.cfg
	tmuxClient := f.tmuxClient
	projCfg := cfg.Projects[project]

	return func() tea.Msg {
		h := hooks.New(projCfg.Hooks)
		projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
		_ = projSvc.Clone(project)

		wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
		var result worktree.NewResult
		var err error
		switch {
		case tracksRef != "":
			result, err = wtSvc.NewTracking(project, branch, tracksRef)
		case baseBranch != "":
			result, err = wtSvc.NewFrom(project, branch, baseBranch)
		default:
			result, err = wtSvc.New(project, branch)
		}
		if err != nil {
			return errMsg{err: err}
		}

		return worktreeCreatedMsg{
			project: project,
			branch:  result.Branch,
			path:    result.Path,
			attach:  attach,
			agent:   agent,
		}
	}
}
```

- [ ] **Step 4: Replace the `showTrackForm` stub in `model.go`**

In `internal/tui/model.go`, replace the temporary `showTrackForm` from Task 7 with:

```go
// showTrackForm opens the create-worktree form pre-filled to track the selected
// remote branch, deriving the local branch name from it.
func (m Model) showTrackForm(project string, rb worktree.RemoteBranch) (tea.Model, tea.Cmd) {
	ctx := formContext{project: project, tracksRef: rb.Ref, branch: rb.Branch}
	m.form = newFormModel(ctx, m.cfg, m.tmuxClient)
	m.screen = screenForm
	return m, m.form.Init()
}
```

- [ ] **Step 5: Run TUI tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/form.go internal/tui/form_test.go internal/tui/model.go
git commit -m "feat(tui): track remote branch via pre-filled create form"
```

---

## Task 9: Integration test + full verification

End-to-end test against real git (and the isolated tmux pattern), then the full project gate.

**Files:**
- Create: `internal/worktree/integration_test.go`

**Interfaces:**
- Consumes: `worktree.Service.NewTracking`, `RealWorktreeRunner` (Tasks 1-4).

- [ ] **Step 1: Write the integration test**

Create `internal/worktree/integration_test.go`. It builds a real "remote" repo with a branch, clones it into the codeherd layout, then checks the branch out via `NewTracking` and asserts the worktree branch tracks the remote.

```go
//go:build integration

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/tmux"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestNewTracking_endToEnd(t *testing.T) {
	root := t.TempDir()

	// Build an upstream repo with a PR branch.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "init")
	git(t, remote, "checkout", "-b", "feat-x")
	if err := os.WriteFile(filepath.Join(remote, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "feature")
	git(t, remote, "checkout", "main")

	// Clone into the codeherd layout: <projectsDir>/github.com/user/myapp.
	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "clone", remote, cloneDir)

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	svc := NewService(cfg, NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})

	result, err := svc.NewTracking("myapp", "", "feat-x")
	if err != nil {
		t.Fatalf("NewTracking: %v", err)
	}
	if result.Branch != "feat-x" {
		t.Errorf("branch = %q, want feat-x", result.Branch)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "feature.txt")); err != nil {
		t.Errorf("expected feature.txt in worktree: %v", err)
	}

	branch := strings.TrimSpace(git(t, result.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "feat-x" {
		t.Errorf("worktree branch = %q, want feat-x", branch)
	}
	upstream := strings.TrimSpace(git(t, result.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"))
	if upstream != "origin/feat-x" {
		t.Errorf("upstream = %q, want origin/feat-x", upstream)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test -tags integration ./internal/worktree/ -run TestNewTracking_endToEnd -v`
Expected: PASS.

- [ ] **Step 3: Run the full gate**

Run: `make check`
Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds. Fix any coverage shortfall by adding focused unit tests in the affected package, and any lint issues inline.

- [ ] **Step 4: Commit**

```bash
git add internal/worktree/integration_test.go
git commit -m "test(worktree): end-to-end remote-branch checkout integration"
```

---

## Self-Review

**Spec coverage:**

- "Track remote branch, same name, origin default/omittable" → Task 4 `NewTracking` + `parseRef` (Task 2). ✅
- "Ref parsed as `[<remote>/]<branch>`, remote only if configured; slashes preserved" → Task 2 `parseRef` + tests. ✅
- "`--track`/`--from` mutually exclusive" → Task 5 `MarkFlagsMutuallyExclusive` + test. ✅
- "CLI `--track`, positional optional + override, composes with attach/agent" → Task 5. ✅
- "Completion suggests remote branches" → Task 6. ✅
- "Fresh source on creation: fetch + FF local + base off remote-tracking; FF best-effort/non-fatal; preserve un-pushed commits; local-only refs skip fetch" → Task 1 `FastForward`, Task 3 `freshenStartPoint` + tests. ✅ (Refinement vs spec: FF prefers the *local* branch as the base point so un-pushed commits are never dropped — see Global Constraints; the spec's "base off remote-tracking ref" is used only when no local branch exists.)
- "WorktreeRunner additions (Fetch/FetchAll/FastForward/Remotes/ListRemoteBranches/AddTracking) + RemoteBranch type" → Task 1. `HasLocalBranch` added beyond the spec to implement the "local branch already exists" error cleanly. ✅
- "Error handling: fetch failure fatal for track, local-branch-exists error, mutual-exclusion error" → Tasks 4, 5. ✅
- "TUI `r` picker: blocking-ish async fetch, filterable list, empty message, select → pre-filled form → NewTracking" → Tasks 7, 8. ✅ (`r` is rebound from manual refresh to the remote picker; the manual-refresh binding is removed since the dashboard auto-refreshes every 3s.)
- "Testing: unit (parse, ordering, derivation, errors, conflict), CLI, TUI, integration; coverage ≥ 80%" → Tasks 1-9. ✅
- Out-of-scope items (PR numbers, configurable remote, async background refresh) correctly omitted. ✅

**Deviations from spec, intentional and noted in-plan:**
1. The fast-forward courtesy uses the *local* branch as the base point when it exists (never the remote-tracking ref), to avoid dropping un-pushed local commits — a correctness fix surfaced during planning.
2. No `NewResult.Notice` for skipped fast-forwards: FF is silent best-effort. The freshness guarantee still holds because either the local branch is fast-forwarded or (when absent) the remote-tracking ref is used.
3. TUI keybinding is `r`, rebound from the manual dashboard refresh (which is removed — auto-refresh runs every 3s). `refreshCmd` itself is unchanged.
4. Added `WorktreeRunner.HasLocalBranch` to back the "local branch already exists" error.

**Placeholder scan:** No TBD/TODO/"handle errors appropriately"; every code step shows complete code. ✅

**Type consistency:** `RemoteBranch{Remote,Branch,Ref}`, `NewResult{Path,Branch}`, `NewTracking(project, branch, ref)`, `RemoteBranches(project)`, `ListRemoteBranches(project)`, `freshenStartPoint(cloneDir, src)`, `parseRef(remotes, ref)`, `remotePickerModel.{setBranches,selected}`, `formContext.{tracksRef,branch}` are used identically across tasks. ✅
