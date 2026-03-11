# Hooks Lifecycle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add hook lifecycle management, file copy, and `.herd` template processing to codeherd's workflow.

**Architecture:** Each lifecycle step (clone, worktree, copy, template, session) has pre/post hooks configured per-project in `config.toml`. A `hooks.Hook` interface is injected into services. New `filecopy` and `herdtemplate` service packages handle file copying and `.herd` template rendering. `cmd/` and TUI orchestrate the chain.

**Tech Stack:** Go, TOML config, `os/exec` for shell hooks, `text/template` for `.herd` files.

---

### Task 1: Add semconv hook constants

**Files:**
- Modify: `internal/semconv/semconv.go`
- Modify: `internal/semconv/semconv_test.go`

**Step 1: Add hook name and attribute constants to semconv.go**

Add after the existing `CodeherdSessionName` constant block in `internal/semconv/semconv.go`:

```go
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

// Hook attributes — environment variable names passed to hook commands.
const (
	HookAttrProject      = "CODEHERD_PROJECT"
	HookAttrBranch       = "CODEHERD_BRANCH"
	HookAttrRepo         = "CODEHERD_REPO"
	HookAttrCloneDir     = "CODEHERD_CLONE_DIR"
	HookAttrWorktreePath = "CODEHERD_WORKTREE_PATH"
	HookAttrSessionName  = "CODEHERD_SESSION_NAME"
)
```

**Step 2: Write tests to validate constants**

Add to `internal/semconv/semconv_test.go`:

```go
func TestHookConstants_NotEmpty(t *testing.T) {
	hooks := []string{
		semconv.HookPreClone, semconv.HookPostClone,
		semconv.HookPreWorktree, semconv.HookPostWorktree,
		semconv.HookPreCopy, semconv.HookPostCopy,
		semconv.HookPreTemplate, semconv.HookPostTemplate,
		semconv.HookPreSession, semconv.HookPostSession,
	}
	for _, h := range hooks {
		if h == "" {
			t.Errorf("hook constant is empty")
		}
	}
}

func TestHookAttrConstants_HavePrefix(t *testing.T) {
	attrs := []string{
		semconv.HookAttrProject, semconv.HookAttrBranch,
		semconv.HookAttrRepo, semconv.HookAttrCloneDir,
		semconv.HookAttrWorktreePath, semconv.HookAttrSessionName,
	}
	for _, a := range attrs {
		if !strings.HasPrefix(a, "CODEHERD_") {
			t.Errorf("attribute %q missing CODEHERD_ prefix", a)
		}
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/semconv/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/semconv/semconv.go internal/semconv/semconv_test.go
git commit -m "feat: add hook lifecycle and attribute constants to semconv"
```

---

### Task 2: Add config structs for hooks and files

**Files:**
- Modify: `internal/config/project.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write test for parsing hooks and files from TOML**

Add to `internal/config/config_test.go`:

```go
func TestLoad_HooksAndFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[projects.myapp]
repo = "git@github.com:user/myapp.git"
files = ["CLAUDE.md", "~/.config/prompts/safety.md:RULES.md"]

[projects.myapp.hooks]
pre-clone = "echo preparing"
post-clone = "make deps"
pre-worktree = "echo wt"
post-worktree = "npm install"
pre-copy = "echo copy"
post-copy = "chmod 600 .env"
pre-template = "echo tmpl"
post-template = "echo done"
pre-session = "docker compose up -d"
post-session = "curl http://example.com"
`
	os.WriteFile(path, []byte(content), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	p := cfg.Projects["myapp"]

	if len(p.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(p.Files))
	}
	if p.Files[0] != "CLAUDE.md" {
		t.Errorf("Files[0] = %q", p.Files[0])
	}

	if p.Hooks.PreClone != "echo preparing" {
		t.Errorf("PreClone = %q", p.Hooks.PreClone)
	}
	if p.Hooks.PostClone != "make deps" {
		t.Errorf("PostClone = %q", p.Hooks.PostClone)
	}
	if p.Hooks.PreWorktree != "echo wt" {
		t.Errorf("PreWorktree = %q", p.Hooks.PreWorktree)
	}
	if p.Hooks.PostWorktree != "npm install" {
		t.Errorf("PostWorktree = %q", p.Hooks.PostWorktree)
	}
	if p.Hooks.PreCopy != "echo copy" {
		t.Errorf("PreCopy = %q", p.Hooks.PreCopy)
	}
	if p.Hooks.PostCopy != "chmod 600 .env" {
		t.Errorf("PostCopy = %q", p.Hooks.PostCopy)
	}
	if p.Hooks.PreTemplate != "echo tmpl" {
		t.Errorf("PreTemplate = %q", p.Hooks.PreTemplate)
	}
	if p.Hooks.PostTemplate != "echo done" {
		t.Errorf("PostTemplate = %q", p.Hooks.PostTemplate)
	}
	if p.Hooks.PreSession != "docker compose up -d" {
		t.Errorf("PreSession = %q", p.Hooks.PreSession)
	}
	if p.Hooks.PostSession != "curl http://example.com" {
		t.Errorf("PostSession = %q", p.Hooks.PostSession)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestLoad_HooksAndFiles -v`
Expected: FAIL — `Files` and `Hooks` fields don't exist yet

**Step 3: Add HooksConfig and update ProjectConfig in project.go**

In `internal/config/project.go`, add after the `ProjectConfig` struct:

```go
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
```

Add `Files` and `Hooks` fields to `ProjectConfig`:

```go
type ProjectConfig struct {
	Repo          string      `toml:"repo"           validate:"omitempty"`
	DefaultBranch string      `toml:"default_branch" validate:"omitempty"`
	EnvTemplate   string      `toml:"env_template"   validate:"omitempty"`
	Files         []string    `toml:"files"          validate:"omitempty"`
	Hooks         HooksConfig `toml:"hooks"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestLoad_HooksAndFiles -v`
Expected: PASS

**Step 5: Run full config test suite**

Run: `go test ./internal/config/...`
Expected: PASS — existing tests unaffected

**Step 6: Commit**

```bash
git add internal/config/project.go internal/config/config_test.go
git commit -m "feat: add HooksConfig and Files to ProjectConfig"
```

---

### Task 3: Create hooks package

**Files:**
- Create: `internal/hooks/hooks.go`
- Create: `internal/hooks/hooks_test.go`

**Step 1: Write failing tests**

Create `internal/hooks/hooks_test.go`:

```go
package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

