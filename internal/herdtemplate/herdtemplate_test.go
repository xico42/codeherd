package herdtemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

func TestProcess_RendersHerdFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("PORT={{ port \"http\" }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) == 0 {
		t.Error("output file is empty")
	}
}

func TestProcess_StripsHerdSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml.herd"), []byte("version: '3'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Errorf("output file not created: %v", err)
	}
}

func TestProcess_NoHerdFiles_NoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
}

func TestProcess_TemplateContext(t *testing.T) {
	dir := t.TempDir()
	tmpl := "project={{ .Project }}\nbranch={{ .Branch }}\npath={{ .WorktreePath }}\nsession={{ .SessionName }}\n"
	if err := os.WriteFile(filepath.Join(dir, "info.txt.herd"), []byte(tmpl), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "info.txt"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	got := string(data)
	if got != "project=myapp\nbranch=feature\npath="+dir+"\nsession=myapp-feature\n" {
		t.Errorf("content = %q", got)
	}
}

func TestProcess_PortFunction(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ports.herd"), []byte(`{{ port "http" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ports"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	got := string(data)
	if len(got) < 5 {
		t.Errorf("port output too short: %q", got)
	}
}

func TestProcess_TriggersHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{}
	svc := New(mock)

	attrs := map[string]string{semconv.HookAttrProject: "myapp"}
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, attrs)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(mock.calls))
	}
	if mock.calls[0].name != semconv.HookPreTemplate {
		t.Errorf("first hook = %q, want %q", mock.calls[0].name, semconv.HookPreTemplate)
	}
	if mock.calls[1].name != semconv.HookPostTemplate {
		t.Errorf("second hook = %q, want %q", mock.calls[1].name, semconv.HookPostTemplate)
	}
}

func TestProcess_PreHookFailureStops(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{failOn: semconv.HookPreTemplate}
	svc := New(mock)

	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error when pre-template hook fails")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "test")); statErr == nil {
		t.Error("template should not be processed when pre-template hook fails")
	}
}

func TestProcess_BadTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.herd"), []byte("{{ .Invalid | bad }}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error for bad template")
	}
}

// TestProcess_ExecuteError exercises the template execution error path.
// {{ call .Project }} parses fine but fails at execution because Project is a
// string, not a function.
func TestProcess_ExecuteError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad_exec.herd"), []byte(`{{ call .Project }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Error("expected error for template execution failure")
	}
}

type mockHook struct {
	calls  []hookCall
	failOn string
}

type hookCall struct {
	name    string
	attrs   map[string]string
	workDir string
}

