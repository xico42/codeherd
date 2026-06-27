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
	// Cobra's mutual-exclusion error names both flags in the group.
	if err == nil || !strings.Contains(err.Error(), "from") || !strings.Contains(err.Error(), "track") {
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
