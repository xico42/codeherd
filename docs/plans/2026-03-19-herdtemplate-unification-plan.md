# Herdtemplate Unification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `envtemplate` with `herdtemplate` as the single template engine and add standalone `ch template` and `ch worktree template` commands.

**Architecture:** Move `DeterministicPort` into `herdtemplate`, add dry-run support returning `ProcessResult`, delete `envtemplate` and all its wiring. Add two new commands that call `herdtemplate.Service.Process()`.

**Tech Stack:** Go, Cobra CLI, `text/template`, FNV-1a hashing

---

### Task 1: Move `DeterministicPort` into `herdtemplate`

**Files:**
- Modify: `internal/herdtemplate/herdtemplate.go:1-14` (add hash import, add function)
- Modify: `internal/herdtemplate/herdtemplate.go:52-55` (update `port` func to use local function)
- Test: `internal/herdtemplate/herdtemplate_test.go`

**Step 1: Write failing tests for `DeterministicPort` in `herdtemplate`**

Add to `internal/herdtemplate/herdtemplate_test.go`:

```go
func TestDeterministicPort_Idempotent(t *testing.T) {
	p1 := DeterministicPort("myapp", "feature", "api")
	p2 := DeterministicPort("myapp", "feature", "api")
	if p1 != p2 {
		t.Errorf("not idempotent: %d != %d", p1, p2)
	}
}

func TestDeterministicPort_InRange(t *testing.T) {
	p := DeterministicPort("myapp", "feature", "api")
	if p < 10000 || p > 59999 {
		t.Errorf("port %d out of range 10000-59999", p)
	}
}

func TestDeterministicPort_DifferentNames(t *testing.T) {
	p1 := DeterministicPort("myapp", "feature", "api")
	p2 := DeterministicPort("myapp", "feature", "db")
	if p1 == p2 {
		t.Errorf("same port %d for different names", p1)
	}
}

func TestDeterministicPort_NullByteSeparation(t *testing.T) {
	p1 := DeterministicPort("ab", "cd", "x")
	p2 := DeterministicPort("a", "bcd", "x")
	if p1 == p2 {
		t.Errorf("null-byte separation failed: both hashed to %d", p1)
	}
}
```

Note: these tests are `package herdtemplate` (internal), so they call `DeterministicPort` directly.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/herdtemplate/... -run TestDeterministicPort -v`
Expected: FAIL — `DeterministicPort` not defined

**Step 3: Add `DeterministicPort` to `herdtemplate.go`**

Add `"hash/fnv"` to imports. Add after the `herdSuffix` const:

```go
// DeterministicPort returns a stable port for the given project/branch/name.
// Uses FNV-1a 32-bit hash with null-byte separators. Range: 10000–59999.
func DeterministicPort(project, branch, name string) int {
	key := project + "\x00" + branch + "\x00" + name
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()%50000) + 10000
}
```

Update the `port` template func (line 53-54) to call local function:

```go
"port": func(name string) int {
    return DeterministicPort(ctx.Project, ctx.Branch, name)
},
```

Remove the `"github.com/xico42/codeherd/internal/envtemplate"` import.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/herdtemplate/... -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/herdtemplate/
git commit -m "refactor: move DeterministicPort from envtemplate to herdtemplate"
```

---

### Task 2: Add dry-run support to `herdtemplate`

**Files:**
- Modify: `internal/herdtemplate/herdtemplate.go:18-81` (add result types, modify Process signature)
- Test: `internal/herdtemplate/herdtemplate_test.go`

**Step 1: Write failing tests for dry-run behavior**

Add to `internal/herdtemplate/herdtemplate_test.go`:

```go
func TestProcess_DryRun_DoesNotWriteFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("PORT={{ port \"http\" }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Output file should NOT be written
	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Error("expected .env NOT to be written in dry-run mode")
	}

	// Result should contain the rendered file
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 rendered file, got %d", len(result.Files))
	}
	if result.Files[0].Target != filepath.Join(dir, ".env") {
		t.Errorf("target = %q, want %q", result.Files[0].Target, filepath.Join(dir, ".env"))
	}
	if result.Files[0].Output == "" {
		t.Error("expected non-empty rendered output")
	}
}

func TestProcess_DryRun_SkipsHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{}
	svc := New(mock)
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, map[string]string{semconv.HookAttrProject: "myapp"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(mock.calls) != 0 {
		t.Errorf("expected 0 hook calls in dry-run, got %d", len(mock.calls))
	}
}

func TestProcess_ReturnsProcessResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt.herd"), []byte("hello {{ .Project }}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml.herd"), []byte("version: 3"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("expected 2 rendered files, got %d", len(result.Files))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/herdtemplate/... -run "TestProcess_DryRun|TestProcess_ReturnsProcessResult" -v`
Expected: FAIL — `ProcessContext.DryRun` field doesn't exist, `Process` returns `error` not `(ProcessResult, error)`

