package tui

import "testing"

func TestKeyMap_ShortHelp(t *testing.T) {
	km := defaultKeyMap()
	bindings := km.ShortHelp()
	if len(bindings) == 0 {
		t.Fatal("ShortHelp returned no bindings")
	}
	// Verify key actions are present.
	keys := make(map[string]bool)
	for _, b := range bindings {
		keys[b.Help().Key] = true
	}
	for _, want := range []string{"a/enter", "s", "n", "d", "q"} {
		if !keys[want] {
			t.Errorf("ShortHelp missing key %q", want)
		}
	}
}

func TestKeys_profileCycleBindings(t *testing.T) {
	k := defaultKeyMap()
	if len(k.NextProfile.Keys()) == 0 {
		t.Error("NextProfile binding has no keys")
	}
	if len(k.PrevProfile.Keys()) == 0 {
		t.Error("PrevProfile binding has no keys")
	}
	hasCtrlP := false
	for _, key := range k.NextProfile.Keys() {
		if key == "ctrl+p" {
			hasCtrlP = true
		}
	}
	if !hasCtrlP {
		t.Errorf("NextProfile keys = %v, want ctrl+p", k.NextProfile.Keys())
	}
}
