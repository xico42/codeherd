# Shell Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add dynamic shell completion to `ch` so positional args and flag values are suggested from known config-time values (agents, projects, profiles) and runtime state (worktree branches).

**Architecture:** A central `cmd/completion.go` holds reusable Cobra completion functions plus two override points (package-level `var` function values) that make the providers unit-testable without touching git, tmux, or the filesystem. Each command's `Cobra()` method attaches the providers via `ValidArgsFunction` and `RegisterFlagCompletionFunc`. Because Cobra skips `PersistentPreRunE` during completion, every provider loads its own config (reusing `resolveProfileArg` for `--profile`/`CODEHERD_PROFILE` precedence). A new `config.ProfileNamesFor` discovers profiles even when no profile is active.

**Tech Stack:** Go, `github.com/spf13/cobra` shell completion, existing `internal/config`, `internal/worktree`, `internal/tmux`, `internal/hooks` packages.

**Spec:** `docs/superpowers/specs/2026-05-24-shell-completion-design.md`

---

## Design decisions locked from spec review

- **`completeSessions` is dropped.** Session commands take two positionals `<project> <branch>`, not a single session token. `session.Service.List()` does not populate `Project`/`Branch` (only the joined canonical `Name`, which cannot be split reliably because branch names contain dashes). Session commands therefore reuse the same project→branch dispatch as worktree commands.
- **`create worktree <project> <new-branch>`**: arg[1] is a *new* branch being created, so it gets **no** branch completion. Only `--from` completes existing branches. All other `<project> <branch>` commands complete both positions.
- **Profile completion uses `config.ProfileNamesFor`**, not the active registry, so `ch -p <TAB>` works even when `profiles_enabled=true` but no `main_profile`/`--profile` is set (the case where `config.Load` would error).

## File structure

- **Create** `cmd/completion.go` — providers (`completeAgents`, `completeProjects`, `completeProfiles`, `completeBranches`), dispatch helpers (`completeProjectThenBranch`, `completeProjectOnly`), pure helpers (`branchNames`, `completionFlag`), and override vars (`loadCompletionConfig`, `completionProfileNames`, `completionBranchLister`).
- **Create** `cmd/completion_internal_test.go` (`package cmd`) — unit tests overriding the vars.
- **Modify** `internal/config/profile.go` — extract `resolveProfilesDir`, add `ProfileNamesFor`.
- **Modify** `internal/config/profile_test.go` — tests for `ProfileNamesFor`.
- **Modify** `cmd/run.go`, `cmd/session.go`, `cmd/worktree.go`, `cmd/project.go` — attach completions.
- **Modify** `cmd/root.go` — register `--profile` flag completion.
- **Modify** `README.md` — add a "Shell Completion" section.

## Wiring map

| Command | struct | arg[0] | arg[1] | flag completions |
|---|---|---|---|---|
| `run <agent>` | `RunAgentCmd` | agents | — | — |
| `create session` | `CreateSessionCmd` | projects | branches | `--agent` → agents |
| `delete session` | `DeleteSessionCmd` | projects | branches | — |
| `show session` | `ShowSessionCmd` | projects | branches | — |
| `attach session` | `AttachSessionCmd` | projects | branches | — |
| `create worktree` | `CreateWorktreeCmd` | projects | — (new branch) | `--agent` → agents, `--from` → branches |
| `delete worktree` | `DeleteWorktreeCmd` | projects | branches | — |
| `show project` | `ShowProjectCmd` | projects | — | — |
| `clone project` | `CloneProjectCmd` | projects | — | — |
| `list worktree` | `ListWorktreeCmd` | projects | — | — |
| root | `rootCmd` | — | — | `--profile` → profiles |

---

## Task 1: `config.ProfileNamesFor` + extract `resolveProfilesDir`

**Files:**
- Modify: `internal/config/profile.go`
- Test: `internal/config/profile_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/profile_test.go`:

