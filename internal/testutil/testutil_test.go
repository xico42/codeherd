package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := string(ReadFile(t, dir, "a.txt")); got != "hello" {
		t.Errorf("ReadFile() = %q, want %q", got, "hello")
	}
}

// recorder captures Fatalf instead of stopping the test, so the failure paths
// can be asserted.
type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper() {}

func (r *recorder) Fatalf(string, ...any) {
	r.failed = true
	panic(r)
}

func readExpectingFailure(t *testing.T, dir, name string) {
	t.Helper()
	r := &recorder{TB: t}
	defer func() {
		if p := recover(); p != r {
			t.Fatalf("expected ReadFile to fail the test, recovered %v", p)
		}
	}()
	ReadFile(r, dir, name)
}

func TestReadFile_missingFileFails(t *testing.T) {
	readExpectingFailure(t, t.TempDir(), "missing.txt")
}

func TestReadFile_missingDirFails(t *testing.T) {
	readExpectingFailure(t, filepath.Join(t.TempDir(), "absent"), "a.txt")
}

func TestReadFile_escapeFails(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "inner")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	readExpectingFailure(t, dir, "../secret")
}