func TestTrigger_EmptyCommand_NoOp(t *testing.T) {
	h := New(config.HooksConfig{})
	err := h.Trigger(semconv.HookPreClone, nil, "")
	if err != nil {
		t.Errorf("empty hook should be no-op, got %v", err)
	}
}

func TestTrigger_SuccessfulCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	h := New(config.HooksConfig{PreClone: "true"})
	err := h.Trigger(semconv.HookPreClone, nil, "")
	if err != nil {
		t.Errorf("successful command should not error, got %v", err)
	}
}

func TestTrigger_FailingCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	h := New(config.HooksConfig{PreClone: "false"})
	err := h.Trigger(semconv.HookPreClone, nil, "")
	if err == nil {
		t.Error("failing command should return error")
	}
}

func TestTrigger_EnvironmentVariables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.txt")
	cmd := "echo $CODEHERD_PROJECT > " + outFile
	h := New(config.HooksConfig{PreClone: cmd})

	attrs := map[string]string{
		semconv.HookAttrProject: "myapp",
	}
	err := h.Trigger(semconv.HookPreClone, attrs, "")
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	got := string(data)
	if got != "myapp\n" {
		t.Errorf("env var not passed: got %q, want %q", got, "myapp\n")
	}
}

func TestTrigger_WorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	dir := t.TempDir()
	outFile := filepath.Join(dir, "pwd.txt")
	cmd := "pwd > " + outFile
	h := New(config.HooksConfig{PostClone: cmd})

	err := h.Trigger(semconv.HookPostClone, nil, dir)
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	// Resolve symlinks for macOS /private/tmp
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	got := string(data)
	if got != resolvedDir+"\n" && got != dir+"\n" {
		t.Errorf("working dir wrong: got %q, want %q", got, dir)
	}
}

func TestTrigger_UnknownHookName_NoOp(t *testing.T) {
	h := New(config.HooksConfig{PreClone: "echo hello"})
	err := h.Trigger("unknown-hook", nil, "")
	if err != nil {
		t.Errorf("unknown hook should be no-op, got %v", err)
	}
}

func TestTrigger_ErrorIncludesHookName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	h := New(config.HooksConfig{PreClone: "exit 42"})
	err := h.Trigger(semconv.HookPreClone, nil, "")
	if err == nil {
		t.Fatal("expected error")
	}
	errStr := err.Error()
	if !containsAll(errStr, "pre-clone", "exit 42") {
		t.Errorf("error should mention hook name and command: %q", errStr)
	}
}

func TestNoOp_Trigger(t *testing.T) {
	h := &NoOp{}
	if err := h.Trigger("anything", nil, ""); err != nil {
		t.Errorf("NoOp.Trigger() error = %v", err)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/hooks/...`
Expected: FAIL — package doesn't exist

**Step 3: Implement hooks package**

Create `internal/hooks/hooks.go`:

```go
package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// Hook triggers lifecycle hooks by name.
type Hook interface {
	Trigger(name string, attrs map[string]string, workDir string) error
}

// Service maps hook names to configured commands and executes them.
type Service struct {
	hooks map[string]string
}

// New creates a Hook from the given config.
func New(cfg config.HooksConfig) *Service {
	return &Service{
		hooks: map[string]string{
			semconv.HookPreClone:     cfg.PreClone,
			semconv.HookPostClone:    cfg.PostClone,
			semconv.HookPreWorktree:  cfg.PreWorktree,
			semconv.HookPostWorktree: cfg.PostWorktree,
			semconv.HookPreCopy:      cfg.PreCopy,
			semconv.HookPostCopy:     cfg.PostCopy,
			semconv.HookPreTemplate:  cfg.PreTemplate,
			semconv.HookPostTemplate: cfg.PostTemplate,
			semconv.HookPreSession:   cfg.PreSession,
			semconv.HookPostSession:  cfg.PostSession,
		},
	}
}

// Trigger runs the hook command for the given name. Returns nil if the hook
// is not configured (empty command). Returns an error on non-zero exit code.
func (s *Service) Trigger(name string, attrs map[string]string, workDir string) error {
	command := s.hooks[name]
	if command == "" {
		return nil
	}

	cmd := exec.Command("sh", "-c", command)

	// Inherit current environment and add hook attributes.
	cmd.Env = os.Environ()
	for k, v := range attrs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %q failed (command: %s): %w\n%s",
			name, command, err, stderr.String())
	}
	return nil
}

// NoOp is a Hook that does nothing. Used in tests and when hooks are not configured.
type NoOp struct{}

