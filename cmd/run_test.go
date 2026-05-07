package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRunConfig(t *testing.T) string {
	t.Helper()
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.toml")
	content := `[agents.my-agent]
cmd = "echo"
args = ["hello"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestRun_tooFewArgs(t *testing.T) {
	cfgPath := writeRunConfig(t)
	if err := runCmd(t, "--config", cfgPath, "run"); err == nil {
		t.Error("expected error for missing agent argument")
	}
}

func TestRun_tooManyArgs(t *testing.T) {
	cfgPath := writeRunConfig(t)
	if err := runCmd(t, "--config", cfgPath, "run", "a", "b"); err == nil {
		t.Error("expected error for too many arguments")
	}
}

func TestRun_unknownAgent(t *testing.T) {
	cfgPath := writeRunConfig(t)
	err := runCmd(t, "--config", cfgPath, "run", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}
