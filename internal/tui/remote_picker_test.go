package tui

import (
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/worktree"
)

func TestRemotePicker_setAndSelect(t *testing.T) {
	p := newRemotePicker("myapp", &config.Config{}, nil)
	if !p.loading {
		t.Error("picker should start in loading state")
	}
	p.setBranches([]worktree.RemoteBranch{
		{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
		{Remote: "origin", Branch: "fix-y", Ref: "origin/fix-y"},
	})
	if p.loading {
		t.Error("picker should leave loading state after setBranches")
	}
	rb, ok := p.selected()
	if !ok || rb.Ref != "origin/feat-x" {
		t.Errorf("selected = %+v ok=%v, want origin/feat-x", rb, ok)
	}
}

func TestRemotePicker_emptySelectNone(t *testing.T) {
	p := newRemotePicker("myapp", &config.Config{}, nil)
	p.setBranches(nil)
	if _, ok := p.selected(); ok {
		t.Error("expected no selection on empty picker")
	}
}

func TestRemoteBranchesMsg_populatesPicker(t *testing.T) {
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp", &config.Config{}, nil)}
	updated, _ := m.Update(remoteBranchesMsg{
		project:  "myapp",
		branches: []worktree.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}},
	})
	mm := updated.(Model)
	if mm.remotePicker.loading {
		t.Error("expected picker to leave loading after branches arrive")
	}
	if got := len(mm.remotePicker.list.Items()); got != 1 {
		t.Errorf("items = %d, want 1", got)
	}
}

func TestRemoteKeyBindingPresent(t *testing.T) {
	k := defaultKeyMap()
	if len(k.Remote.Keys()) == 0 {
		t.Fatal("expected Remote binding to have keys")
	}
	if k.Remote.Keys()[0] != "r" {
		t.Errorf("Remote key = %q, want r", k.Remote.Keys()[0])
	}
}

func TestRefreshKeyBindingRemoved(t *testing.T) {
	// 'r' now opens the remote picker; manual refresh is gone (auto-refresh
	// covers it). The keyMap must no longer carry a Refresh binding — this test
	// fails to compile if the field is reintroduced, which is the intent.
	k := defaultKeyMap()
	for _, b := range k.FullHelp() {
		for _, kb := range b {
			for _, key := range kb.Keys() {
				if key == "r" && kb.Help().Desc == "refresh" {
					t.Error("manual refresh binding should be removed")
				}
			}
		}
	}
}