// Trigger always returns nil.
func (n *NoOp) Trigger(name string, attrs map[string]string, workDir string) error {
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/hooks/
git commit -m "feat: add hooks package with Hook interface and shell executor"
```

---

### Task 4: Create filecopy package

**Files:**
- Create: `internal/filecopy/filecopy.go`
- Create: `internal/filecopy/filecopy_test.go`

**Step 1: Write failing tests**

Create `internal/filecopy/filecopy_test.go`:

```go
package filecopy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

func TestParseEntry_SamePath(t *testing.T) {
	src, dst := parseEntry("CLAUDE.md")
	if src != "CLAUDE.md" || dst != "CLAUDE.md" {
		t.Errorf("parseEntry(%q) = %q, %q", "CLAUDE.md", src, dst)
	}
}

func TestParseEntry_WithColon(t *testing.T) {
	src, dst := parseEntry("~/.config/prompts/safety.md:RULES.md")
	if src != "~/.config/prompts/safety.md" || dst != "RULES.md" {
		t.Errorf("parseEntry(%q) = %q, %q", "~/.config/prompts/safety.md:RULES.md", src, dst)
	}
}

func TestParseEntry_NestedPath(t *testing.T) {
	src, dst := parseEntry("src/config.json")
	if src != "src/config.json" || dst != "src/config.json" {
		t.Errorf("parseEntry(%q) = %q, %q", "src/config.json", src, dst)
	}
}

func TestCopy_SamePathEntry(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()

	// Create source file.
	os.WriteFile(filepath.Join(baseDir, "CLAUDE.md"), []byte("hello"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Copy([]string{"CLAUDE.md"}, baseDir, targetDir, nil)
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}

func TestCopy_ColonEntry(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()
	srcDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "safety.md")
	os.WriteFile(srcFile, []byte("rules"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Copy([]string{srcFile + ":RULES.md"}, baseDir, targetDir, nil)
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "RULES.md"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(data) != "rules" {
		t.Errorf("content = %q, want %q", string(data), "rules")
	}
}

func TestCopy_CreatesIntermediateDirectories(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()

	// Create nested source file.
	os.MkdirAll(filepath.Join(baseDir, "src"), 0o755)
	os.WriteFile(filepath.Join(baseDir, "src", "config.json"), []byte("{}"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Copy([]string{"src/config.json"}, baseDir, targetDir, nil)
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "src", "config.json")); err != nil {
		t.Errorf("intermediate dir not created: %v", err)
	}
}

func TestCopy_SourceNotFound(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()

	svc := New(&hooks.NoOp{})
	err := svc.Copy([]string{"nonexistent.md"}, baseDir, targetDir, nil)
	if err == nil {
		t.Error("expected error for missing source file")
	}
}

func TestCopy_EmptyFilesList(t *testing.T) {
	svc := New(&hooks.NoOp{})
	err := svc.Copy(nil, "/tmp", "/tmp", nil)
	if err != nil {
		t.Errorf("empty files list should be no-op, got %v", err)
	}
}

func TestCopy_TriggersHooks(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(baseDir, "file.txt"), []byte("x"), 0o644)

	mock := &mockHook{}
	svc := New(mock)

	attrs := map[string]string{semconv.HookAttrProject: "myapp"}
	err := svc.Copy([]string{"file.txt"}, baseDir, targetDir, attrs)
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(mock.calls))
	}
	if mock.calls[0].name != semconv.HookPreCopy {
		t.Errorf("first hook = %q, want %q", mock.calls[0].name, semconv.HookPreCopy)
	}
	if mock.calls[1].name != semconv.HookPostCopy {
		t.Errorf("second hook = %q, want %q", mock.calls[1].name, semconv.HookPostCopy)
	}
}

func TestCopy_HookFailureStops(t *testing.T) {
	baseDir := t.TempDir()
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(baseDir, "file.txt"), []byte("x"), 0o644)

	mock := &mockHook{failOn: semconv.HookPreCopy}
	svc := New(mock)

	err := svc.Copy([]string{"file.txt"}, baseDir, targetDir, nil)
	if err == nil {
		t.Error("expected error when pre-copy hook fails")
	}

	// File should not have been copied.
	if _, statErr := os.Stat(filepath.Join(targetDir, "file.txt")); statErr == nil {
		t.Error("file should not be copied when pre-copy hook fails")
	}
}

// mockHook records Trigger calls.
type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}
```

Note: add `"fmt"` to the imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/filecopy/...`
Expected: FAIL — package doesn't exist

**Step 3: Implement filecopy package**

Create `internal/filecopy/filecopy.go`:

```go
package filecopy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

// Service copies files into worktrees.
type Service struct {
	hook hooks.Hook
}

// New creates a file copy service.
func New(hook hooks.Hook) *Service {
	return &Service{hook: hook}
}

// Copy processes the files list, copying each entry from baseDir (or absolute
// path) to targetDir. Triggers pre-copy and post-copy hooks.
func (s *Service) Copy(files []string, baseDir, targetDir string, attrs map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	if err := s.hook.Trigger(semconv.HookPreCopy, attrs, targetDir); err != nil {
		return err
	}

	for _, entry := range files {
		src, dst := parseEntry(entry)
		srcPath := resolveSrc(src, baseDir)
		dstPath := filepath.Join(targetDir, dst)

		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copying %s: %w", entry, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostCopy, attrs, targetDir); err != nil {
		return err
	}

	return nil
}

// parseEntry splits a file entry into source and destination.
// "file.md" → ("file.md", "file.md")
// "src:dst" → ("src", "dst")
func parseEntry(entry string) (src, dst string) {
	if idx := strings.LastIndex(entry, ":"); idx > 0 {
		return entry[:idx], entry[idx+1:]
	}
	return entry, entry
}

// resolveSrc resolves a source path. Absolute paths and ~ paths are returned
// as-is (with ~ expanded). Relative paths are joined with baseDir.
func resolveSrc(src, baseDir string) string {
	if strings.HasPrefix(src, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return src
		}
		return filepath.Join(home, src[2:])
	}
	if filepath.IsAbs(src) {
		return src
	}
	return filepath.Join(baseDir, src)
}

// copyFile copies a single file, creating intermediate directories.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/filecopy/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/filecopy/
git commit -m "feat: add filecopy package for worktree file copying"
```

---

### Task 5: Create herdtemplate package

**Files:**
- Create: `internal/herdtemplate/herdtemplate.go`
- Create: `internal/herdtemplate/herdtemplate_test.go`

**Step 1: Write failing tests**

Create `internal/herdtemplate/herdtemplate_test.go`:

```go
package herdtemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

func TestProcess_RendersHerdFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("PORT={{ port \"http\" }}\n"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) == 0 {
		t.Error("output file is empty")
	}
}

func TestProcess_StripsHerdSuffix(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml.herd"), []byte("version: '3'\n"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Errorf("output file not created: %v", err)
	}
}

func TestProcess_NoHerdFiles_NoOp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
}

func TestProcess_TemplateContext(t *testing.T) {
	dir := t.TempDir()
	tmpl := "project={{ .Project }}\nbranch={{ .Branch }}\npath={{ .WorktreePath }}\nsession={{ .SessionName }}\n"
	os.WriteFile(filepath.Join(dir, "info.txt.herd"), []byte(tmpl), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "info.txt"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	got := string(data)
	if got != "project=myapp\nbranch=feature\npath="+dir+"\nsession=myapp-feature\n" {
		t.Errorf("content = %q", got)
	}
}

func TestProcess_PortFunction(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ports.herd"), []byte(`{{ port "http" }}`), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ports"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	// Port should be a number in range 10000-59999.
	got := string(data)
	if len(got) < 5 {
		t.Errorf("port output too short: %q", got)
	}
}

func TestProcess_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644)

	mock := &mockHook{}
	svc := New(mock)

	attrs := map[string]string{semconv.HookAttrProject: "myapp"}
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, attrs)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(mock.calls))
	}
	if mock.calls[0].name != semconv.HookPreTemplate {
		t.Errorf("first hook = %q, want %q", mock.calls[0].name, semconv.HookPreTemplate)
	}
	if mock.calls[1].name != semconv.HookPostTemplate {
		t.Errorf("second hook = %q, want %q", mock.calls[1].name, semconv.HookPostTemplate)
	}
}

func TestProcess_PreHookFailureStops(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644)

	mock := &mockHook{failOn: semconv.HookPreTemplate}
	svc := New(mock)

	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error when pre-template hook fails")
	}

	// Output file should not exist.
	if _, statErr := os.Stat(filepath.Join(dir, "test")); statErr == nil {
		t.Error("template should not be processed when pre-template hook fails")
	}
}

func TestProcess_BadTemplate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.herd"), []byte("{{ .Invalid | bad }}"), 0o644)

	svc := New(&hooks.NoOp{})
	err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error for bad template")
	}
}

// mockHook records Trigger calls.
type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/herdtemplate/...`
Expected: FAIL — package doesn't exist

**Step 3: Implement herdtemplate package**

Create `internal/herdtemplate/herdtemplate.go`:

```go
package herdtemplate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/xico42/codeherd/internal/envtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

const herdSuffix = ".herd"

// ProcessContext holds values available to .herd templates.
type ProcessContext struct {
	Project      string
	Branch       string
	WorktreePath string
	SessionName  string
}

// Service processes .herd template files in worktrees.
type Service struct {
	hook hooks.Hook
}

// New creates a template processing service.
func New(hook hooks.Hook) *Service {
	return &Service{hook: hook}
}

// Process walks the worktree directory, finds all .herd files, renders them,
// and writes the output without the .herd suffix. Triggers pre/post-template hooks.
func (s *Service) Process(ctx ProcessContext, attrs map[string]string) error {
	herdFiles, err := findHerdFiles(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("scanning for .herd files: %w", err)
	}

	if len(herdFiles) == 0 {
		return nil
	}

	if err := s.hook.Trigger(semconv.HookPreTemplate, attrs, ctx.WorktreePath); err != nil {
		return err
	}

	funcMap := template.FuncMap{
		"port": func(name string) int {
			return envtemplate.DeterministicPort(ctx.Project, ctx.Branch, name)
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

	for _, path := range herdFiles {
		if err := renderFile(path, ctx, funcMap); err != nil {
			return fmt.Errorf("rendering %s: %w", path, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostTemplate, attrs, ctx.WorktreePath); err != nil {
		return err
	}

	return nil
}

// findHerdFiles returns all .herd files in the directory tree.
func findHerdFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, herdSuffix) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// renderFile renders a single .herd template and writes the output.
func renderFile(path string, ctx ProcessContext, funcMap template.FuncMap) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	outPath := strings.TrimSuffix(path, herdSuffix)
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/herdtemplate/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/herdtemplate/
git commit -m "feat: add herdtemplate package for .herd template processing"
```

---

### Task 6: Integrate hooks into project.Service

**Files:**
- Modify: `internal/project/project.go:64-72` (Service struct and NewService)
- Modify: `internal/project/project.go:116-136` (Clone method)
- Modify: `internal/project/project_test.go`

**Step 1: Write failing test for hook integration**

Add to `internal/project/project_test.go`:

```go
// mockHook records Trigger calls.
type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

func TestClone_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	hookMock := &mockHook{}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
	})
	svc := project.NewService(cfg, mock, hookMock)
	if err := svc.Clone("myapp"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if len(hookMock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookMock.calls))
	}
	if hookMock.calls[0].name != semconv.HookPreClone {
		t.Errorf("first hook = %q, want %q", hookMock.calls[0].name, semconv.HookPreClone)
	}
	if hookMock.calls[1].name != semconv.HookPostClone {
		t.Errorf("second hook = %q, want %q", hookMock.calls[1].name, semconv.HookPostClone)
	}
	if hookMock.calls[0].attrs[semconv.HookAttrProject] != "myapp" {
		t.Errorf("project attr = %q", hookMock.calls[0].attrs[semconv.HookAttrProject])
	}
}

func TestClone_PreHookFailure_StopsClone(t *testing.T) {
	dir := t.TempDir()
	mock := &mockGitRunner{}
	hookMock := &mockHook{failOn: semconv.HookPreClone}
	cfg := makeConfig(dir, map[string]config.ProjectConfig{
		"myapp": {Repo: "git@github.com:user/myapp.git"},
	})
	svc := project.NewService(cfg, mock, hookMock)
	err := svc.Clone("myapp")
	if err == nil {
		t.Error("expected error when pre-clone hook fails")
	}
	if len(mock.calls) != 0 {
		t.Error("git clone should not be called when pre-clone hook fails")
	}
}
```

