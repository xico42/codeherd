package tui

import (
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
)

func TestRemotePicker_setAndSelect(t *testing.T) {
	p := newRemotePicker("myapp")
	p.setBranches([]herd.RemoteBranch{
		{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
		{Remote: "origin", Branch: "fix-y", Ref: "origin/fix-y"},
	})
	rb, ok := p.selected()
	if !ok || rb.Ref != "origin/feat-x" {
		t.Errorf("selected = %+v ok=%v, want origin/feat-x", rb, ok)
	}
}

func TestRemotePicker_emptySelectNone(t *testing.T) {
	p := newRemotePicker("myapp")
	p.setBranches(nil)
	if _, ok := p.selected(); ok {
		t.Error("expected no selection on empty picker")
	}
}

func TestRemoteBranchesMsg_populatesPicker(t *testing.T) {
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp")}
	updated, _ := m.Update(remoteBranchesMsg{
		project:  "myapp",
		branches: []herd.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}},
	})
	mm := updated.(Model)
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

func TestRemoteBranchesMsg_errorShownInPicker(t *testing.T) {
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp")}
	updated, _ := m.Update(remoteBranchesMsg{project: "myapp", err: herd.ErrNotCloned})
	if got := updated.(Model).remotePicker.errText; got == "" {
		t.Error("errText is empty, want the humanized fetch error")
	}
}

func pickerScreenModel(t *testing.T, branches []herd.RemoteBranch) Model {
	t.Helper()
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp"), cfg: &config.Config{}}
	m.list = newList(nil)
	m.remotePicker.setBranches(branches)
	return m
}

func TestUpdateRemotePicker_escClosesPicker(t *testing.T) {
	m := pickerScreenModel(t, nil)

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	um := updated.(Model)
	if um.screen != screenList || um.remotePicker != nil {
		t.Errorf("screen = %d, picker = %v; want the list and no picker", um.screen, um.remotePicker)
	}
}

func TestUpdateRemotePicker_enterOpensTrackForm(t *testing.T) {
	m := pickerScreenModel(t, []herd.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}})

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	um := updated.(Model)
	if um.screen != screenForm || um.form == nil {
		t.Fatalf("screen = %d, form = %v; want the track form", um.screen, um.form)
	}
	if um.remotePicker != nil {
		t.Error("picker should be closed once a branch is chosen")
	}
}

func TestUpdateRemotePicker_enterWithoutBranchesStays(t *testing.T) {
	m := pickerScreenModel(t, nil)

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if um := updated.(Model); um.screen != screenRemotePicker {
		t.Errorf("screen = %d, want the picker to stay open", um.screen)
	}
}

func TestUpdateRemotePicker_otherKeysGoToTheList(t *testing.T) {
	m := pickerScreenModel(t, []herd.RemoteBranch{
		{Remote: "origin", Branch: "a", Ref: "origin/a"},
		{Remote: "origin", Branch: "b", Ref: "origin/b"},
	})

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if rb, _ := updated.(Model).remotePicker.selected(); rb.Branch != "b" {
		t.Errorf("selected = %q after down, want b", rb.Branch)
	}
}

func TestUpdateRemotePicker_filteringPassesEscToTheList(t *testing.T) {
	m := pickerScreenModel(t, []herd.RemoteBranch{{Remote: "origin", Branch: "a", Ref: "origin/a"}})
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: '/', Text: "/"})) // start filtering
	m = updated.(Model)
	if m.remotePicker.list.FilterState() != list.Filtering {
		t.Fatal("expected the picker list to be filtering after /")
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if um := updated.(Model); um.screen != screenRemotePicker {
		t.Errorf("screen = %d, want esc to cancel the filter, not close the picker", um.screen)
	}
}