**Step 3: Implement dry-run and `ProcessResult`**

Replace the types and `Process` method in `internal/herdtemplate/herdtemplate.go`:

```go
// ProcessContext holds values available to .herd templates.
type ProcessContext struct {
	Project      string
	Branch       string
	WorktreePath string
	SessionName  string
	DryRun       bool
}

// RenderedFile holds the result of rendering a single .herd file.
type RenderedFile struct {
	Source string // e.g. "docker-compose.yml.herd"
	Target string // e.g. "docker-compose.yml"
	Output string // rendered content
}

// ProcessResult holds the results of template processing.
type ProcessResult struct {
	Files []RenderedFile
}

// Process walks the directory, finds all .herd files, renders them,
// and writes the output without the .herd suffix. Triggers pre/post-template hooks
// unless DryRun is set.
func (s *Service) Process(ctx ProcessContext, attrs map[string]string) (ProcessResult, error) {
	herdFiles, err := findHerdFiles(ctx.WorktreePath)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("scanning for .herd files: %w", err)
	}

	if len(herdFiles) == 0 {
		return ProcessResult{}, nil
	}

	if !ctx.DryRun {
		if err := s.hook.Trigger(semconv.HookPreTemplate, attrs, ctx.WorktreePath); err != nil {
			return ProcessResult{}, fmt.Errorf("pre-template hook: %w", err)
		}
	}

	funcMap := template.FuncMap{
		"port": func(name string) int {
			return DeterministicPort(ctx.Project, ctx.Branch, name)
		},
		"env": func(args ...string) string {
			if len(args) == 0 {
				return ""
			}
			if v := os.Getenv(args[0]); v != "" {
				return v
			}
			if len(args) > 1 {
				return args[1]
			}
			return ""
		},
	}

	var result ProcessResult
	for _, path := range herdFiles {
		rendered, err := renderFile(path, ctx, funcMap)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("rendering %s: %w", path, err)
		}
		result.Files = append(result.Files, rendered)
	}

	if !ctx.DryRun {
		if err := s.hook.Trigger(semconv.HookPostTemplate, attrs, ctx.WorktreePath); err != nil {
			return ProcessResult{}, fmt.Errorf("post-template hook: %w", err)
		}
	}

	return result, nil
}
```

Update `renderFile` to return `RenderedFile` and conditionally write:

```go
func renderFile(path string, ctx ProcessContext, funcMap template.FuncMap) (RenderedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return RenderedFile{}, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return RenderedFile{}, fmt.Errorf("executing template: %w", err)
	}

	outPath := strings.TrimSuffix(path, herdSuffix)

	if !ctx.DryRun {
		if err := os.WriteFile(outPath, buf.Bytes(), 0o600); err != nil {
			return RenderedFile{}, fmt.Errorf("writing output: %w", err)
		}
	}

	return RenderedFile{
		Source: path,
		Target: outPath,
		Output: buf.String(),
	}, nil
}
```

**Step 4: Fix all existing callers that use the old `Process` signature**

The old signature was `Process(...) error`. The new signature is `Process(...) (ProcessResult, error)`. Update all call sites to ignore the first return value:

- `cmd/worktree.go:120-127`: change `if err := tmplSvc.Process(...)` to `if _, err := tmplSvc.Process(...)`
- `cmd/session.go:111-118`: change `if err := tmplSvc.Process(...)` to `if _, err := tmplSvc.Process(...)`
- `internal/tui/actions.go:424-431`: change `if err := tmplSvc.Process(...)` to `if _, err := tmplSvc.Process(...)`

**Step 5: Run all tests to verify they pass**

Run: `go test ./internal/herdtemplate/... -v && go test ./cmd/... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/herdtemplate/ cmd/ internal/tui/
git commit -m "feat: add dry-run support and ProcessResult to herdtemplate"
```

---

