package cmd_test

import (
	"os"
	"testing"

	"github.com/xico42/codeherd/cmd"
)

// runCmd sets os.Args to simulate a CLI invocation and calls Execute.
// It restores os.Args after the call.
func runCmd(t *testing.T, args ...string) error {
	t.Helper()
	orig := os.Args
	os.Args = append([]string{"ch"}, args...)
	defer func() { os.Args = orig }()
	return cmd.Execute("test")
}

// TestExecute_Help exercises Execute() and all init() registrations by
// running --help, which Cobra handles internally and returns nil.
func TestExecute_Help(t *testing.T) {
	if err := runCmd(t, "--help"); err != nil {
		t.Errorf("Execute(--help) = %v, want nil", err)
	}
}

// TestExecute_Subcommands exercises the RunE closures of stub subcommands
// and the PersistentPreRunE that loads config.
// Each subcommand prints "not implemented" and returns nil.
func TestExecute_Subcommands(t *testing.T) {
	// Use a non-existent config path so Load() returns empty defaults (nil error).
	dir := t.TempDir()
	cfgPath := dir + "/config.toml"

	subcommands := [][]string{
		{"list", "project"},
		{"list", "worktree"},
		{"list", "session"},
	}
	for _, sub := range subcommands {
		t.Run(sub[0], func(t *testing.T) {
			args := append([]string{"--config", cfgPath}, sub...)
			err := runCmd(t, args...)
			if err != nil {
				t.Errorf("Execute(%q) = %v, want nil", sub, err)
			}
		})
	}
}

// TestExecute_ConfigLoadError exercises PersistentPreRunE when the config file
// cannot be read (by pointing --config at a directory, causing ReadFile to fail).
func TestExecute_ConfigLoadError(t *testing.T) {
	dir := t.TempDir()
	// A directory path will cause os.ReadFile to fail with a non-ErrNotExist error.
	err := runCmd(t, "--config", dir, "list", "session")
	if err == nil {
		t.Error("Execute() with unreadable config = nil, want error")
	}
}

// TestExecute_UnknownCommand exercises the Execute() error branch by passing
// a command name that Cobra does not recognise.
func TestExecute_UnknownCommand(t *testing.T) {
	err := runCmd(t, "totally-unknown-subcommand")
	if err == nil {
		t.Error("Execute() with unknown command = nil, want error")
	}
}

// TestExecute_Version exercises the version subcommand.
func TestExecute_Version(t *testing.T) {
	if err := runCmd(t, "version"); err != nil {
		t.Errorf("Execute(version) = %v, want nil", err)
	}
}
