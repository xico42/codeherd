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