Add imports: `"github.com/xico42/codeherd/internal/semconv"` and `"fmt"`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/project/... -run TestClone_TriggersHooks -v`
Expected: FAIL — `NewService` signature doesn't accept Hook

**Step 3: Add Hook to project.Service**

Update `internal/project/project.go`:

1. Add import: `"github.com/xico42/codeherd/internal/hooks"` and `"github.com/xico42/codeherd/internal/semconv"`

2. Update `Service` struct:
```go
type Service struct {
	cfg  *config.Config
	git  GitRunner
	hook hooks.Hook
}
```

3. Update `NewService`:
```go
func NewService(cfg *config.Config, git GitRunner, hook hooks.Hook) *Service {
	return &Service{cfg: cfg, git: git, hook: hook}
}
```

4. Update `Clone` method to trigger hooks. Add hook calls around the git clone:
```go
func (s *Service) Clone(name string) error {
	p, ok := s.cfg.Projects[name]
	if !ok {
		return fmt.Errorf("project %q is not configured", name)
	}
	repoPath, err := config.RepoPath(p.Repo)
	if err != nil {
		return fmt.Errorf("cannot parse repo URL %q: %w", p.Repo, err)
	}
	absPath := filepath.Join(s.cfg.Defaults.ProjectsDir, repoPath)
	if _, err := os.Stat(absPath); err == nil {
		return &AlreadyClonedError{Path: absPath}
	}

	attrs := map[string]string{
		semconv.HookAttrProject:  name,
		semconv.HookAttrRepo:     p.Repo,
		semconv.HookAttrCloneDir: absPath,
	}

	if err := s.hook.Trigger(semconv.HookPreClone, attrs, s.cfg.Defaults.ProjectsDir); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	if err := s.git.Clone(p.Repo, absPath, p.DefaultBranch); err != nil {
		return fmt.Errorf("cloning repository: %w", err)
	}

	if err := s.hook.Trigger(semconv.HookPostClone, attrs, s.cfg.Defaults.ProjectsDir); err != nil {
		return err
	}

	return nil
}
```

**Step 4: Update existing tests and callers**

All existing `project.NewService(cfg, mock)` calls now need a third argument. Add `&hooks.NoOp{}` (or a local `noopHook` if you prefer not to import hooks in test):

Update `internal/project/project_test.go`: change all `project.NewService(cfg, &mockGitRunner{})` to `project.NewService(cfg, &mockGitRunner{}, &mockHook{})` — but since `mockHook` is defined in the test file, you need to add `hooks.Hook` import. Actually, since `mockHook` implements the interface, just add a no-fail mock. For tests that don't care about hooks, use `&mockHook{}` (which never fails).

Update `cmd/project.go`: change all `project.NewService(cfg, project.NewRealGitRunner())` to `project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})`. Import `"github.com/xico42/codeherd/internal/hooks"`.

Update `cmd/root.go` (`runTUIDirect`): change `project.NewService(cfg, project.NewRealGitRunner())` to `project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})`.

**Step 5: Run all tests**

Run: `go test ./internal/project/... ./cmd/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/project/project.go internal/project/project_test.go cmd/project.go cmd/root.go
git commit -m "feat: integrate hooks into project.Service"
```

---

### Task 7: Integrate hooks into worktree.Service

**Files:**
- Modify: `internal/worktree/worktree.go:146-156` (Service struct and NewService)
- Modify: `internal/worktree/worktree.go:193-242` (New method)
- Modify: `internal/worktree/worktree.go:245-291` (NewFrom method)
- Modify: `internal/worktree/worktree_test.go`

**Step 1: Write failing test for hook integration**

Add to `internal/worktree/worktree_test.go` (using the existing `mockGit` and test patterns):

```go
func TestNew_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	cloneDir := filepath.Join(dir, "github.com", "user", "myapp")
	os.MkdirAll(cloneDir, 0o755)

	mock := &mockGit{}
	hookMock := &mockHook{}
	cfg := &config.Config{
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	cfg.Defaults.ProjectsDir = dir
	tc := tmux.NewClient(&mockTmuxRunner{})
	svc := NewService(cfg, mock, tc, hookMock)

	_, err := svc.New("myapp", "feature")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Should have pre-worktree and post-worktree.
	hookNames := make([]string, len(hookMock.calls))
	for i, c := range hookMock.calls {
		hookNames[i] = c.name
	}
	if hookNames[0] != semconv.HookPreWorktree {
		t.Errorf("first hook = %q, want %q", hookNames[0], semconv.HookPreWorktree)
	}
	if hookNames[1] != semconv.HookPostWorktree {
		t.Errorf("second hook = %q, want %q", hookNames[1], semconv.HookPostWorktree)
	}
}
```

Add `mockHook` and `hookCall` types to the test file (same pattern as project_test.go).

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -run TestNew_TriggersHooks -v`
Expected: FAIL — `NewService` signature doesn't accept Hook

**Step 3: Add Hook to worktree.Service**

Update `internal/worktree/worktree.go`:

1. Add import: `"github.com/xico42/codeherd/internal/hooks"` (semconv already imported)

2. Update `Service` struct:
```go
type Service struct {
	cfg  *config.Config
	git  WorktreeRunner
	tmux *tmux.Client
	hook hooks.Hook
}
```

3. Update `NewService`:
```go
func NewService(cfg *config.Config, git WorktreeRunner, tmux *tmux.Client, hook hooks.Hook) *Service {
	return &Service{cfg: cfg, git: git, tmux: tmux, hook: hook}
}
```