```go
func TestProfileNamesFor_listsProfiles(t *testing.T) {
	dir := t.TempDir()
	profDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"work", "home"} {
		if err := os.WriteFile(filepath.Join(profDir, n+".toml"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[defaults]\nprofiles_enabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ProfileNamesFor(cfgPath)
	want := []string{"home", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileNamesFor = %v, want %v", got, want)
	}
}

func TestProfileNamesFor_profilesDisabled_returnsNil(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[defaults]\nprofiles_enabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProfileNamesFor(cfgPath); got != nil {
		t.Fatalf("ProfileNamesFor = %v, want nil", got)
	}
}

func TestProfileNamesFor_missingConfig_returnsNil(t *testing.T) {
	if got := ProfileNamesFor(filepath.Join(t.TempDir(), "absent.toml")); got != nil {
		t.Fatalf("ProfileNamesFor = %v, want nil", got)
	}
}
```

Ensure `profile_test.go` imports `os`, `path/filepath`, `reflect`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestProfileNamesFor -v`
Expected: FAIL — `undefined: ProfileNamesFor`.

- [ ] **Step 3: Extract `resolveProfilesDir` and add `ProfileNamesFor`**

In `internal/config/profile.go`, replace the profiles-dir block inside `loadProfileMode` (currently lines ~62-74):

```go
	profilesDir := main.Defaults.ProfilesDir
	if profilesDir == "" {
		profilesDir = filepath.Join(filepath.Dir(mainPath), "profiles")
	} else {
		expanded, err := expandTilde(profilesDir)
		if err != nil {
			return nil, nil, err
		}
		profilesDir = expanded
	}
	if st, err := os.Stat(profilesDir); err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("profiles_enabled=true but profiles_dir %q does not exist", profilesDir)
	}
```

with:

```go
	profilesDir, err := resolveProfilesDir(main, mainPath)
	if err != nil {
		return nil, nil, err
	}
	if st, err := os.Stat(profilesDir); err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("profiles_enabled=true but profiles_dir %q does not exist", profilesDir)
	}
```

Leave the later `profCfg, err := LoadProfile(...)` and `names, err := DiscoverProfiles(...)` as `:=` — `profCfg` and `names` are still new variables, so Go's short-var-decl stays valid even though `err` is now declared earlier by `resolveProfilesDir`. (Switching them to `=` would fail: `profCfg`/`names` would be undefined.)

Then add at the end of the file:

```go
// resolveProfilesDir returns the directory holding profile TOML files for
// the given main config: defaults.profiles_dir (tilde-expanded) when set,
// otherwise <dir-of-mainPath>/profiles. It does not check existence.
func resolveProfilesDir(main *Config, mainPath string) (string, error) {
	if main.Defaults.ProfilesDir == "" {
		return filepath.Join(filepath.Dir(mainPath), "profiles"), nil
	}
	return expandTilde(main.Defaults.ProfilesDir)
}

// ProfileNamesFor returns the profile names discoverable from the main
// config at path, independent of whether a profile is active. It returns
// nil when profiles are disabled, the config or profiles dir is missing,
// or any error occurs. Intended for shell completion, where Load may
// error (e.g. profiles_enabled with no active profile) yet the user still
// wants profile suggestions. An empty path resolves to the default config
// location.
func ProfileNamesFor(path string) []string {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".config", "codeherd", "config.toml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	main := &Config{path: path}
	if err := toml.Unmarshal(data, main); err != nil {
		return nil
	}
	if !main.Defaults.ProfilesEnabled {
		return nil
	}
	dir, err := resolveProfilesDir(main, path)
	if err != nil {
		return nil
	}
	names, err := DiscoverProfiles(dir)
	if err != nil {
		return nil
	}
	return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (new tests plus all existing profile/config tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/profile.go internal/config/profile_test.go
git commit -m "feat(config): add ProfileNamesFor for completion-time discovery"
```

---

## Task 2: completion skeleton — `completeAgents` and `completeProjects`

**Files:**
- Create: `cmd/completion.go`
- Test: `cmd/completion_internal_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/completion_internal_test.go`:

```go
package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
)

// withStubConfig swaps loadCompletionConfig for the duration of a test.
func withStubConfig(t *testing.T, c *config.Config) {
	t.Helper()
	orig := loadCompletionConfig
	loadCompletionConfig = func(*cobra.Command) *config.Config { return c }
	t.Cleanup(func() { loadCompletionConfig = orig })
}

func TestCompleteAgents(t *testing.T) {
	withStubConfig(t, &config.Config{Agents: map[string]config.AgentConfig{
		"claude": {Cmd: "claude"},
		"aider":  {Cmd: "aider"},
	}})

	got, dir := completeAgents(&cobra.Command{}, nil, "")
	want := []string{"aider", "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteProjects_sorted(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{
		"zeta":  {},
		"alpha": {},
	}})

	got, dir := completeProjects(&cobra.Command{}, nil, "")
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestCompleteAgents|TestCompleteProjects' -v`
Expected: FAIL — `undefined: completeAgents` / `loadCompletionConfig`.

