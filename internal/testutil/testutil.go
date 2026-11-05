// Package testutil holds helpers shared by codeherd's tests.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// ReadFile reads name from dir and fails the test if it cannot. The read goes
// through an os.Root, so name cannot escape dir.
func ReadFile(t testing.TB, dir, name string) []byte {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("opening %s: %v", dir, err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("closing %s: %v", dir, err)
		}
	}()
	data, err := root.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Join(dir, name), err)
	}
	return data
}