4. Update `New` method — add pre/post worktree hooks around the git worktree add:
```go
func (s *Service) New(project, branch string) (NewResult, error) {
	p, ok := s.cfg.Projects[project]
	if !ok {
		return NewResult{}, fmt.Errorf("project %q is not configured", project)
	}

	cloneDir, worktreesRoot, worktreePath, err := s.resolvePaths(project, branch)
	if err != nil {
		return NewResult{}, err
	}

	if _, err := os.Stat(cloneDir); os.IsNotExist(err) {
		return NewResult{}, fmt.Errorf("%w: %s", ErrNotCloned, project)
	}

	if _, err := os.Stat(worktreePath); err == nil {
		return NewResult{}, fmt.Errorf("%w: %s/%s", ErrWorktreeExists, project, branch)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      project,
		semconv.HookAttrBranch:       branch,
		semconv.HookAttrRepo:         p.Repo,
		semconv.HookAttrCloneDir:     cloneDir,
		semconv.HookAttrWorktreePath: worktreePath,
	}

	if err := s.hook.Trigger(semconv.HookPreWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, err
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return NewResult{}, fmt.Errorf("creating worktrees dir: %w", err)
	}

	addErr := s.git.Add(cloneDir, worktreePath, branch)
	if addErr != nil {
		if err := s.git.AddNewBranch(cloneDir, worktreePath, branch); err != nil {
			return NewResult{}, fmt.Errorf("failed to create worktree (add: %v; add -b: %w)", addErr, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostWorktree, attrs, worktreePath); err != nil {
		return NewResult{}, err
	}

	result := NewResult{Path: worktreePath}

	// Legacy env template processing (kept for backward compatibility).
	content, source, _ := resolveTemplate(worktreePath, p)
	if content != "" {
		ctx := envtemplate.EnvTemplateContext{
			Project:      project,
			Branch:       branch,
			WorktreePath: worktreePath,
			SessionName:  semconv.SessionName(project, branch),
		}
		if rendered, renderErr := envtemplate.Process(content, source, ctx); renderErr == nil {
			envPath := filepath.Join(worktreePath, ".env")
			if writeErr := os.WriteFile(envPath, []byte(rendered), 0o600); writeErr == nil {
				result.EnvWritten = true
			}
		}
	}

	return result, nil
}
```

5. Apply same pattern to `NewFrom` method.

**Step 4: Update all callers**

- `cmd/worktree.go` `newWorktreeService()`: add `&hooks.NoOp{}` as fourth arg — but this should use actual hooks from project config. For now use NoOp, we'll wire it up properly in Task 9.
- `cmd/session.go`: same, uses `newWorktreeService()` which is the central factory.
- `cmd/root.go` `runTUIDirect()`: update `worktree.NewService` call.
- `internal/tui/` files don't construct services directly — they receive them. No changes needed.

Update `cmd/worktree.go`:
```go
func newWorktreeService() *worktree.Service {
	return worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), &hooks.NoOp{})
}
```

Update `cmd/root.go` `runTUIDirect()`:
```go
wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, &hooks.NoOp{})
```

**Step 5: Run all tests**

Run: `go test ./internal/worktree/... ./cmd/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go cmd/worktree.go cmd/session.go cmd/root.go
git commit -m "feat: integrate hooks into worktree.Service"
```

---

### Task 8: Integrate hooks into session.Service

**Files:**
- Modify: `internal/session/session.go:36-43` (Service struct and NewService)
- Modify: `internal/session/session.go:60-103` (Start method)
- Modify: `internal/session/session_test.go`

**Step 1: Write failing test for hook integration**

Add to `internal/session/session_test.go`:

```go
func TestStart_TriggersHooks(t *testing.T) {
	r2 := &mockRunnerSequence{responses: []mockResponse{
		{exitCode: 1},                 // list-sessions → no sessions
		{exitCode: 0},                 // new-session → ok
		{exitCode: 0},                 // set-option status
		{exitCode: 0},                 // set-option started_at
		{exitCode: 0},                 // set-option canonical_name
		{exitCode: 0},                 // set-option session_type
		{exitCode: 0, stdout: "$1\n"}, // display-message → session_id
	}}
	tc := tmux.NewClient(r2)
	hookMock := &mockHook{}
	svc := session.NewService(tc, hookMock)

	wtDir := t.TempDir()
	_, err := svc.Start(session.StartRequest{
		Project: "myapp",
		Branch:  "feature",
		Path:    wtDir,
		Cmd:     "claude",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if len(hookMock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hookMock.calls))
	}
	if hookMock.calls[0].name != semconv.HookPreSession {
		t.Errorf("first hook = %q, want %q", hookMock.calls[0].name, semconv.HookPreSession)
	}
	if hookMock.calls[1].name != semconv.HookPostSession {
		t.Errorf("second hook = %q, want %q", hookMock.calls[1].name, semconv.HookPostSession)
	}
}
```