- [ ] **Step 3: Create `cmd/completion.go`**

```go
package cmd

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
)

// loadCompletionConfig loads config during a shell-completion call.
// PersistentPreRunE does not run before completion functions, so the cfg
// global is nil here. It reads the --config and --profile flag values off
// cmd and applies the same profile precedence as the runtime via
// resolveProfileArg (main_profile < CODEHERD_PROFILE < --profile). On any
// load error it returns an empty (non-nil) config so callers need no nil
// checks. Declared as a var so tests can stub it.
var loadCompletionConfig = func(cmd *cobra.Command) *config.Config {
	c, _, err := config.Load(completionFlag(cmd, "config"), resolveProfileArg(completionFlag(cmd, "profile")))
	if err != nil {
		return &config.Config{}
	}
	return c
}

// completionFlag returns the string value of a (possibly inherited
// persistent) flag, or "" when the flag is not defined on cmd.
func completionFlag(cmd *cobra.Command, name string) string {
	if f := cmd.Flags().Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}

// completeAgents completes against configured agent names.
func completeAgents(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return loadCompletionConfig(cmd).AgentNames(), cobra.ShellCompDirectiveNoFileComp
}

// completeProjects completes against configured project names.
func completeProjects(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg := loadCompletionConfig(cmd)
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestCompleteAgents|TestCompleteProjects' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/completion.go cmd/completion_internal_test.go
git commit -m "feat(cmd): add agent and project shell completion providers"
```

---

## Task 3: `completeProfiles`

**Files:**
- Modify: `cmd/completion.go`
- Test: `cmd/completion_internal_test.go`

- [ ] **Step 1: Write the failing test**

Add to `cmd/completion_internal_test.go`:

```go
func TestCompleteProfiles(t *testing.T) {
	orig := completionProfileNames
	completionProfileNames = func(*cobra.Command) []string { return []string{"home", "work"} }
	t.Cleanup(func() { completionProfileNames = orig })

	got, dir := completeProfiles(&cobra.Command{}, nil, "")
	want := []string{"home", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profiles = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestCompleteProfiles -v`
Expected: FAIL — `undefined: completeProfiles` / `completionProfileNames`.

- [ ] **Step 3: Add `completeProfiles` and its override var**

Append to `cmd/completion.go`:

```go
// completionProfileNames discovers profile names for completion. Declared
// as a var so tests can stub it.
var completionProfileNames = func(cmd *cobra.Command) []string {
	return config.ProfileNamesFor(completionFlag(cmd, "config"))
}

// completeProfiles completes against discoverable profile names. Works even
// when no profile is active (e.g. profiles_enabled with no main_profile).
func completeProfiles(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return completionProfileNames(cmd), cobra.ShellCompDirectiveNoFileComp
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestCompleteProfiles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/completion.go cmd/completion_internal_test.go
git commit -m "feat(cmd): add profile shell completion provider"
```

---

## Task 4: `completeBranches` + `branchNames`

**Files:**
- Modify: `cmd/completion.go`
- Test: `cmd/completion_internal_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cmd/completion_internal_test.go` (add `"github.com/xico42/codeherd/internal/worktree"` to imports):

