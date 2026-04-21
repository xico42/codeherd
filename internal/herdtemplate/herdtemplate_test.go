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
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("PORT={{ port \"http\" }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
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
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml.herd"), []byte("version: '3'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
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
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
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
	if err := os.WriteFile(filepath.Join(dir, "info.txt.herd"), []byte(tmpl), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
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
	if err := os.WriteFile(filepath.Join(dir, "ports.herd"), []byte(`{{ port "http" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
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
	got := string(data)
	if len(got) < 5 {
		t.Errorf("port output too short: %q", got)
	}
}

func TestProcess_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{}
	svc := New(mock)

	attrs := map[string]string{semconv.HookAttrProject: "myapp"}
	_, err := svc.Process(ProcessContext{
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
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{failOn: semconv.HookPreTemplate}
	svc := New(mock)

	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error when pre-template hook fails")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "test")); statErr == nil {
		t.Error("template should not be processed when pre-template hook fails")
	}
}

func TestProcess_BadTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.herd"), []byte("{{ .Invalid | bad }}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error for bad template")
	}
}

// TestProcess_ExecuteError exercises the template execution error path.
// {{ call .Project }} parses fine but fails at execution because Project is a
// string, not a function.
func TestProcess_ExecuteError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad_exec.herd"), []byte(`{{ call .Project }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error for template execution failure")
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

func TestProcess_EnvFunction_NoArgs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "" {
		t.Errorf("expected empty output for env with no args, got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_WithDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_UNSET_VAR_XYZ" "fallback" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "fallback" {
		t.Errorf("expected 'fallback', got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_VarSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_VAR" "default" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("CODEHERD_TEST_VAR", "actual_value")

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "actual_value" {
		t.Errorf("expected 'actual_value', got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_VarNotSet_NoDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_UNSET_VAR_XYZ" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "" {
		t.Errorf("expected empty output, got %q", result.Files[0].Output)
	}
}

func TestProcess_PostHookFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{failOn: semconv.HookPostTemplate}
	svc := New(mock)
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Fatal("expected error when post-template hook fails")
	}
}

func TestProcess_InvalidDirectory(t *testing.T) {
	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: "/nonexistent/path/that/does/not/exist",
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

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

	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Error("expected .env NOT to be written in dry-run mode")
	}

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