Add `mockHook` to the test file.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run TestStart_TriggersHooks -v`
Expected: FAIL — `NewService` signature doesn't accept Hook

**Step 3: Add Hook to session.Service**

Update `internal/session/session.go`:

1. Add import: `"github.com/xico42/codeherd/internal/hooks"`

2. Update `Service` struct:
```go
type Service struct {
	tmux *tmux.Client
	hook hooks.Hook
}
```

3. Update `NewService`:
```go
func NewService(tmux *tmux.Client, hook hooks.Hook) *Service {
	return &Service{tmux: tmux, hook: hook}
}
```

4. Update `Start` method — add hook triggers:
```go
func (s *Service) Start(req StartRequest) (string, error) {
	name := semconv.SessionName(req.Project, req.Branch)

	// Check for existing session by canonical name.
	records, err := s.tmux.ListSessions()
	if err != nil {
		return "", fmt.Errorf("checking session: %w", err)
	}
	for _, r := range records {
		if r.CanonicalName == name {
			return "", &SessionExistsError{Name: name}
		}
	}

	if _, err := os.Stat(req.Path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrPathNotFound, req.Path)
		}
		return "", fmt.Errorf("checking path: %w", err)
	}

	attrs := map[string]string{
		semconv.HookAttrProject:      req.Project,
		semconv.HookAttrBranch:       req.Branch,
		semconv.HookAttrWorktreePath: req.Path,
		semconv.HookAttrSessionName:  name,
	}

	if err := s.hook.Trigger(semconv.HookPreSession, attrs, req.Path); err != nil {
		return "", err
	}

	env := make(map[string]string)
	for k, v := range req.Env {
		env[k] = v
	}
	env[semconv.SessionEnvVar] = name

	if err := s.tmux.NewSessionWithEnv(name, req.Path, env, req.Cmd); err != nil {
		return "", fmt.Errorf("creating tmux session: %w", err)
	}

	now := time.Now().UTC()
	_ = s.tmux.SetOption(name, semconv.TmuxOptionStatus, semconv.StatusRunning)
	_ = s.tmux.SetOption(name, semconv.TmuxOptionStartedAt, now.Format(time.RFC3339))
	_ = s.tmux.SetOption(name, semconv.TmuxOptionCanonicalName, name)
	_ = s.tmux.SetOption(name, semconv.TmuxOptionSessionType, semconv.SessionTypeAgent)

	id, err := s.tmux.SessionID(name)
	if err != nil {
		return "", fmt.Errorf("getting session id: %w", err)
	}

	if err := s.hook.Trigger(semconv.HookPostSession, attrs, req.Path); err != nil {
		return "", err
	}

	return id, nil
}
```

**Step 4: Update all callers**

- `cmd/session.go` `newSessionService()`: add `&hooks.NoOp{}` — will be wired properly in Task 9.
```go
func newSessionService() *session.Service {
	tc := tmux.NewClient(tmux.NewRealRunner())
	return session.NewService(tc, &hooks.NoOp{})
}
```

- All existing tests that call `session.NewService(tc)` need updating to `session.NewService(tc, &mockHook{})`.

- `cmd/plugin.go` if it creates a session.Service — check and update.

**Step 5: Run all tests**

Run: `go test ./internal/session/... ./cmd/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go cmd/session.go cmd/plugin.go
git commit -m "feat: integrate hooks into session.Service"
```

---

### Task 9: Wire hooks from config into cmd/ and TUI

**Files:**
- Modify: `cmd/session.go`
- Modify: `cmd/worktree.go`
- Modify: `cmd/project.go`
- Modify: `cmd/root.go`

Now that all services accept hooks, wire real hooks from project config instead of `&hooks.NoOp{}`.

**Step 1: Update cmd/session.go**

The `session start` command knows the project name from args. Create hooks from project config:

```go
// In sessionStartCmd.RunE, after resolving project name:
projCfg := cfg.Projects[project]
h := hooks.New(projCfg.Hooks)
```

Pass `h` when creating services:

```go
wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
// ...
sesSvc := session.NewService(tc, h)
```

Remove the generic `newSessionService()` and `newWorktreeService()` functions — they can't know the project at construction time. Instead, construct services inline where the project name is available.

For commands that don't know the project (e.g., `session list`, `session stop`), use `&hooks.NoOp{}`.

**Step 2: Update cmd/worktree.go**

Same approach — `worktree new`, `worktree env` know the project. Construct hooks inline:

```go
// In worktreeNewCmd.RunE:
projCfg := cfg.Projects[project]
h := hooks.New(projCfg.Hooks)
svc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmux.NewClient(tmux.NewRealRunner()), h)
```

For `worktree list`, `worktree delete`, `worktree shell` — use NoOp hooks (no lifecycle events).

**Step 3: Add filecopy and herdtemplate to worktree new command**

In `worktreeNewCmd.RunE`, after worktree creation succeeds, add file copy and template processing:

```go
// After svc.New() or svc.NewFrom() returns:
projCfg := cfg.Projects[project]
if len(projCfg.Files) > 0 {
	cloneDir := /* resolve from config */
	copySvc := filecopy.New(h)
	if err := copySvc.Copy(projCfg.Files, cloneDir, result.Path, attrs); err != nil {
		return fmt.Errorf("copying files: %w", err)
	}
}

tmplSvc := herdtemplate.New(h)
tmplCtx := herdtemplate.ProcessContext{
	Project:      project,
	Branch:       branch,
	WorktreePath: result.Path,
	SessionName:  semconv.SessionName(project, branch),
}
if err := tmplSvc.Process(tmplCtx, attrs); err != nil {
	return fmt.Errorf("processing templates: %w", err)
}
```

**Step 4: Add filecopy and herdtemplate to session start command**

In `sessionStartCmd.RunE`, when auto-creating a worktree, add file copy and template processing after `wtSvc.New()`.

**Step 5: Update cmd/project.go**

In `projectCloneCmd.RunE`, create hooks from project config:

```go
// For single clone:
projCfg := cfg.Projects[name]
h := hooks.New(projCfg.Hooks)
svc := project.NewService(cfg, project.NewRealGitRunner(), h)

// For --all: iterate and create per-project hooks, or use NoOp
// (CloneAll iterates internally, so create service with NoOp and let each clone handle it)
```

Actually, for `--all`, since `CloneAll` calls `Clone` internally, and each project may have different hooks, we need to handle this differently. Option: make `Clone` look up hooks per project. Better: keep `NewService` with a NoOp for the `--all` case and handle hooks separately, OR restructure `CloneAll` to accept per-project hooks.

Simplest: for `--all`, create service with NoOp. Document that `clone --all` doesn't trigger hooks. Or: remove `CloneAll` and loop in the command layer, creating per-project services.

The cleaner approach: loop in cmd layer:
```go
if cloneAll {
	names := /* sorted project names */
	for _, name := range names {
		projCfg := cfg.Projects[name]
		h := hooks.New(projCfg.Hooks)
		svc := project.NewService(cfg, project.NewRealGitRunner(), h)
		err := svc.Clone(name)
		// handle result...
	}
}
```

**Step 6: Update cmd/root.go**

In `runTUIDirect()`, create services with project-aware hooks. Since TUI handles multiple projects, pass NoOp to the services — the TUI creates worktrees/sessions inline where it knows the project context.

Actually, since the TUI calls services that already have hooks injected, and those services trigger hooks internally, we need to think about this. The TUI receives pre-constructed services. For hooks to work per-project, we'd need to either:
- A) Create services per-project on-demand in TUI actions
- B) Pass NoOp to services, and handle hooks at the TUI action level

Option A is cleaner. The TUI actions in `actions.go` and `agent_picker.go` already construct closures. They can create per-project services.

For now, keep NoOp in the TUI service construction. Task 10 will integrate hooks into TUI properly.

**Step 7: Run all tests**

Run: `go test ./...`
Expected: PASS

**Step 8: Commit**

```bash
git add cmd/session.go cmd/worktree.go cmd/project.go cmd/root.go
git commit -m "feat: wire per-project hooks from config into commands"
```

---

### Task 10: Integrate hooks, filecopy, and herdtemplate into TUI

**Files:**
- Modify: `internal/tui/model.go:82-106` (NewModel and service construction)
- Modify: `internal/tui/actions.go` (attachAction, shellAction, cloneAction)
- Modify: `internal/tui/agent_picker.go` (submit method)
- Modify: `internal/tui/form.go` (submit method)

The TUI needs to run the full lifecycle chain. Since hooks are per-project and services are pre-constructed, the TUI needs to create per-project hook instances when it triggers lifecycle actions.

**Step 1: Pass config.HooksConfig through TUI actions**

The TUI already has access to `m.cfg`. When triggering clone/worktree/session actions, create a hooks instance from the project's config:

In `actions.go`, update `attachAction()` for the `groupProject` case:

```go
case groupProject:
	// ...
	projCfg := cfg.Projects[project]
	h := hooks.New(projCfg.Hooks)
	// Use h when creating worktree and session...
