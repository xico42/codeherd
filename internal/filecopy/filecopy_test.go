package filecopy

import (
	"fmt"
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

	if err := os.WriteFile(filepath.Join(baseDir, "CLAUDE.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	if err := os.WriteFile(srcFile, []byte("rules"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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

	if err := os.MkdirAll(filepath.Join(baseDir, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "src", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(baseDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(baseDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{failOn: semconv.HookPreCopy}
	svc := New(mock)

	err := svc.Copy([]string{"file.txt"}, baseDir, targetDir, nil)
	if err == nil {
		t.Error("expected error when pre-copy hook fails")
	}

	if _, statErr := os.Stat(filepath.Join(targetDir, "file.txt")); statErr == nil {
		t.Error("file should not be copied when pre-copy hook fails")
	}
}

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