func (m *mockHook) Trigger(name string, attrs map[string]string, workDir string) error {
	m.calls = append(m.calls, hookCall{name, attrs, workDir})
	if m.failOn == name {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

func TestDeterministicPortWithSeed_Idempotent(t *testing.T) {
	p1 := DeterministicPortWithSeed("myapp", "feature", "api", "")
	p2 := DeterministicPortWithSeed("myapp", "feature", "api", "")
	if p1 != p2 {
		t.Errorf("not idempotent: %d != %d", p1, p2)
	}
}

func TestDeterministicPortWithSeed_InRange(t *testing.T) {
	p := DeterministicPortWithSeed("myapp", "feature", "api", "")
	if p < 10000 || p > 59999 {
		t.Errorf("port %d out of range 10000-59999", p)
	}
}

func TestDeterministicPortWithSeed_DifferentNames(t *testing.T) {
	p1 := DeterministicPortWithSeed("myapp", "feature", "api", "")
	p2 := DeterministicPortWithSeed("myapp", "feature", "db", "")
	if p1 == p2 {
		t.Errorf("same port %d for different names", p1)
	}
}

func TestDeterministicPortWithSeed_NullByteSeparation(t *testing.T) {
	p1 := DeterministicPortWithSeed("ab", "cd", "x", "")
	p2 := DeterministicPortWithSeed("a", "bcd", "x", "")
	if p1 == p2 {
		t.Errorf("null-byte separation failed: both hashed to %d", p1)
	}
}

func TestDeterministicPortWithSeed_DifferentSeeds(t *testing.T) {
	p1 := DeterministicPortWithSeed("myapp", "feature", "api", "")
	p2 := DeterministicPortWithSeed("myapp", "feature", "api", "alt")
	if p1 == p2 {
		t.Errorf("same port %d for different seeds", p1)
	}
}

func TestProcess_EnvFunction_NoArgs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "" {
		t.Errorf("expected empty output for env with no args, got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_WithDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_UNSET_VAR_XYZ" "fallback" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "fallback" {
		t.Errorf("expected 'fallback', got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_VarSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_VAR" "default" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("CODEHERD_TEST_VAR", "actual_value")

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "actual_value" {
		t.Errorf("expected 'actual_value', got %q", result.Files[0].Output)
	}
}

func TestProcess_EnvFunction_VarNotSet_NoDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte(`{{ env "CODEHERD_TEST_UNSET_VAR_XYZ" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Files[0].Output != "" {
		t.Errorf("expected empty output, got %q", result.Files[0].Output)
	}
}

func TestProcess_PostHookFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{failOn: semconv.HookPostTemplate}
	svc := New(mock)
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Fatal("expected error when post-template hook fails")
	}
}

func TestProcess_InvalidDirectory(t *testing.T) {
	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: "/nonexistent/path/that/does/not/exist",
		SessionName:  "myapp-main",
	}, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestProcess_DryRun_DoesNotWriteFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.herd"), []byte("PORT={{ port \"http\" }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
		DryRun:       true,
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Error("expected .env NOT to be written in dry-run mode")
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 rendered file, got %d", len(result.Files))
	}
	if result.Files[0].Target != filepath.Join(dir, ".env") {
		t.Errorf("target = %q, want %q", result.Files[0].Target, filepath.Join(dir, ".env"))
	}
	if result.Files[0].Output == "" {
		t.Error("expected non-empty rendered output")
	}
}

func TestProcess_DryRun_SkipsHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.herd"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockHook{}
	svc := New(mock)
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
		DryRun:       true,
	}, map[string]string{semconv.HookAttrProject: "myapp"})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(mock.calls) != 0 {
		t.Errorf("expected 0 hook calls in dry-run, got %d", len(mock.calls))
	}
}

func TestProcess_ReturnsProcessResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt.herd"), []byte("hello {{ .Project }}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml.herd"), []byte("version: 3"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	result, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "main",
		WorktreePath: dir,
		SessionName:  "myapp-main",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("expected 2 rendered files, got %d", len(result.Files))
	}
}

func TestProcess_PortFunction_StableAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.env.herd"), []byte(`{{ port "shared" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.env.herd"), []byte(`{{ port "shared" }}`), 0o644); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "myapp",
		Branch:       "feature",
		WorktreePath: dir,
		SessionName:  "myapp-feature",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	a, err := os.ReadFile(filepath.Join(dir, "a.env"))
	if err != nil {
		t.Fatalf("reading a: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "b.env"))
	if err != nil {
		t.Fatalf("reading b: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("port differs across files: a=%q b=%q", string(a), string(b))
	}
}

// TestProcess_PortCollision_ResolvedByAltHash uses a known h1-colliding pair
// under the seeded key format: project="testproj", branch="testbranch", names
// "svc295" and "svc758" both produce h1=58792. Their h2 values differ
// (svc295=13363, svc758=35555), so the allocator resolves the conflict via h2
// without falling back to linear probing.
func TestProcess_PortCollision_ResolvedByAltHash(t *testing.T) {
	dir := t.TempDir()
	tmpl := `svc295={{ port "svc295" }}` + "\n" + `svc758={{ port "svc758" }}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ports.env.herd"), []byte(tmpl), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(&hooks.NoOp{})
	_, err := svc.Process(ProcessContext{
		Project:      "testproj",
		Branch:       "testbranch",
		WorktreePath: dir,
		SessionName:  "test-session",
	}, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ports.env"))
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := "svc295=58792\nsvc758=35555\n"
	if string(data) != want {
		t.Errorf("output = %q, want %q", string(data), want)
	}
}

// TestPortAllocator_FallsBackToProbe pre-occupies both h1 and h2 of a chosen
// name with sentinel entries, then asserts that allocate falls through to the
// linear-probe path and returns a slot distinct from h1 and h2.
func TestPortAllocator_FallsBackToProbe(t *testing.T) {
	project, branch, name := "probetest", "branch", "svc295"
	h1 := DeterministicPortWithSeed(project, branch, name, "")
	h2 := DeterministicPortWithSeed(project, branch, name, "alt")

	alloc := newPortAllocator(project, branch)
	alloc.byPort[h1] = "sentinel-h1"
	alloc.byName["sentinel-h1"] = h1
	if h2 != h1 {
		alloc.byPort[h2] = "sentinel-h2"
		alloc.byName["sentinel-h2"] = h2
	}

	p, err := alloc.allocate(name)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if p == h1 || p == h2 {
		t.Errorf("probed port %d should differ from h1=%d and h2=%d", p, h1, h2)
	}
	if p < portMin || p > portMax {
		t.Errorf("probed port %d out of range [%d, %d]", p, portMin, portMax)
	}
}

// TestPortAllocator_ProbeWraps fills every slot in [portMin, portMax] with
// sentinels except one chosen freeSlot positioned immediately behind h1 (with
// wrap). The probe must walk forward from h1, pass portMax, wrap to portMin,
// and continue until it reaches freeSlot — proving the wrap branch works.
func TestPortAllocator_ProbeWraps(t *testing.T) {
	project, branch, name := "wraptest", "branch", "probetest"
	h1 := DeterministicPortWithSeed(project, branch, name, "")
	h2 := DeterministicPortWithSeed(project, branch, name, "alt")

	freeSlot := h1 - 1
	if freeSlot < portMin {
		freeSlot = portMax
	}
	// If the chosen freeSlot happens to equal h2, the allocator would short-
	// circuit there via the h2 path and never enter the probe loop. Shift one
	// slot further back (with wrap) to keep the probe path under test.
	if h2 == freeSlot {
		freeSlot--
		if freeSlot < portMin {
			freeSlot = portMax
		}
	}

	alloc := newPortAllocator(project, branch)
	for p := portMin; p <= portMax; p++ {
		if p == freeSlot {
			continue
		}
		alloc.byPort[p] = fmt.Sprintf("sentinel-%d", p)
	}

	p, err := alloc.allocate(name)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if p != freeSlot {
		t.Errorf("expected probed port %d (free slot), got %d", freeSlot, p)
	}
}

// TestPortAllocator_Exhausted_Errors fills every slot in [portMin, portMax]
// with sentinel entries and asserts that allocate returns a non-nil error
// mentioning the requested name. Practically unreachable in real renders but
// guards the "we cannot guarantee non-collision" branch the spec requires.
func TestPortAllocator_Exhausted_Errors(t *testing.T) {
	alloc := newPortAllocator("exhausttest", "branch")
	for p := portMin; p <= portMax; p++ {
		alloc.byPort[p] = fmt.Sprintf("sentinel-%d", p)
	}

	_, err := alloc.allocate("anything")
	if err == nil {
		t.Fatal("expected error when allocator is exhausted, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("error %q should mention exhaustion", err.Error())
	}
	if !strings.Contains(err.Error(), `"anything"`) {
		t.Errorf("error %q should mention the requested name", err.Error())
	}
}