```

The challenge: the TUI services (`m.wtSvc`, `m.sesSvc`, `m.projSvc`) are pre-injected with NoOp hooks. To use per-project hooks, either:
- A) Create fresh services per action (expensive but correct)
- B) Add a method to services to swap hooks (breaks immutability)
- C) Create per-project services and stash them

Go with A — create services per action. It's only done when users trigger actions, so performance is not a concern.

**Step 2: Update attachAction groupProject case**

```go
case groupProject:
	// ...
	projCfg := cfg.Projects[project]
	h := hooks.New(projCfg.Hooks)
	wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
	sesSvc := session.NewService(tmuxClient, h)
	projSvc := project.NewService(cfg, project.NewRealGitRunner(), h)
	// Use these local services instead of m.wtSvc, m.sesSvc, m.projSvc
```

Wait — the TUI shouldn't import `worktree.NewRealWorktreeRunner()` etc. It should use factories.

Better approach: add a service factory to the Model, or pass the hook into the existing service constructors stored on Model.

Simplest: store hook constructors or create a helper. Actually, the TUI already creates closures that capture services. The key insight is that the TUI `actions.go` closures capture `wtSvc`, `sesSvc`, `projSvc` at the start of the method. We just need to capture per-project ones.

Since the TUI already stores the tmux client and uses it directly, we can construct fresh services in the closure:

```go
case groupProject:
	agents := cfg.AgentNames()
	// ...
	tmuxClient := m.tmuxClient
	projCfg := cfg.Projects[project]

	return m, func() tea.Msg {
		h := hooks.New(projCfg.Hooks)
		projSvc := project.NewService(cfg, project.NewRealGitRunner(), h)
		_ = projSvc.Clone(project)

		wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
		result, err := wtSvc.New(project, defaultBranch)
		if err != nil {
			return errMsg{err: err}
		}

		// File copy
		if len(projCfg.Files) > 0 {
			copySvc := filecopy.New(h)
			cloneDir := /* resolve */
			if err := copySvc.Copy(projCfg.Files, cloneDir, result.Path, nil); err != nil {
				return errMsg{err: err}
			}
		}

		// Template processing
		tmplSvc := herdtemplate.New(h)
		if err := tmplSvc.Process(herdtemplate.ProcessContext{
			Project:      project,
			Branch:       defaultBranch,
			WorktreePath: result.Path,
			SessionName:  semconv.SessionName(project, defaultBranch),
		}, nil); err != nil {
			return errMsg{err: err}
		}

		sesSvc := session.NewService(tmuxClient, h)
		sessionID, err := sesSvc.Start(session.StartRequest{...})
		// ...
	}
```

**Step 3: Apply the same pattern to:**
- `attachAction()` groupWorktree case
- `shellAction()` when creating worktree
- `cloneAction()`
- `agent_picker.go` `submit()` method
- `form.go` `submit()` method
- `startSessionAfterCreate()` method

**Step 4: Add filecopy and herdtemplate to form.go submit**

The form's `submit()` method creates a worktree. After creation, add file copy and template processing:

```go
func (f *formModel) submit() tea.Cmd {
	// ... existing code ...
	return func() tea.Msg {
		if projSvc != nil {
			_ = projSvc.Clone(project)
		}
		// ... worktree creation ...

		// File copy and template processing
		// Need access to cfg and hooks here...
	}
}
```

The form model needs access to `cfg` for files config and hooks. It already receives `cfg` in `newFormModel`. Store it and use in `submit()`.

**Step 5: Run all tests**

Run: `go test ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/tui/actions.go internal/tui/agent_picker.go internal/tui/form.go internal/tui/model.go
git commit -m "feat: integrate hooks, filecopy, and herdtemplate into TUI"
```

---

### Task 11: Add README documentation

**Files:**
- Create: `docs/hooks.md`

**Step 1: Write the README**

Create `docs/hooks.md` with the full documentation from the design doc. Include:

- Lifecycle mermaid diagram
- Configuration syntax
- File copy rules
- Template processing (.herd files)
- Environment variables per hook step
- Error handling behavior
- Working directory rules

Use the content from the design doc's "README.md" section (Design Section 7).

**Step 2: Commit**

```bash
git add docs/hooks.md
git commit -m "docs: add hooks lifecycle documentation"
```

---

### Task 12: Run full test suite and coverage check

**Step 1: Run all unit tests**

Run: `go test ./...`
Expected: PASS

**Step 2: Run coverage check**

Run: `make check`
Expected: PASS — coverage >= 80%, lint passes, build succeeds

**Step 3: Fix any issues found**

If coverage is below 80%, add tests for uncovered paths. Focus on:
- Hook error paths in services
- File copy edge cases
- Template processing edge cases

**Step 4: Final commit if fixes were needed**

```bash
git add -A
git commit -m "test: improve coverage for hooks lifecycle"
```