### Task 3: Remove `envtemplate` and all its wiring

**Files:**
- Delete: `internal/envtemplate/envtemplate.go`
- Delete: `internal/envtemplate/envtemplate_test.go`
- Modify: `internal/config/project.go:10-16` (remove `EnvTemplate` field)
- Modify: `internal/config/config.go:66-84` (remove `EnvTemplate` expansion from `expandPaths`)
- Modify: `internal/worktree/worktree.go:1-18` (remove `envtemplate` import)
- Modify: `internal/worktree/worktree.go:42-53` (remove `EnvWritten` from `NewResult`, remove `EnvResult`)
- Modify: `internal/worktree/worktree.go:176-192` (remove `resolveTemplate`)
- Modify: `internal/worktree/worktree.go:241-257` (remove env processing from `New`)
- Modify: `internal/worktree/worktree.go:306-322` (remove env processing from `NewFrom`)
- Modify: `internal/worktree/worktree.go:428-471` (remove `Env` method)
- Modify: `cmd/worktree.go:129-133` (remove `EnvWritten` output)
- Modify: `cmd/worktree.go:239-271` (remove `worktreeEnvCmd`)
- Modify: `cmd/worktree.go:292-306` (remove `worktreeEnvCmd` registration)
- Modify: `internal/worktree/worktree_test.go` (remove/update env-related tests)
- Modify: `internal/config/config_test.go` (remove `EnvTemplate` tests)

**Step 1: Delete `internal/envtemplate/` directory**

```bash
rm -rf internal/envtemplate/
```

**Step 2: Remove `EnvTemplate` from `ProjectConfig`**

In `internal/config/project.go`, remove line 13:

```go
EnvTemplate   string      `toml:"env_template"   validate:"omitempty"`
```

So `ProjectConfig` becomes:

```go
type ProjectConfig struct {
	Repo          string      `toml:"repo"           validate:"omitempty"`
	DefaultBranch string      `toml:"default_branch" validate:"omitempty"`
	Files         []string    `toml:"files"          validate:"omitempty"`
	Hooks         HooksConfig `toml:"hooks"`
}
```

**Step 3: Remove `EnvTemplate` expansion from `expandPaths`**

In `internal/config/config.go`, remove lines 77-82 (the `for` loop expanding `p.EnvTemplate`):

```go
for name, p := range c.Projects {
    if p.EnvTemplate, err = expandTilde(p.EnvTemplate); err != nil {
        return err
    }
    c.Projects[name] = p
}
```

So `expandPaths` becomes:

```go
func (c *Config) expandPaths() error {
	if c.Defaults.ProjectsDir == "" {
		c.Defaults.ProjectsDir = defaultProjectsDir
	}
	var err error
	if c.Defaults.ProjectsDir, err = expandTilde(c.Defaults.ProjectsDir); err != nil {
		return err
	}
	if c.Defaults.GitIdentityFile, err = expandTilde(c.Defaults.GitIdentityFile); err != nil {
		return err
	}
	return nil
}
```

**Step 4: Remove env-related code from `internal/worktree/worktree.go`**

Remove:
- `"github.com/xico42/codeherd/internal/envtemplate"` import (line 14)
- `EnvWritten` field from `NewResult` (line 45)
- `EnvResult` struct (lines 48-53)
- `resolveTemplate` function (lines 176-192)
- Env processing block from `New` (lines 243-257 — the `content, source, _ := resolveTemplate(...)` block). After removing, `New` should end with:

```go
	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath}, nil
}
```

- Env processing block from `NewFrom` (lines 308-322). After removing, `NewFrom` should end with:

```go
	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, fmt.Errorf("post-worktree hook: %w", err)
	}

	return NewResult{Path: worktreePath}, nil
}
```

- `Env` method entirely (lines 428-471)

**Step 5: Remove `worktreeEnvCmd` from `cmd/worktree.go`**

Remove:
- `worktreeEnvDryRun` var (line 241)
- `worktreeEnvCmd` command definition (lines 243-271)
- `worktreeEnvCmd.Flags()` line in `init()` (line 298)
- `worktreeCmd.AddCommand(worktreeEnvCmd)` in `init()` (line 304)
- `EnvWritten` output in `worktreeNewCmd` (lines 131-133):

```go
if result.EnvWritten {
    fmt.Fprintf(cmd.OutOrStdout(), "  Env:  %s/.env\n", result.Path)
}
```

