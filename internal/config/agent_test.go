package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
)

func TestAgentByName_found(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `
[agents.claude]
cmd = "claude"
args = ["--dangerously-skip-permissions"]

[agents.claude.env]
CLAUDE_CONFIG_DIR = "/custom"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := cfg.AgentByName("claude")
	if err != nil {
		t.Fatalf("AgentByName(claude) error: %v", err)
	}
	if agent.Cmd != "claude" {
		t.Errorf("Cmd = %q, want claude", agent.Cmd)
	}
	if len(agent.Args) != 1 || agent.Args[0] != "--dangerously-skip-permissions" {
		t.Errorf("Args = %v, want [--dangerously-skip-permissions]", agent.Args)
	}
	if agent.Env["CLAUDE_CONFIG_DIR"] != "/custom" {
		t.Errorf("Env[CLAUDE_CONFIG_DIR] = %q, want /custom", agent.Env["CLAUDE_CONFIG_DIR"])
	}
}

func TestAgentByName_notFound(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `
[agents.claude]
cmd = "claude"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.AgentByName("nonexistent")
	if err == nil {
		t.Error("AgentByName(nonexistent) should return error")
	}
}

func TestAgentByName_noAgentsDefined(t *testing.T) {
	cfg, _, err := config.Load(filepath.Join(t.TempDir(), "config.toml"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.AgentByName("claude")
	if err == nil {
		t.Error("AgentByName on empty config should return error")
	}
}

func TestAgentNames_sorted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `
[agents.zed]
cmd = "zed"

[agents.aider]
cmd = "aider"

[agents.claude]
cmd = "claude"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	names := cfg.AgentNames()
	if len(names) != 3 {
		t.Fatalf("AgentNames() = %v, want 3 entries", names)
	}
	if names[0] != "aider" || names[1] != "claude" || names[2] != "zed" {
		t.Errorf("AgentNames() = %v, want [aider claude zed]", names)
	}
}

func TestAgentConfig_Command(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.AgentConfig
		want string
	}{
		{"cmd only", config.AgentConfig{Cmd: "claude"}, "claude"},
		{"cmd with args", config.AgentConfig{Cmd: "claude", Args: []string{"--model", "opus"}}, "claude --model opus"},
		{"empty", config.AgentConfig{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.Command()
			if got != tc.want {
				t.Errorf("Command() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAgentNames_empty(t *testing.T) {
	cfg, _, err := config.Load(filepath.Join(t.TempDir(), "config.toml"), "")
	if err != nil {
		t.Fatal(err)
	}
	names := cfg.AgentNames()
	if len(names) != 0 {
		t.Errorf("AgentNames() = %v, want empty", names)
	}
}