```go
func TestBranchNames_dedupAndSkipEmpty(t *testing.T) {
	entries := []worktree.ListEntry{
		{Project: "p", Branch: "main"},
		{Project: "p", Branch: ""},
		{Project: "p", Branch: "feature"},
		{Project: "p", Branch: "main"},
	}
	got := branchNames(entries)
	want := []string{"feature", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branchNames = %v, want %v", got, want)
	}
}

func TestCompleteBranches_needsProject(t *testing.T) {
	got, dir := completeBranches(&cobra.Command{}, nil, "")
	if got != nil {
		t.Fatalf("branches = %v, want nil when no project arg", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteBranches_listsForProject(t *testing.T) {
	orig := completionBranchLister
	var gotProject string
	completionBranchLister = func(_ *config.Config, project string) ([]worktree.ListEntry, error) {
		gotProject = project
		return []worktree.ListEntry{{Branch: "main"}, {Branch: "dev"}}, nil
	}
	t.Cleanup(func() { completionBranchLister = orig })
	withStubConfig(t, &config.Config{})

	got, dir := completeBranches(&cobra.Command{}, []string{"myproj"}, "")
	if gotProject != "myproj" {
		t.Fatalf("lister got project %q, want myproj", gotProject)
	}
	want := []string{"dev", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}

func TestCompleteBranches_listerErrorYieldsNothing(t *testing.T) {
	orig := completionBranchLister
	completionBranchLister = func(*config.Config, string) ([]worktree.ListEntry, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { completionBranchLister = orig })
	withStubConfig(t, &config.Config{})

	got, dir := completeBranches(&cobra.Command{}, []string{"myproj"}, "")
	if got != nil {
		t.Fatalf("branches = %v, want nil on error", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("dir = %v, want NoFileComp", dir)
	}
}
```

Add `"errors"` to the test file imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestBranchNames|TestCompleteBranches' -v`
Expected: FAIL — `undefined: completeBranches` / `branchNames` / `completionBranchLister`.

- [ ] **Step 3: Add `completeBranches`, `branchNames`, and the lister var**

Append to `cmd/completion.go` (extend imports with `"github.com/xico42/codeherd/internal/hooks"`, `"github.com/xico42/codeherd/internal/tmux"`, `"github.com/xico42/codeherd/internal/worktree"`):

```go
// completionBranchLister lists worktree entries for a project during
// completion. Declared as a var so tests can stub it without touching git
// or tmux.
var completionBranchLister = func(cfg *config.Config, project string) ([]worktree.ListEntry, error) {
	svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
	return svc.List(project)
}

// completeBranches completes against the worktree branches of the project
// named in args[0]. Returns nothing when no project is present or listing
// fails (e.g. project not cloned).
func completeBranches(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	entries, err := completionBranchLister(loadCompletionConfig(cmd), args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return branchNames(entries), cobra.ShellCompDirectiveNoFileComp
}

// branchNames returns the sorted, deduplicated, non-empty branch names
// from worktree entries.
func branchNames(entries []worktree.ListEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	var names []string
	for _, e := range entries {
		if e.Branch == "" {
			continue
		}
		if _, dup := seen[e.Branch]; dup {
			continue
		}
		seen[e.Branch] = struct{}{}
		names = append(names, e.Branch)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestBranchNames|TestCompleteBranches' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/completion.go cmd/completion_internal_test.go
git commit -m "feat(cmd): add worktree branch shell completion provider"
```

---

## Task 5: positional dispatch helpers

**Files:**
- Modify: `cmd/completion.go`
- Test: `cmd/completion_internal_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cmd/completion_internal_test.go`:

```go
func TestCompleteProjectThenBranch_dispatch(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{"alpha": {}}})
	orig := completionBranchLister
	completionBranchLister = func(*config.Config, string) ([]worktree.ListEntry, error) {
		return []worktree.ListEntry{{Branch: "main"}}, nil
	}
	t.Cleanup(func() { completionBranchLister = orig })

	// arg position 0 -> projects
	got, _ := completeProjectThenBranch(&cobra.Command{}, nil, "")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("pos0 = %v, want [alpha]", got)
	}
	// arg position 1 -> branches
	got, _ = completeProjectThenBranch(&cobra.Command{}, []string{"alpha"}, "")
	if !reflect.DeepEqual(got, []string{"main"}) {
		t.Fatalf("pos1 = %v, want [main]", got)
	}
	// arg position 2 -> nothing
	got, _ = completeProjectThenBranch(&cobra.Command{}, []string{"alpha", "main"}, "")
	if got != nil {
		t.Fatalf("pos2 = %v, want nil", got)
	}
}

func TestCompleteProjectOnly_dispatch(t *testing.T) {
	withStubConfig(t, &config.Config{Projects: map[string]config.ProjectConfig{"alpha": {}}})

	got, _ := completeProjectOnly(&cobra.Command{}, nil, "")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("pos0 = %v, want [alpha]", got)
	}
	got, _ = completeProjectOnly(&cobra.Command{}, []string{"alpha"}, "")
	if got != nil {
		t.Fatalf("pos1 = %v, want nil", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestCompleteProjectThenBranch|TestCompleteProjectOnly' -v`
Expected: FAIL — `undefined: completeProjectThenBranch` / `completeProjectOnly`.

- [ ] **Step 3: Add the dispatch helpers**

Append to `cmd/completion.go`:

```go
// completeProjectThenBranch completes the <project> positional (arg 0),
// then the <branch> positional (arg 1) against that project's worktrees.
// Used by commands operating on an existing branch.
func completeProjectThenBranch(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeProjects(cmd, args, toComplete)
	case 1:
		return completeBranches(cmd, args, toComplete)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeProjectOnly completes only the <project> positional (arg 0).
// Later positionals (e.g. a new branch name) get no completion.
func completeProjectOnly(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeProjects(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestCompleteProjectThenBranch|TestCompleteProjectOnly' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/completion.go cmd/completion_internal_test.go
git commit -m "feat(cmd): add positional completion dispatch helpers"
```

---

## Task 6: wire `run <agent>`

**Files:**
- Modify: `cmd/run.go:16-24`

- [ ] **Step 1: Add `ValidArgsFunction` to the run command**

In `cmd/run.go`, the `Cobra()` method currently returns:

```go
	return &cobra.Command{
		Use:   "run <agent>",
		Short: "Run a registered agent in the current shell",
		Long:  "Resolves a registered agent from config and replaces the current process with it. The agent inherits the current environment with its configured env vars overlaid.",
		Args:  cobra.ExactArgs(1),
		RunE:  c.Run,
	}
```

Add the `ValidArgsFunction` field:

```go
	return &cobra.Command{
		Use:   "run <agent>",
		Short: "Run a registered agent in the current shell",
		Long:  "Resolves a registered agent from config and replaces the current process with it. The agent inherits the current environment with its configured env vars overlaid.",
		Args:  cobra.ExactArgs(1),
		RunE:  c.Run,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeAgents(cmd, args, toComplete)
		},
	}
```

- [ ] **Step 2: Build and manually verify completion output**

Run:
```bash
go build -o /tmp/ch . && CODEHERD_TEST_HOME= /tmp/ch __complete run '' 2>/dev/null
```
Expected: lists configured agent names (or nothing if no config), followed by a `:4` directive line. No error, no file paths.

- [ ] **Step 3: Run the full cmd test suite**

Run: `go test ./cmd/ -v 2>&1 | tail -20`
Expected: PASS (no regressions).

- [ ] **Step 4: Commit**

```bash
git add cmd/run.go
git commit -m "feat(cmd): complete agent names for ch run"
```

---

## Task 7: wire session commands

**Files:**
- Modify: `cmd/session.go` — `ShowSessionCmd` (~99), `CreateSessionCmd` (~143), `DeleteSessionCmd` (~286 area), `AttachSessionCmd` (~336 area)

- [ ] **Step 1: Add `ValidArgsFunction` to each session `<project> <branch>` command**

For **each** of `ShowSessionCmd`, `CreateSessionCmd`, `DeleteSessionCmd`, `AttachSessionCmd`, in their `Cobra()` methods add `ValidArgsFunction: completeProjectThenBranch,` to the `&cobra.Command{...}` literal (alongside the existing `Use`/`Args`/`RunE` fields). Example for `ShowSessionCmd`:

```go
	cobraCmd := &cobra.Command{
		Use:               "session <project> <branch>",
		Aliases:           []string{"sessions", "ses"},
		Short:             "Show details for a session",
		Args:              cobra.ExactArgs(2),
		RunE:              c.Run,
		ValidArgsFunction: completeProjectThenBranch,
	}
```

- [ ] **Step 2: Register `--agent` completion on `CreateSessionCmd`**

In `CreateSessionCmd.Cobra()`, after the existing flag registrations and before `return cobraCmd`, add:

```go
	_ = cobraCmd.RegisterFlagCompletionFunc("agent", completeAgents)
```

- [ ] **Step 3: Build and manually verify**

Run:
```bash
go build -o /tmp/ch . \
  && /tmp/ch __complete show session '' 2>/dev/null \
  && /tmp/ch __complete create session --agent '' 2>/dev/null
```
Expected: first lists project names; second lists agent names. Each ends with a `:4` directive line, no errors.

- [ ] **Step 4: Run the cmd test suite**

Run: `go test ./cmd/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/session.go
git commit -m "feat(cmd): complete project/branch and --agent for session commands"
```

---

## Task 8: wire worktree commands

**Files:**
- Modify: `cmd/worktree.go` — `CreateWorktreeCmd` (~74), `DeleteWorktreeCmd` (~190)

- [ ] **Step 1: Add `ValidArgsFunction` to `DeleteWorktreeCmd`**

In `DeleteWorktreeCmd.Cobra()`, add `ValidArgsFunction: completeProjectThenBranch,` to the command literal (delete targets an existing branch).

- [ ] **Step 2: Add `ValidArgsFunction` to `CreateWorktreeCmd`**

In `CreateWorktreeCmd.Cobra()`, add `ValidArgsFunction: completeProjectOnly,` to the command literal (arg 1 is a *new* branch — no completion).

- [ ] **Step 3: Register `--from` and `--agent` completion on `CreateWorktreeCmd`**

In `CreateWorktreeCmd.Cobra()`, after the existing `cmd.Flags().StringVar(...)` calls for `from` and `agent` and before the return, add:

```go
	_ = cmd.RegisterFlagCompletionFunc("from", completeBranches)
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgents)
```

(Use the same receiver variable name the method already uses for the `*cobra.Command` — confirm whether it is `cmd` or `cobraCmd` in this method and match it.)

- [ ] **Step 4: Build and manually verify**

Run:
```bash
go build -o /tmp/ch . \
  && /tmp/ch __complete delete worktree '' 2>/dev/null \
  && /tmp/ch __complete create worktree myproj '' 2>/dev/null \
  && /tmp/ch __complete create worktree myproj --from '' 2>/dev/null
```
Expected: delete lists projects; `create worktree myproj ''` lists nothing for the new-branch positional; `--from` lists branches of `myproj` (empty if not cloned). Each ends with `:4`.

- [ ] **Step 5: Run the cmd test suite**

Run: `go test ./cmd/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/worktree.go
git commit -m "feat(cmd): complete project/branch, --from and --agent for worktree commands"
```

---

## Task 9: wire project commands and list worktree

**Files:**
- Modify: `cmd/project.go` — `ShowProjectCmd` (~45), `CloneProjectCmd` (~81)
- Modify: `cmd/worktree.go` — `ListWorktreeCmd` (~23)

- [ ] **Step 1: Add `ValidArgsFunction: completeProjectOnly` to each**

- `ShowProjectCmd.Cobra()` (Use `project <name>`, ExactArgs(1)) → add `ValidArgsFunction: completeProjectOnly,`.
- `CloneProjectCmd.Cobra()` (Use `project [<name>]`) → add `ValidArgsFunction: completeProjectOnly,`.
- `ListWorktreeCmd.Cobra()` (Use `worktree [project]`, MaximumNArgs(1)) → add `ValidArgsFunction: completeProjectOnly,`.

- [ ] **Step 2: Build and manually verify**

Run:
```bash
go build -o /tmp/ch . \
  && /tmp/ch __complete show project '' 2>/dev/null \
  && /tmp/ch __complete clone project '' 2>/dev/null \
  && /tmp/ch __complete list worktree '' 2>/dev/null
```
Expected: each lists project names followed by `:4`.