**Step 6: Update tests**

In `internal/worktree/worktree_test.go`, remove or rewrite:
- `TestService_New_withEnvTemplate` (line 574) — remove entirely (tested env template via config, no longer applies)
- `TestService_NewFrom_withEnvTemplate` (line 299) — remove entirely
- `TestService_Env_*` tests (lines 610-843) — remove all of them:
  - `TestService_Env_unknownProject`
  - `TestService_Env_badRepoURL`
  - `TestService_Env_worktreeNotFound`
  - `TestService_Env_templateReadError`
  - `TestService_Env_invalidTemplate`
  - `TestService_Env_noTemplate`
  - `TestService_Env_repoLocalTemplate`
  - `TestService_Env_dryRun`
  - `TestService_Env_configTemplate`

In `internal/config/config_test.go`, update:
- `TestLoad_Projects` (line 175): remove the `env_template` from config content and the assertion checking `api.EnvTemplate` (lines 186, 209-214)
- `TestLoad_ProjectsWithAbsEnvTemplate` (line 301): remove entire test
- `TestIsValidKeyPath` (line 364): remove `{"projects.myapp.env_template", true}` entry (line 378)

**Step 7: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS (no compilation errors from removed types/imports)

**Step 8: Commit**

```bash
git add -A
git commit -m "refactor: remove envtemplate package and all env_template config wiring"
```

---

### Task 4: Add `ch worktree template` command

**Files:**
- Modify: `cmd/worktree.go` (add `worktreeTemplateCmd` and register it)
- Modify: `cmd/worktree_test.go` (add tests)

**Step 1: Write failing tests**

Check the existing test patterns in `cmd/worktree_test.go` to match style. Add:

```go
func TestWorktreeTemplate_tooFewArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"worktree", "template"})
	err := Execute()
	if err == nil {
		t.Fatal("expected error with no args")
	}
}

func TestWorktreeTemplate_tooManyArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"worktree", "template", "a", "b", "c"})
	err := Execute()
	if err == nil {
		t.Fatal("expected error with too many args")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/... -run TestWorktreeTemplate -v`
Expected: FAIL — unknown command "template"

**Step 3: Implement `worktreeTemplateCmd`**

Add to `cmd/worktree.go`:

```go
// ── template ─────────────────────────────────────────────────────────────────

var worktreeTemplateDryRun bool

var worktreeTemplateCmd = &cobra.Command{
	Use:   "template <project> <branch>",
	Short: "Process .herd template files in a worktree",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, branch := args[0], args[1]

		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)
		svc := newWorktreeService()

		path, err := svc.WorktreePath(project, branch)
		if err != nil {
			return worktreeErr(cmd, project, branch, err)
		}

		if !worktreeTemplateDryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Processing templates...  ")
		}

		tmplSvc := herdtemplate.New(h)
		result, err := tmplSvc.Process(herdtemplate.ProcessContext{
			Project:      project,
			Branch:       branch,
			WorktreePath: path,
			SessionName:  semconv.SessionName(project, branch),
			DryRun:       worktreeTemplateDryRun,
		}, map[string]string{
			semconv.HookAttrProject:      project,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: path,
		})
		if err != nil {
			if !worktreeTemplateDryRun {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return err
		}

		if worktreeTemplateDryRun {
			for _, f := range result.Files {
				fmt.Fprintf(cmd.OutOrStdout(), "# %s\n%s\n", f.Target, f.Output)
			}
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		for _, f := range result.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.Target)
		}
		return nil
	},
}
```

Register in `init()`:

```go
worktreeTemplateCmd.Flags().BoolVar(&worktreeTemplateDryRun, "dry-run", false, "print rendered output without writing")
worktreeCmd.AddCommand(worktreeTemplateCmd)
```

**Step 4: Run tests**

Run: `go test ./cmd/... -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add cmd/worktree.go cmd/worktree_test.go
git commit -m "feat: add ch worktree template command"
```

---

### Task 5: Add `ch template` top-level command

**Files:**
- Create: `cmd/template.go`
- Create: `cmd/template_test.go`

**Step 1: Write failing tests**

Create `cmd/template_test.go`:

```go
package cmd

import (
	"testing"
)

func TestTemplate_noArgs_usesCurrentDir(t *testing.T) {
	// With no project resolvable and no flags, should error
	rootCmd.SetArgs([]string{"template"})
	err := Execute()
	if err == nil {
		t.Fatal("expected error when project can't be resolved")
	}
}

func TestTemplate_tooManyArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"template", "dir1", "dir2"})
	err := Execute()
	if err == nil {
		t.Fatal("expected error with too many args")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/... -run TestTemplate -v`
Expected: FAIL — unknown command "template"

**Step 3: Implement `cmd/template.go`**

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

var (
	templateProject string
	templateBranch  string
	templateDryRun  bool
)

var templateCmd = &cobra.Command{
	Use:   "template [dir]",
	Short: "Process .herd template files in a directory",
	Long: `Process all .herd template files in a directory, rendering them with
project and branch context. The project is resolved by matching the directory
against configured projects. The branch is detected from git. Use --project
and --branch flags as fallbacks.`,
	Args:    cobra.MaximumNArgs(1),
	GroupID: "projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		project := templateProject
		if project == "" {
			project = resolveProjectFromDir(absDir)
		}
		if project == "" {
			return fmt.Errorf("could not resolve project for %s; use --project flag", absDir)
		}

		branch := templateBranch
		if branch == "" {
			branch = detectGitBranch(absDir)
		}
		if branch == "" {
			return fmt.Errorf("could not detect branch for %s; use --branch flag", absDir)
		}

		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)

		if !templateDryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Processing templates...  ")
		}

		tmplSvc := herdtemplate.New(h)
		result, err := tmplSvc.Process(herdtemplate.ProcessContext{
			Project:      project,
			Branch:       branch,
			WorktreePath: absDir,
			SessionName:  semconv.SessionName(project, branch),
			DryRun:       templateDryRun,
		}, map[string]string{
			semconv.HookAttrProject:      project,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: absDir,
		})
		if err != nil {
			if !templateDryRun {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return err
		}

		if templateDryRun {
			for _, f := range result.Files {
				fmt.Fprintf(cmd.OutOrStdout(), "# %s\n%s\n", f.Target, f.Output)
			}
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "done")
		for _, f := range result.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f.Target)
		}
		return nil
	},
}

// resolveProjectFromDir matches a directory against configured projects by checking
// if the directory is inside any project's clone dir or worktrees root.
func resolveProjectFromDir(absDir string) string {
	for name, p := range cfg.Projects {
		if p.Repo == "" {
			continue
		}
		repoPath, err := config.RepoPath(p.Repo)
		if err != nil {
			continue
		}
		cloneDir := semconv.CloneDir(cfg.Defaults.ProjectsDir, repoPath)
		worktreesRoot := semconv.WorktreesRoot(cfg.Defaults.ProjectsDir, repoPath)

		if absDir == cloneDir || strings.HasPrefix(absDir, cloneDir+string(os.PathSeparator)) {
			return name
		}
		if absDir == worktreesRoot || strings.HasPrefix(absDir, worktreesRoot+string(os.PathSeparator)) {
			return name
		}
	}
	return ""
}

// detectGitBranch runs git rev-parse to get the current branch name.
func detectGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func init() {
	templateCmd.Flags().StringVar(&templateProject, "project", "", "project name (auto-detected from directory)")
	templateCmd.Flags().StringVar(&templateBranch, "branch", "", "branch name (auto-detected from git)")
	templateCmd.Flags().BoolVar(&templateDryRun, "dry-run", false, "print rendered output without writing")
	rootCmd.AddCommand(templateCmd)
}
```

**Step 4: Run tests**

Run: `go test ./cmd/... -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add cmd/template.go cmd/template_test.go
git commit -m "feat: add ch template command for ad-hoc template processing"
```

---

### Task 6: Verify and clean up

**Files:**
- All modified files

**Step 1: Run full test suite with coverage**

Run: `make check`
Expected: Coverage >= 80%, all tests pass, lint clean, build succeeds

**Step 2: If coverage is below 80%, add tests to cover gaps**

Focus on:
- `resolveProjectFromDir` edge cases (no match, multiple projects, worktrees root match)
- `detectGitBranch` in non-git directory
- `ch template` with `--project` and `--branch` flags
- `ch worktree template` with `--dry-run`

**Step 3: Run make check again after adding any new tests**

Run: `make check`
Expected: ALL PASS

**Step 4: Commit any additional tests**

```bash
git add -A
git commit -m "test: add coverage for template commands and project resolution"
```