- [ ] **Step 3: Run the cmd test suite**

Run: `go test ./cmd/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/project.go cmd/worktree.go
git commit -m "feat(cmd): complete project names for show/clone project and list worktree"
```

---

## Task 10: wire root `--profile` flag completion

**Files:**
- Modify: `cmd/root.go:45-54` (`init`)

- [ ] **Step 1: Register the completion in `init`**

In `cmd/root.go`, inside `init()`, after the persistent flag is declared:

```go
	rootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "profile to load; overrides $CODEHERD_PROFILE and defaults.main_profile (requires profiles_enabled=true)")
```

add:

```go
	_ = rootCmd.RegisterFlagCompletionFunc("profile", completeProfiles)
```

- [ ] **Step 2: Build and manually verify**

Run:
```bash
go build -o /tmp/ch . && /tmp/ch __complete --profile '' 2>/dev/null
```
Expected: lists profile names (empty if profiles disabled), ending with `:4`. No error.

- [ ] **Step 3: Run the cmd test suite**

Run: `go test ./cmd/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/root.go
git commit -m "feat(cmd): complete profile names for --profile"
```

---

## Task 11: README "Shell Completion" section

**Files:**
- Modify: `README.md` (add a section after the "Install" section, ~line 274-281)

- [ ] **Step 1: Add the documentation section**

Insert after the Install section and before "Quick Start":

````markdown
## Shell Completion

`ch` ships dynamic completion for agents, projects, profiles, and worktree
branches. Cobra generates per-shell scripts via `ch completion <shell>`.

```bash
# zsh — ensure the dir is on your $fpath, then reload
ch completion zsh > "${fpath[1]}/_ch"

# bash
ch completion bash | sudo tee /etc/bash_completion.d/ch > /dev/null

# fish
ch completion fish > ~/.config/fish/completions/ch.fish
```

Completion respects the active profile: inside a profile-mode session
(`$CODEHERD_PROFILE` set) or with `-p <profile>`, branch suggestions come
from that profile's worktrees.
````

- [ ] **Step 2: Verify the section renders**

Run: `grep -n "Shell Completion" README.md`
Expected: prints the new heading line.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document shell completion setup"
```

---

## Task 12: full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the project check gate**

Run: `make check`
Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds.

- [ ] **Step 2: If coverage dipped below 80%**

Add targeted tests for any uncovered completion branch (e.g. a `completionFlag` test with a `*cobra.Command` that has the `--config` flag set, or a `loadCompletionConfig` test pointing `--config` at a temp config file). Re-run `make check`.

- [ ] **Step 3: Final manual smoke**

Run:
```bash
go build -o /tmp/ch . && /tmp/ch completion zsh > /dev/null && echo "completion script generated OK"
```
Expected: `completion script generated OK`.

- [ ] **Step 4: Commit any added tests**

```bash
git add -A
git commit -m "test(cmd): cover remaining completion branches"
```

---

## Self-review notes

- **Spec coverage:** agents (T6), projects (T9), profiles (T1+T3+T10), branches (T4), per-command wiring (T6-T10), config-load-during-completion gotcha + `resolveProfileArg` reuse (T2 `loadCompletionConfig`), profile precedence (T1 `ProfileNamesFor` + T2), error→`NoFileComp` (T2-T4), docs (T11), `make check` gate (T12). `completeSessions` intentionally dropped (see "Design decisions").
- **Profile precedence test:** `loadCompletionConfig` reuses `resolveProfileArg`; the spec's env-precedence behavior is exercised by `internal/config` profile tests plus the runtime `resolveProfileArg`. An optional `loadCompletionConfig` integration test (temp config + `CODEHERD_PROFILE`) can be added in T12 Step 2 if coverage requires.
- **Type consistency:** `loadCompletionConfig`, `completionProfileNames`, `completionBranchLister` are the three override vars; `worktree.ListEntry.Branch`, `config.AgentConfig`, `config.ProjectConfig`, `config.ProfileNamesFor(path string) []string`, `resolveProfileArg(string) string` all match current source signatures.
