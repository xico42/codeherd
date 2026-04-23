# Profiles Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional, fully-scoped "profiles" to codeherd so one install can juggle multiple independent contexts (personal, work, client A/B) without merging their data; cycle profiles in the TUI with `Ctrl+P`/`Ctrl+N`.

**Architecture:** Opt-in via `[defaults].profiles_enabled` in the main config. Each profile lives in its own flat TOML file under `<main-config-dir>/profiles/<name>.toml` (or a custom `profiles_dir`). `config.Load` returns both the active profile's `*Config` and a `*ProfileRegistry`. Session name carries the profile as a prefix (`<profile>-<project>-<branch>`) for tmux-level uniqueness; profile identity travels via a dedicated tmux user option `@codeherd_profile` (no name parsing). `internal/session` method signatures are preserved — profile awareness enters via additive struct fields and two new `*ByName` methods. The TUI caches per-profile services by name for the TUI's lifetime.

**Tech Stack:** Go, Cobra, Bubble Tea v2, pelletier/go-toml, tmux, git.

**Spec:** `docs/plans/2026-04-23-profiles-design.md`.

---

## File Structure

**Create:**
- `internal/config/profile.go` — `ProfileRegistry` type, `LoadProfile`, warning sink var.
- `internal/config/profile_test.go` — profile-mode loader tests.
- `cmd/profiles_integration_test.go` — in-process integration test (`//go:build integration`).

**Modify:**
- `internal/config/config.go` — `DefaultsConfig` gains `ProfilesEnabled`, `ProfilesDir`, `MainProfile`; `Load` signature `(path, profileName)`; resolution + stray-keys warning.
- `internal/config/config_test.go` — update existing tests for the new signature.
- `internal/semconv/semconv.go` — `SessionName(profile, project, branch)`; add `TmuxOptionProfile` constant.
- `internal/semconv/semconv_test.go` — add profile cases; update existing.
- `internal/tmux/client.go` — `SessionRecord.Profile`; `ListSessions` reads `@codeherd_profile`.
- `internal/tmux/client_test.go` — update `ListSessions` test for new column.
- `internal/session/session.go` — `StartRequest.Profile`; `SessionInfo.Profile`; `Start` sets `@codeherd_profile`; `List` populates `Profile`; new `StopByName`/`ShowByName`.
- `internal/session/session_test.go` — new cases for `Profile` plumbing.
- `cmd/root.go` — persistent `-p, --profile` flag; capture `*ProfileRegistry`.
- `cmd/services.go` — helpers `showSessionForProfile`, `stopSessionForProfile`, `listSessionsForProfile`.
- `cmd/session.go` — dispatch via helpers.
- `cmd/tui.go` — pass `registry` into `tui.NewModel`.
- `internal/tui/keys.go` — `NextProfile`, `PrevProfile` bindings.
- `internal/tui/model.go` — `registry`, `profileCache`; switch flow; title; session filtering.
- `internal/tui/model_test.go` — profile switch flow + filtering tests.
- `.gitignore` — add `/tmp/`.

---

## Chunk 1: Config foundation

### Task 1.1: Extend `DefaultsConfig` with profile-meta fields

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1.1.1: Write the failing test**

Add to `internal/config/config_test.go` (keep existing tests):

```go
func TestLoad_parsesProfileMetaFields(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "config.toml")
    content := `[defaults]
profiles_enabled = true
profiles_dir = "/custom/profiles"
main_profile = "personal"
`
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
        t.Fatal(err)
    }
    cfg, _, err := config.Load(path, "")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    if !cfg.Defaults.ProfilesEnabled {
        t.Error("ProfilesEnabled = false, want true")
    }
    if cfg.Defaults.ProfilesDir != "/custom/profiles" {
        t.Errorf("ProfilesDir = %q, want /custom/profiles", cfg.Defaults.ProfilesDir)
    }
    if cfg.Defaults.MainProfile != "personal" {
        t.Errorf("MainProfile = %q, want personal", cfg.Defaults.MainProfile)
    }
}
```

Also update every existing `Load` call site to pass `""` as the second argument. Run:

```
grep -rn "config.Load(" --include="*.go"
```

Expect hits in **`internal/config/config_test.go` (11 calls)** and **`internal/config/agent_test.go` (5 calls)** — all need the new empty second argument.

- [ ] **Step 1.1.2: Run the tests to verify the new one fails**

Run: `go test ./internal/config/... -run TestLoad_parsesProfileMetaFields -v`

Expected: FAIL — field does not exist (compile error) OR signature mismatch. Both indicate we need to implement.

- [ ] **Step 1.1.3: Add fields to `DefaultsConfig` and update `Load` signature**

In `internal/config/config.go`:

```go
type DefaultsConfig struct {
    ProjectsDir     string `toml:"projects_dir"`
    Agent           string `toml:"agent"`
    ProfilesEnabled bool   `toml:"profiles_enabled"`
    ProfilesDir     string `toml:"profiles_dir"`
    MainProfile     string `toml:"main_profile"`
}
```

Change `Load`'s signature to `func Load(path, profileName string) (*Config, *ProfileRegistry, error)` and for now return `nil` as the registry (we'll fill it in Task 1.3):

```go
func Load(path, profileName string) (*Config, *ProfileRegistry, error) {
    // ... existing body ...
    return cfg, nil, nil
}
```

`ProfileRegistry` is a forward reference — add the type stub in the same file or create `profile.go` now with just the struct shell:

```go
type ProfileRegistry struct {
    Active      string
    Names       []string
    ProfilesDir string
}
```

- [ ] **Step 1.1.4: Run all config tests**

Run: `go test ./internal/config/... -v`

Expected: PASS. Existing tests that call `Load(path)` need updating to `Load(path, "")` — do this exhaustively (the test file is the only place).

- [ ] **Step 1.1.5: Run all unit tests to catch other callers**

Run: `go test ./...`

Expected: compile failures in `cmd/root.go` calling `config.Load(cfgFile)`. Fix by passing `""`:

```go
cfg, _, err = config.Load(cfgFile, "")
```

The second return value is ignored for now (`_`). Re-run `go test ./...` → all tests pass.

- [ ] **Step 1.1.6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/agent_test.go cmd/root.go
git commit -m "feat(config): add profile-meta fields and profile-aware Load signature

Adds ProfilesEnabled / ProfilesDir / MainProfile to DefaultsConfig and
bumps Load to return a *ProfileRegistry (currently nil). Callers pass
the new empty profile name; behavior is otherwise unchanged."
```

---

### Task 1.2: Implement `ProfileRegistry` discovery and `LoadProfile`

**Files:**
- Create: `internal/config/profile.go`
- Test: Create `internal/config/profile_test.go`

- [ ] **Step 1.2.1: Write the failing tests**

Create `internal/config/profile_test.go`:

```go
package config_test

import (
    "os"
    "path/filepath"
    "reflect"
    "testing"

    "github.com/xico42/codeherd/internal/config"
)

func TestLoadProfile_roundTrip(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "personal.toml")
    content := `[defaults]
projects_dir = "~/personal"
agent = "claude"

[projects.myapp]
repo = "git@github.com:u/myapp.git"

[agents.claude]
cmd = "claude"
`
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
        t.Fatal(err)
    }
    cfg, err := config.LoadProfile(dir, "personal")
    if err != nil {
        t.Fatalf("LoadProfile() error = %v", err)
    }
    if cfg.Defaults.Agent != "claude" {
        t.Errorf("Agent = %q, want claude", cfg.Defaults.Agent)
    }
    if _, ok := cfg.Projects["myapp"]; !ok {
        t.Error("missing project myapp")
    }
    if _, ok := cfg.Agents["claude"]; !ok {
        t.Error("missing agent claude")
    }
}

func TestLoadProfile_missingFile(t *testing.T) {
    dir := t.TempDir()
    _, err := config.LoadProfile(dir, "ghost")
    if err == nil {
        t.Fatal("LoadProfile() error = nil, want non-nil for missing profile")
    }
}

func TestLoadProfile_ignoresProfileMetaInsideProfileFile(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "p.toml")
    content := `[defaults]
profiles_enabled = true
profiles_dir = "/nope"
main_profile = "other"
projects_dir = "~/ok"
`
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
        t.Fatal(err)
    }
    cfg, err := config.LoadProfile(dir, "p")
    if err != nil {
        t.Fatalf("LoadProfile() error = %v", err)
    }
    // Struct does hold the fields (they parse), but LoadProfile's contract
    // is that the returned *Config is used only as a plain project/agent
    // bundle — the caller must not consult the profile-meta fields. The
    // test asserts the non-meta field is parsed correctly.
    if cfg.Defaults.ProjectsDir == "" {
        t.Error("ProjectsDir not parsed")
    }
}

func TestDiscoverProfiles_sortsNames(t *testing.T) {
    dir := t.TempDir()
    for _, n := range []string{"work.toml", "personal.toml", "client-a.toml", "not-a-profile.txt"} {
        if err := os.WriteFile(filepath.Join(dir, n), []byte(""), 0o600); err != nil {
            t.Fatal(err)
        }
    }
    names, err := config.DiscoverProfiles(dir)
    if err != nil {
        t.Fatalf("DiscoverProfiles() error = %v", err)
    }
    want := []string{"client-a", "personal", "work"}
    if !reflect.DeepEqual(names, want) {
        t.Errorf("DiscoverProfiles = %v, want %v", names, want)
    }
}

func TestDiscoverProfiles_missingDirIsError(t *testing.T) {
    _, err := config.DiscoverProfiles("/nope/does/not/exist")
    if err == nil {
        t.Fatal("DiscoverProfiles() error = nil, want non-nil for missing dir")
    }
}
```

- [ ] **Step 1.2.2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run "TestLoadProfile|TestDiscoverProfiles" -v`

Expected: FAIL — functions don't exist.

- [ ] **Step 1.2.3: Implement `LoadProfile` and `DiscoverProfiles`**

Create `internal/config/profile.go`:

```go
package config

import (
    "errors"
    "fmt"
    "io"
    "io/fs"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/pelletier/go-toml"
)

// ProfileRegistry summarizes the set of discovered profiles and which
// one is active. Returned by Load alongside *Config when profile mode
// is on. Commands ignore it; the TUI uses it for cycling.
type ProfileRegistry struct {
    Active      string
    Names       []string
    ProfilesDir string
}

// warningSink is the destination for one-time Load warnings. Tests
// swap it to capture output.
var warningSink io.Writer = os.Stderr

// LoadProfile parses <profilesDir>/<name>.toml and returns its Config.
// Errors when the file is missing or malformed.
func LoadProfile(profilesDir, name string) (*Config, error) {
    path := filepath.Join(profilesDir, name+".toml")
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return nil, fmt.Errorf("profile %q not found at %s", name, path)
        }
        return nil, fmt.Errorf("reading profile %s: %w", path, err)
    }
    cfg := &Config{path: path}
    if err := toml.Unmarshal(data, cfg); err != nil {
        return nil, fmt.Errorf("parsing profile %s: %w", path, err)
    }
    if err := cfg.expandPaths(); err != nil {
        return nil, err
    }
    return cfg, nil
}

// DiscoverProfiles lists profile names (filenames minus ".toml") in
// profilesDir. Non-TOML files are ignored. Result is sorted.
func DiscoverProfiles(profilesDir string) ([]string, error) {
    entries, err := os.ReadDir(profilesDir)
    if err != nil {
        return nil, fmt.Errorf("reading profiles dir %s: %w", profilesDir, err)
    }
    var names []string
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        if !strings.HasSuffix(name, ".toml") {
            continue
        }
        names = append(names, strings.TrimSuffix(name, ".toml"))
    }
    sort.Strings(names)
    return names, nil
}
```

Remove the stub `ProfileRegistry` in `config.go` if you added it there in Task 1.1 (it now lives in `profile.go`).

- [ ] **Step 1.2.4: Run the tests**

Run: `go test ./internal/config/... -v`

Expected: all new tests PASS, existing tests continue to PASS.

- [ ] **Step 1.2.5: Commit**

```bash
git add internal/config/profile.go internal/config/profile_test.go internal/config/config.go
git commit -m "feat(config): add ProfileRegistry, LoadProfile, DiscoverProfiles

LoadProfile parses one profile TOML file at <dir>/<name>.toml.
DiscoverProfiles enumerates *.toml filenames as profile names, sorted.
ProfileRegistry is a plain value holding Active / Names / ProfilesDir."
```

---

### Task 1.3: Implement profile resolution inside `Load`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/profile_test.go`

- [ ] **Step 1.3.1: Write failing tests for `Load` in profile mode**

Append to `internal/config/profile_test.go`:

```go
func writeMainConfig(t *testing.T, dir string, body string) string {
    t.Helper()
    path := filepath.Join(dir, "config.toml")
    if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
        t.Fatal(err)
    }
    return path
}

func writeProfile(t *testing.T, profilesDir, name, body string) {
    t.Helper()
    if err := os.MkdirAll(profilesDir, 0o700); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(profilesDir, name+".toml"), []byte(body), 0o600); err != nil {
        t.Fatal(err)
    }
}

func TestLoad_profilesDisabled_registryNil(t *testing.T) {
    dir := t.TempDir()
    path := writeMainConfig(t, dir, "[defaults]\nprojects_dir = \"~/p\"\n")
    _, reg, err := config.Load(path, "")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    if reg != nil {
        t.Errorf("registry = %+v, want nil when profiles disabled", reg)
    }
}

func TestLoad_profilesEnabled_flagBeatsMainProfile(t *testing.T) {
    dir := t.TempDir()
    profilesDir := filepath.Join(dir, "profiles")
    writeProfile(t, profilesDir, "personal", `[projects.pa]
repo = "git@x:a"
`)
    writeProfile(t, profilesDir, "work", `[projects.wb]
repo = "git@x:b"
`)
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
    cfg, reg, err := config.Load(main, "work")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    if _, ok := cfg.Projects["wb"]; !ok {
        t.Error("expected work profile to be active (wb project missing)")
    }
    if reg == nil || reg.Active != "work" {
        t.Errorf("registry.Active = %v, want work", reg)
    }
    if len(reg.Names) != 2 {
        t.Errorf("registry.Names = %v, want 2 entries", reg.Names)
    }
}

func TestLoad_profilesEnabled_fallsBackToMainProfile(t *testing.T) {
    dir := t.TempDir()
    profilesDir := filepath.Join(dir, "profiles")
    writeProfile(t, profilesDir, "personal", `[projects.pa]
repo = "git@x:a"
`)
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
    cfg, reg, err := config.Load(main, "")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    if _, ok := cfg.Projects["pa"]; !ok {
        t.Error("expected personal profile to be active (pa project missing)")
    }
    if reg.Active != "personal" {
        t.Errorf("registry.Active = %q, want personal", reg.Active)
    }
}

func TestLoad_profilesEnabled_errorsWhenUnresolved(t *testing.T) {
    dir := t.TempDir()
    writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
    main := writeMainConfig(t, dir, "[defaults]\nprofiles_enabled = true\n")
    _, _, err := config.Load(main, "")
    if err == nil {
        t.Fatal("Load() error = nil, want unresolved-profile error")
    }
}

func TestLoad_profilesEnabled_errorsWhenProfileFileMissing(t *testing.T) {
    dir := t.TempDir()
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "ghost"
`)
    _, _, err := config.Load(main, "")
    if err == nil {
        t.Fatal("Load() error = nil, want missing-profile error")
    }
}

func TestLoad_profilesEnabled_defaultProfilesDir(t *testing.T) {
    dir := t.TempDir()
    writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
    _, reg, err := config.Load(main, "")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    want := filepath.Join(dir, "profiles")
    if reg.ProfilesDir != want {
        t.Errorf("ProfilesDir = %q, want %q", reg.ProfilesDir, want)
    }
}

func TestLoad_profilesEnabled_errorsWhenProfilesDirMissing(t *testing.T) {
    dir := t.TempDir()
    // no profiles/ subdir created
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
`)
    _, _, err := config.Load(main, "")
    if err == nil {
        t.Fatal("Load() error = nil, want missing-profiles-dir error")
    }
}

func TestLoad_profilesEnabled_warnsOnStrayKeys(t *testing.T) {
    dir := t.TempDir()
    writeProfile(t, filepath.Join(dir, "profiles"), "personal", "")
    main := writeMainConfig(t, dir, `[defaults]
profiles_enabled = true
main_profile = "personal"
projects_dir = "~/ignored"

[projects.ignored]
repo = "git@x:i"
`)
    var buf strings.Builder
    config.SetWarningSink(&buf)
    defer config.SetWarningSink(nil) // reset to default
    _, _, err := config.Load(main, "")
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    out := buf.String()
    if !strings.Contains(out, "profiles_enabled=true") {
        t.Errorf("warning missing profiles_enabled mention: %q", out)
    }
    if !strings.Contains(out, "projects_dir") || !strings.Contains(out, "projects") {
        t.Errorf("warning missing stray key mention: %q", out)
    }
}
```

Add the import `"strings"` to the test file if not already present.

- [ ] **Step 1.3.2: Run tests to verify they fail**

Run: `go test ./internal/config/... -v`

Expected: FAIL — various.

- [ ] **Step 1.3.3: Implement profile resolution and warning in `Load`**

Replace the body of `Load` in `internal/config/config.go` with:

```go
func Load(path, profileName string) (*Config, *ProfileRegistry, error) {
    if path == "" {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, nil, fmt.Errorf("getting home dir: %w", err)
        }
        path = filepath.Join(home, ".config", "codeherd", "config.toml")
    }

    cfg := &Config{path: path}
    data, err := os.ReadFile(path)
    if errors.Is(err, fs.ErrNotExist) {
        if err := cfg.expandPaths(); err != nil {
            return nil, nil, err
        }
        return cfg, nil, nil
    }
    if err != nil {
        return nil, nil, fmt.Errorf("reading config: %w", err)
    }
    if err := toml.Unmarshal(data, cfg); err != nil {
        return nil, nil, fmt.Errorf("parsing config %s: %w", path, err)
    }

    if !cfg.Defaults.ProfilesEnabled {
        if err := cfg.expandPaths(); err != nil {
            return nil, nil, err
        }
        return cfg, nil, nil
    }

    // In profile mode the main config's projects/agents/projects_dir fields
    // are ignored (a warning is emitted in loadProfileMode), so we deliberately
    // skip cfg.expandPaths() here — the returned *Config is the profile's,
    // not this main one.
    return loadProfileMode(cfg, path, profileName)
}
```

Add `loadProfileMode` in `internal/config/profile.go`:

```go
// loadProfileMode resolves the active profile, parses it, warns about
// stray keys in the main config, and returns a clean *Config scoped to
// that profile plus a populated *ProfileRegistry.
func loadProfileMode(main *Config, mainPath, profileName string) (*Config, *ProfileRegistry, error) {
    // Resolve profiles_dir.
    profilesDir := main.Defaults.ProfilesDir
    if profilesDir == "" {
        profilesDir = filepath.Join(filepath.Dir(mainPath), "profiles")
    } else {
        expanded, err := expandTilde(profilesDir)
        if err != nil {
            return nil, nil, err
        }
        profilesDir = expanded
    }
    if st, err := os.Stat(profilesDir); err != nil || !st.IsDir() {
        return nil, nil, fmt.Errorf("profiles_enabled=true but profiles_dir %q does not exist", profilesDir)
    }

    // Resolve active profile.
    active := profileName
    if active == "" {
        active = main.Defaults.MainProfile
    }
    if active == "" {
        return nil, nil, fmt.Errorf("profiles_enabled=true but no profile specified: set defaults.main_profile in %s or pass -p/--profile", mainPath)
    }

    // Warn about stray keys. Emit once per Load call.
    warnStrayKeys(main, mainPath)

    // Load the profile file.
    profCfg, err := LoadProfile(profilesDir, active)
    if err != nil {
        return nil, nil, err
    }
    // Profile-meta fields inside a profile file are ignored silently —
    // zero them out so callers can never accidentally consult them.
    profCfg.Defaults.ProfilesEnabled = false
    profCfg.Defaults.ProfilesDir = ""
    profCfg.Defaults.MainProfile = ""

    names, err := DiscoverProfiles(profilesDir)
    if err != nil {
        return nil, nil, err
    }
    reg := &ProfileRegistry{Active: active, Names: names, ProfilesDir: profilesDir}
    return profCfg, reg, nil
}

func warnStrayKeys(main *Config, mainPath string) {
    var stray []string
    if main.Defaults.ProjectsDir != "" {
        stray = append(stray, "defaults.projects_dir")
    }
    if main.Defaults.Agent != "" {
        stray = append(stray, "defaults.agent")
    }
    if len(main.Projects) > 0 {
        stray = append(stray, "projects")
    }
    if len(main.Agents) > 0 {
        stray = append(stray, "agents")
    }
    if len(stray) == 0 {
        return
    }
    fmt.Fprintf(warningSink, "warning: %s sets profiles_enabled=true; ignoring %s in main config\n", mainPath, strings.Join(stray, ", "))
}

// SetWarningSink replaces the destination used by Load for one-time
// warnings. Pass nil to reset to os.Stderr. Intended for tests.
func SetWarningSink(w io.Writer) {
    if w == nil {
        warningSink = os.Stderr
        return
    }
    warningSink = w
}
```

- [ ] **Step 1.3.4: Run all config tests**

Run: `go test ./internal/config/... -v`

Expected: PASS.

- [ ] **Step 1.3.5: Wire the registry into `cmd/root.go`**

In `cmd/root.go`:

Add near the top-level vars. `registry` is package-level because `cmd/tui.go` reads it in Chunk 3 (Task 3.2) when constructing the TUI model:

```go
var (
    cfgFile      string
    profileFlag  string
    noTmux       bool
    cfg          *config.Config
    registry     *config.ProfileRegistry
)
```

Inside `init()`:

```go
rootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "profile to load (when profiles_enabled=true)")
```

In `PersistentPreRunE`:

```go
cfg, registry, err = config.Load(cfgFile, profileFlag)
```

(The `_` previously discarding the registry becomes `registry`.)

- [ ] **Step 1.3.6: Run all unit tests**

Run: `go test ./...`

Expected: PASS across all packages.

- [ ] **Step 1.3.7: Commit**

```bash
git add internal/config/profile.go internal/config/profile_test.go internal/config/config.go cmd/root.go
git commit -m "feat(config): resolve active profile in Load and add -p/--profile flag

Load now returns a populated *ProfileRegistry when profiles_enabled is
true, with the active profile resolved via flag -> main_profile -> error.
Warns once per Load call about stray projects/agents/defaults.projects_dir
in the main config while in profile mode. The -p/--profile persistent
flag is wired on the root command."
```

---

## Chunk 2: Session naming and tmux profile option

### Task 2.1: Update `semconv.SessionName` and `semconv.ShellSessionName` to take a profile

**Files:**
- Modify: `internal/semconv/semconv.go`
- Test: `internal/semconv/semconv_test.go`
- Update callers (mechanical, no semantic change yet): `internal/session/session.go`, `internal/worktree/worktree.go`, `cmd/session.go`, `cmd/worktree.go`, `cmd/template.go`, `internal/tui/actions.go`, `internal/tui/items.go`, `internal/semconv/semconv_test.go`, `internal/worktree/worktree_test.go`.

**Why both names change:** `ShellSessionName` is defined as `SessionName(project, branch) + "~sh"`. If we only update `SessionName`, either (a) `ShellSessionName`'s body breaks compilation, or (b) we freeze it at `profile == ""`, which would let two profiles' shell sessions for the same `(project, branch)` collide in tmux. Profile prefixing must apply to both.

- [ ] **Step 2.1.1: Write the failing tests**

Add to `internal/semconv/semconv_test.go`:

```go
func TestSessionName_withProfile(t *testing.T) {
    cases := []struct {
        profile string
        project string
        branch  string
        want    string
    }{
        {"", "myapp", "main", "myapp-main"},
        {"", "myapp", "feat/x", "myapp-feat-x"},
        {"personal", "myapp", "main", "personal-myapp-main"},
        {"work", "myapp", "feat/x", "work-myapp-feat-x"},
    }
    for _, tc := range cases {
        got := semconv.SessionName(tc.profile, tc.project, tc.branch)
        if got != tc.want {
            t.Errorf("SessionName(%q, %q, %q) = %q, want %q",
                tc.profile, tc.project, tc.branch, got, tc.want)
        }
    }
}

func TestShellSessionName_withProfile(t *testing.T) {
    cases := []struct {
        profile string
        project string
        branch  string
        want    string
    }{
        {"", "myapp", "main", "myapp-main~sh"},
        {"work", "myapp", "main", "work-myapp-main~sh"},
    }
    for _, tc := range cases {
        got := semconv.ShellSessionName(tc.profile, tc.project, tc.branch)
        if got != tc.want {
            t.Errorf("ShellSessionName(%q, %q, %q) = %q, want %q",
                tc.profile, tc.project, tc.branch, got, tc.want)
        }
    }
}

func TestTmuxOptionProfile_constant(t *testing.T) {
    if semconv.TmuxOptionProfile != "@codeherd_profile" {
        t.Errorf("TmuxOptionProfile = %q, want @codeherd_profile", semconv.TmuxOptionProfile)
    }
}
```

Also update the existing `TestSessionName` case in the same file to use the new three-arg signature (`semconv.SessionName("", project, branch)`).

- [ ] **Step 2.1.2: Run tests to verify they fail**

Run: `go test ./internal/semconv/... -v`

Expected: FAIL — compile error (wrong arity) and missing constant.

- [ ] **Step 2.1.3: Update `SessionName`, `ShellSessionName`, add the constant**

In `internal/semconv/semconv.go`:

```go
const (
    // ... existing constants ...
    TmuxOptionProfile = "@codeherd_profile"
)

// SessionName returns the canonical tmux session name.
// profile == "" gives "<project>-<branch>" (backward-compatible).
// profile != "" gives "<profile>-<project>-<branch>" for tmux uniqueness.
func SessionName(profile, project, branch string) string {
    if profile == "" {
        return project + "-" + FlattenBranch(branch)
    }
    return profile + "-" + project + "-" + FlattenBranch(branch)
}

// ShellSessionName returns the tmux session name for the shell variant,
// carrying the same profile prefix as SessionName.
func ShellSessionName(profile, project, branch string) string {
    return SessionName(profile, project, branch) + "~sh"
}
```

- [ ] **Step 2.1.4: Fix every caller — mechanical, prepend `""`**

Grep (run both):

```
grep -rn "semconv.SessionName(" --include="*.go"
grep -rn "semconv.ShellSessionName(" --include="*.go"
```

Expected call sites (all need `""` as new leading arg; no semantic change yet — profile plumbing lands in Task 2.3):

- `internal/session/session.go:72, 75, 182, 213`
- `internal/worktree/worktree.go:293, 330, 331`
- `internal/worktree/worktree_test.go` — adjust fixtures
- `cmd/session.go:230, 239, 241`
- `cmd/worktree.go:131, 153`
- `cmd/template.go:83`
- `internal/tui/actions.go:442`
- `internal/tui/items.go:76`

- [ ] **Step 2.1.5: Run full test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2.1.6: Commit**

```bash
git add internal/semconv/semconv.go internal/semconv/semconv_test.go \
        internal/session/session.go internal/worktree/worktree.go \
        internal/worktree/worktree_test.go \
        cmd/session.go cmd/worktree.go cmd/template.go \
        internal/tui/actions.go internal/tui/items.go
git commit -m "feat(semconv): SessionName and ShellSessionName take a profile prefix

Both name builders gain a leading profile argument. When empty, output
is unchanged (backward-compatible). When non-empty, names carry the
<profile>- prefix so agent and shell sessions across profiles cannot
collide in tmux. Adds the TmuxOptionProfile constant."
```

---

### Task 2.2: Thread `@codeherd_profile` through `tmux.SessionRecord`

**Files:**
- Modify: `internal/tmux/client.go`
- Test: `internal/tmux/client_test.go`

- [ ] **Step 2.2.1: Write the failing tests**

The existing test helper in `internal/tmux/client_test.go` is `mockRunner` with fields `{stdout, stderr, exitCode, err, lastArgs}`. Use it. Add to the same file:

```go
func TestClient_ListSessions_readsProfileOption(t *testing.T) {
    // 8-field line: profile populated.
    line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-04-23T00:00:00Z\twork\n"
    r := &mockRunner{exitCode: 0, stdout: line}
    c := tmux.NewClient(r)
    records, err := c.ListSessions()
    if err != nil {
        t.Fatalf("ListSessions() error = %v", err)
    }
    if len(records) != 1 {
        t.Fatalf("len(records) = %d, want 1", len(records))
    }
    if records[0].Profile != "work" {
        t.Errorf("Profile = %q, want work", records[0].Profile)
    }
}

func TestClient_ListSessions_missingProfileIsEmpty(t *testing.T) {
    // Old 7-field line: profile must default to "" (backward-compatible
    // with existing runs after tmux reports no @codeherd_profile option).
    line := "$1\tmyapp-feat\tmyapp-feat\tagent\trunning\t\t2026-04-23T00:00:00Z\n"
    r := &mockRunner{exitCode: 0, stdout: line}
    c := tmux.NewClient(r)
    records, err := c.ListSessions()
    if err != nil {
        t.Fatalf("ListSessions() error = %v", err)
    }
    if records[0].Profile != "" {
        t.Errorf("Profile = %q, want empty", records[0].Profile)
    }
}
```

Also update `TestClient_ListSessions_ok` (and any other existing list-sessions test) to append an 8th tab+value to its fixture line, or to expect the 8-field format. The format string changes in Step 2.2.3 — the existing tests will fail on unchanged fixtures until updated.

- [ ] **Step 2.2.2: Run tests to verify they fail**

Run: `go test ./internal/tmux/... -v`

Expected: FAIL — field does not exist.

- [ ] **Step 2.2.3: Add the field and extend ListSessions**

In `internal/tmux/client.go`:

```go
type SessionRecord struct {
    ID            string
    Name          string
    CanonicalName string
    SessionType   string
    Status        string
    Annotation    string
    StartedAt     string
    Profile       string // @codeherd_profile, "" when unset
}
```

Update `ListSessions`:

```go
format := "#{session_id}\t#{session_name}\t#{@codeherd_canonical_name}\t#{@codeherd_session_type}\t#{@codeherd_status}\t#{@codeherd_annotation}\t#{@codeherd_started_at}\t#{@codeherd_profile}"
// ...
fields := strings.SplitN(line, "\t", 8)
for len(fields) < 8 {
    fields = append(fields, "")
}
records = append(records, SessionRecord{
    ID:            fields[0],
    Name:          fields[1],
    CanonicalName: fields[2],
    SessionType:   fields[3],
    Status:        fields[4],
    Annotation:    fields[5],
    StartedAt:     fields[6],
    Profile:       fields[7],
})
```

- [ ] **Step 2.2.4: Run tests**

Run: `go test ./internal/tmux/... -v`

Expected: PASS. Existing tests still pass because the 7-field fixture branch pads with an empty 8th field.

- [ ] **Step 2.2.5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): surface @codeherd_profile on SessionRecord

ListSessions now includes an 8th column (@codeherd_profile) in its
display-message format, populating SessionRecord.Profile. Missing option
yields an empty string."
```

---

### Task 2.3: Extend `session.Service` with profile fields and `*ByName` methods

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step 2.3.1: Write the failing tests**

**Test-helper shape (important).** `internal/session/session_test.go` does **not** use a high-level fake tmux service. It uses `mockRunnerSequence` (defined in the same file) — a queued `Runner` implementation where each call consumes one element from `responses []mockResponse` and appends to `calls [][]string`. Assertions are made against the `calls` slice to verify tmux commands. Profile-option writes must therefore be asserted by inspecting `r2.calls` for a `set-option … @codeherd_profile work` invocation.

Add to `internal/session/session_test.go`:

```go
// findCall reports whether any recorded tmux invocation contains all
// the given substrings in order.
func findCall(calls [][]string, want ...string) bool {
    for _, c := range calls {
        joined := strings.Join(c, " ")
        ok := true
        for _, w := range want {
            if !strings.Contains(joined, w) {
                ok = false
                break
            }
        }
        if ok {
            return true
        }
    }
    return false
}

func TestStart_writesProfileOption_whenSet(t *testing.T) {
    // Same response shape as TestStart_OK, plus one extra set-option
    // for @codeherd_profile. 8 responses now instead of 7.
    r2 := &mockRunnerSequence{responses: []mockResponse{
        {exitCode: 1},                 // list-sessions → no sessions
        {exitCode: 0},                 // new-session
        {exitCode: 0},                 // set-option status
        {exitCode: 0},                 // set-option started_at
        {exitCode: 0},                 // set-option canonical_name
        {exitCode: 0},                 // set-option session_type
        {exitCode: 0},                 // set-option @codeherd_profile (new)
        {exitCode: 0, stdout: "$1\n"}, // display-message → session_id
    }}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    _, err := svc.Start(session.StartRequest{
        Project: "myapp",
        Branch:  "feature",
        Path:    t.TempDir(),
        Cmd:     "claude",
        Profile: "work",
    })
    if err != nil {
        t.Fatalf("Start() error = %v", err)
    }
    // New-session must target the profile-prefixed name.
    if !findCall(r2.calls, "new-session", "-s", "work-myapp-feature") {
        t.Errorf("expected new-session on work-myapp-feature; got %v", r2.calls)
    }
    // @codeherd_profile must be set to "work" on that session.
    if !findCall(r2.calls, "set-option", "work-myapp-feature", semconv.TmuxOptionProfile, "work") {
        t.Errorf("expected set-option @codeherd_profile work; got %v", r2.calls)
    }
}

func TestStart_emptyProfile_noProfileOptionWritten(t *testing.T) {
    // 7 responses — no extra @codeherd_profile set-option.
    r2 := &mockRunnerSequence{responses: []mockResponse{
        {exitCode: 1},
        {exitCode: 0},
        {exitCode: 0},
        {exitCode: 0},
        {exitCode: 0},
        {exitCode: 0},
        {exitCode: 0, stdout: "$1\n"},
    }}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    _, err := svc.Start(session.StartRequest{
        Project: "myapp",
        Branch:  "feature",
        Path:    t.TempDir(),
        Cmd:     "claude",
        // Profile intentionally empty
    })
    if err != nil {
        t.Fatalf("Start() error = %v", err)
    }
    // Name stays unprefixed.
    if !findCall(r2.calls, "new-session", "-s", "myapp-feature") {
        t.Errorf("expected new-session on myapp-feature (no prefix); got %v", r2.calls)
    }
    // @codeherd_profile must NOT appear in any call.
    for _, c := range r2.calls {
        if strings.Contains(strings.Join(c, " "), semconv.TmuxOptionProfile) {
            t.Errorf("unexpected @codeherd_profile call: %v", c)
        }
    }
}

func TestList_populatesProfile(t *testing.T) {
    // list-sessions returns one 8-field line with Profile="work".
    line := "$1\twork-a-main\twork-a-main\tagent\trunning\t\t\twork\n"
    r2 := &mockRunnerSequence{responses: []mockResponse{{exitCode: 0, stdout: line}}}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    list, err := svc.List()
    if err != nil {
        t.Fatalf("List() error = %v", err)
    }
    if len(list) != 1 || list[0].Profile != "work" {
        t.Errorf("List() = %+v, want one SessionInfo with Profile=work", list)
    }
}

func TestShowByName_findsByNameAndType(t *testing.T) {
    line := "$1\twork-a-main\twork-a-main\tagent\trunning\t\t\twork\n"
    r2 := &mockRunnerSequence{responses: []mockResponse{{exitCode: 0, stdout: line}}}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    info, err := svc.ShowByName("work-a-main", semconv.SessionTypeAgent)
    if err != nil {
        t.Fatalf("ShowByName() error = %v", err)
    }
    if info == nil || info.Profile != "work" {
        t.Errorf("ShowByName() = %+v, want Profile=work", info)
    }
}

func TestShowByName_notFound(t *testing.T) {
    r2 := &mockRunnerSequence{responses: []mockResponse{{exitCode: 1}}}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    _, err := svc.ShowByName("nope", semconv.SessionTypeAgent)
    if err == nil || !errors.Is(err, session.ErrSessionNotFound) {
        t.Errorf("ShowByName() error = %v, want ErrSessionNotFound", err)
    }
}

func TestStopByName_killsCorrectSession(t *testing.T) {
    line := "$1\twork-a-main\twork-a-main\tagent\trunning\t\t\twork\n"
    r2 := &mockRunnerSequence{responses: []mockResponse{
        {exitCode: 0, stdout: line}, // list-sessions
        {exitCode: 0},                // kill-session
    }}
    tc := tmux.NewClient(r2)
    svc := session.NewService(tc, &mockHook{})

    if err := svc.StopByName("work-a-main", semconv.SessionTypeAgent); err != nil {
        t.Fatalf("StopByName() error = %v", err)
    }
    if !findCall(r2.calls, "kill-session", "-t", "work-a-main") {
        t.Errorf("expected kill-session on work-a-main; got %v", r2.calls)
    }
}
```

Add the `"strings"` import to the test file if not already present.

**Existing tests to update in the same step:** any `TestStart_*` case that counts `len(r2.calls) == 7` in its assertion. After 2.3.3 lands, `Start` writes `@codeherd_profile` unconditionally when `req.Profile != ""`. Existing tests use empty `Profile` — they must still see 7 calls (not 8). Verify `TestStart_OK` still passes unchanged; if it fails with `len == 8`, the implementation is writing the option unconditionally — the implementation must gate the `SetOption` on `req.Profile != ""`.

- [ ] **Step 2.3.2: Run tests to verify they fail**

Run: `go test ./internal/session/... -v`

Expected: FAIL — fields and methods don't exist.

- [ ] **Step 2.3.3: Extend `StartRequest`, `SessionInfo`, `Start`, `List`**

In `internal/session/session.go`:

```go
type StartRequest struct {
    Project string
    Branch  string
    Path    string
    Type    string
    Cmd     string
    Env     map[string]string
    Attach  bool
    Profile string // "" when profiles are disabled
}
```

Inside `Start`, change **both** name builds (canonical and shell tmuxName) to include the profile:

```go
canonicalName := semconv.SessionName(req.Profile, req.Project, req.Branch)
var tmuxName string
if req.Type == semconv.SessionTypeShell {
    tmuxName = semconv.ShellSessionName(req.Profile, req.Project, req.Branch)
} else {
    tmuxName = canonicalName
}
```

After writing `@codeherd_session_type` (the last existing `SetOption` in `Start`), add a gated `@codeherd_profile` write. **Must be gated on `req.Profile != ""`** so existing tests counting `len(r2.calls) == 7` continue to pass:

```go
if req.Profile != "" {
    _ = s.tmux.SetOption(tmuxName, semconv.TmuxOptionProfile, req.Profile)
}
```

Extend `SessionInfo`:

```go
type SessionInfo struct {
    Name       string
    TmuxName   string
    SessionID  string
    Type       string
    Project    string
    Branch     string
    Status     string
    Annotation string
    StartedAt  time.Time
    UpdatedAt  time.Time
    Profile    string
}
```

Inside `List`, populate `Profile` from `r.Profile`:

```go
info := SessionInfo{
    Name:       r.CanonicalName,
    Type:       r.SessionType,
    Status:     r.Status,
    Annotation: r.Annotation,
    Profile:    r.Profile,
}
```

- [ ] **Step 2.3.4: Add `ShowByName` and `StopByName`**

Append to `internal/session/session.go`:

```go
// ShowByName returns the SessionInfo for the session whose canonical
// name + type match exactly. Returns ErrSessionNotFound otherwise.
func (s *Service) ShowByName(name, sessionType string) (*SessionInfo, error) {
    if sessionType == "" {
        sessionType = semconv.SessionTypeAgent
    }
    records, err := s.tmux.ListSessions()
    if err != nil {
        return nil, fmt.Errorf("listing sessions: %w", err)
    }
    for _, r := range records {
        if r.CanonicalName == name && r.SessionType == sessionType {
            info := &SessionInfo{
                Name:       r.CanonicalName,
                TmuxName:   r.Name,
                SessionID:  r.ID,
                Type:       r.SessionType,
                Status:     r.Status,
                Annotation: r.Annotation,
                Profile:    r.Profile,
            }
            if r.StartedAt != "" {
                info.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
            }
            return info, nil
        }
    }
    return nil, fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, name, sessionType)
}

// StopByName kills the session whose canonical name + type match exactly.
func (s *Service) StopByName(name, sessionType string) error {
    if sessionType == "" {
        sessionType = semconv.SessionTypeAgent
    }
    records, err := s.tmux.ListSessions()
    if err != nil {
        return fmt.Errorf("listing sessions: %w", err)
    }
    actual := ""
    for _, r := range records {
        if r.CanonicalName == name && r.SessionType == sessionType {
            actual = r.Name
            break
        }
    }
    if actual == "" {
        return fmt.Errorf("%w: %s (%s)", ErrSessionNotFound, name, sessionType)
    }
    if err := s.tmux.KillSession(actual); err != nil {
        return fmt.Errorf("killing session: %w", err)
    }
    return nil
}
```

- [ ] **Step 2.3.5: Run tests**

Run: `go test ./internal/session/... -v`

Expected: PASS across new and existing tests (existing tests call `Start` without `Profile` — zero-value, backward-compatible).

- [ ] **Step 2.3.6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): plumb profile through StartRequest/SessionInfo + ByName lookups

Additive changes only: StartRequest and SessionInfo gain a Profile field;
Start sets @codeherd_profile when Profile is non-empty and uses
semconv.SessionName with the profile prefix; List populates
SessionInfo.Profile from tmux records. Two new methods, ShowByName and
StopByName, address sessions by canonical name + type for profile-aware
cmd dispatch. Existing Show/Stop signatures are unchanged."
```

---

## Chunk 3: CLI dispatch and TUI plumbing

### Task 3.1: Add profile-aware helpers in `cmd/services.go`

**Files:**
- Modify: `cmd/services.go`
- Test: `cmd/services_test.go` (create if absent — otherwise inline into `cmd/session_test.go`)

- [ ] **Step 3.1.1: Read existing `cmd/services.go` and `cmd/session.go`**

Understand the helper conventions (newWorktreeService, newSessionService, newProjectService). New helpers will live alongside them.

- [ ] **Step 3.1.2: Add the helpers**

Append to `cmd/services.go`:

```go
// activeProfile returns the currently active profile name, or "" when
// profile mode is off. Safe when registry is nil.
func activeProfile() string {
    if registry == nil {
        return ""
    }
    return registry.Active
}

// showSessionForProfile dispatches Show/ShowByName based on whether a
// profile is active. Callers pass the logical (project, branch, type).
func showSessionForProfile(svc *session.Service, project, branch, sessionType string) (*session.SessionInfo, error) {
    prof := activeProfile()
    if prof == "" {
        return svc.Show(project, branch, sessionType)
    }
    name := semconv.SessionName(prof, project, branch)
    return svc.ShowByName(name, sessionType)
}

// stopSessionForProfile dispatches Stop/StopByName based on the active
// profile.
func stopSessionForProfile(svc *session.Service, project, branch, sessionType string) error {
    prof := activeProfile()
    if prof == "" {
        return svc.Stop(project, branch, sessionType)
    }
    name := semconv.SessionName(prof, project, branch)
    return svc.StopByName(name, sessionType)
}

// listSessionsForProfile returns only sessions matching the active
// profile. With no active profile, all sessions are returned.
func listSessionsForProfile(svc *session.Service) ([]session.SessionInfo, error) {
    all, err := svc.List()
    if err != nil {
        return nil, err
    }
    prof := activeProfile()
    if prof == "" {
        return all, nil
    }
    var out []session.SessionInfo
    for _, s := range all {
        if s.Profile == prof {
            out = append(out, s)
        }
    }
    return out, nil
}
```

Add the `semconv` import at the top of the file if it isn't already present.

- [ ] **Step 3.1.3: Run all tests**

Run: `go test ./...`

Expected: PASS (nothing calls the new helpers yet).

- [ ] **Step 3.1.4: Switch `cmd/session.go` callers to the helpers**

In `cmd/session.go`, replace:

| Current call | Replacement |
|---|---|
| `svc.List()` (in `ListSessionCmd.Run`) | `listSessionsForProfile(svc)` |
| `svc.Show(project, branch, sessionType)` (in `ShowSessionCmd.Run`) | `showSessionForProfile(svc, project, branch, sessionType)` |
| `svc.Show(project, branch, sessionType)` (in `DeleteSessionCmd.Run`) | `showSessionForProfile(svc, project, branch, sessionType)` |
| `svc.Stop(project, branch, sessionType)` (in `DeleteSessionCmd.Run`) | `stopSessionForProfile(svc, project, branch, sessionType)` |
| `svc.Show(project, branch, sessionType)` (in `AttachSessionCmd.Run`) | `showSessionForProfile(svc, project, branch, sessionType)` |

In `CreateSessionCmd.Run`, the `svc.Start(session.StartRequest{...})` call gets a `Profile: activeProfile()` field added to the struct literal.

- [ ] **Step 3.1.5: Run all tests**

Run: `go test ./...`

Expected: PASS (existing tests execute with `registry == nil` ⇒ `activeProfile()` returns `""` ⇒ behavior unchanged).

- [ ] **Step 3.1.6: Commit**

```bash
git add cmd/services.go cmd/session.go
git commit -m "feat(cmd): route session verbs through profile-aware helpers

Adds showSessionForProfile / stopSessionForProfile / listSessionsForProfile
plus activeProfile() in cmd/services.go. Session verb Run methods now
dispatch through these helpers, which transparently fall back to the
existing Show/Stop/List when no profile is active."
```

---

### Task 3.2: Pass `registry` into the TUI

**Files:**
- Modify: `cmd/tui.go`
- Modify: `internal/tui/model.go` (constructor only — switch flow comes in Chunk 4)
- Test: adjust `internal/tui/model_test.go` NewModel callers

- [ ] **Step 3.2.1: Extend `tui.NewModel` to accept `*config.ProfileRegistry`**

In `internal/tui/model.go`, add to the `Model` struct:

```go
registry     *config.ProfileRegistry
profileCache map[string]profileBundle
```

Add the bundle type (unexported):

```go
type profileBundle struct {
    cfg     *config.Config
    wtSvc   *worktree.Service
    projSvc *project.Service
}
```

Replace `NewModel` with the exact body below. The seeding is done on a local `m` before return so the cache is reachable:

```go
func NewModel(
    cfg *config.Config,
    wtSvc *worktree.Service,
    sesSvc *session.Service,
    projSvc *project.Service,
    tmuxClient *tmux.Client,
    insideTmux bool,
    registry *config.ProfileRegistry,
) Model {
    keys := defaultKeyMap()
    l := newList(nil)
    h := help.New()

    m := Model{
        screen:       screenList,
        list:         l,
        keys:         keys,
        help:         h,
        cfg:          cfg,
        wtSvc:        wtSvc,
        sesSvc:       sesSvc,
        projSvc:      projSvc,
        tmuxClient:   tmuxClient,
        InsideTmux:   insideTmux,
        registry:     registry,
        profileCache: map[string]profileBundle{},
    }
    if registry != nil {
        m.profileCache[registry.Active] = profileBundle{cfg: cfg, wtSvc: wtSvc, projSvc: projSvc}
    }
    return m
}
```

- [ ] **Step 3.2.2: Update the single call site**

In `cmd/tui.go`, inside `runTUIDirect`:

```go
m := tui.NewModel(cfg, wtSvc, sesSvc, projSvc, tmuxClient, insideTmux, registry)
```

- [ ] **Step 3.2.3: Update every test that calls `tui.NewModel`**

Grep:

```
grep -rn "tui.NewModel(" --include="*.go"
```

Every call adds a trailing `nil` argument (tests run with profiles off).

- [ ] **Step 3.2.4: Run all tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3.2.5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go cmd/tui.go
git commit -m "feat(tui): accept ProfileRegistry in NewModel

NewModel now receives a *config.ProfileRegistry (nil when profile mode
is off) and initializes an empty profileCache, seeded with the initial
active profile when the registry is present. No behavior change yet —
switching comes in a follow-up commit."
```

---

### Task 3.3: Add `/tmp/` to `.gitignore`

**Files:**
- Modify: `.gitignore`

- [ ] **Step 3.3.1: Append entry**

Add a new line to `.gitignore`:

```
/tmp/
```

(Place it near `ch` and `*.test` — binary/artifact section.)

- [ ] **Step 3.3.2: Verify no existing `tmp/` is tracked**

Run: `git ls-files tmp/ 2>/dev/null`

Expected: empty output.

- [ ] **Step 3.3.3: Commit**

```bash
git add .gitignore
git commit -m "chore: ignore /tmp/ for manual acceptance scratch

Profiles plan (docs/plans/2026-04-23-profiles-design.md) uses ./tmp/
for a one-off manual validation run; ignore it to prevent accidental
commits of scratch config or built repos."
```

---

## Chunk 4: TUI profile switching

### Task 4.1: Add `NextProfile` / `PrevProfile` key bindings

**Files:**
- Modify: `internal/tui/keys.go`
- Test: `internal/tui/keys_test.go`

- [ ] **Step 4.1.1: Write the failing test**

Append to `internal/tui/keys_test.go`:

```go
func TestKeys_profileCycleBindings(t *testing.T) {
    k := defaultKeyMap()
    if len(k.NextProfile.Keys()) == 0 {
        t.Error("NextProfile binding has no keys")
    }
    if len(k.PrevProfile.Keys()) == 0 {
        t.Error("PrevProfile binding has no keys")
    }
    // Sanity: Ctrl+P / Ctrl+N present in the key list.
    hasCtrlP := false
    for _, k := range k.NextProfile.Keys() {
        if k == "ctrl+p" {
            hasCtrlP = true
        }
    }
    if !hasCtrlP {
        t.Errorf("NextProfile keys = %v, want ctrl+p", k.NextProfile.Keys())
    }
}
```

- [ ] **Step 4.1.2: Run the test — expect FAIL**

Run: `go test ./internal/tui/... -run TestKeys_profileCycleBindings -v`

Expected: compile error — field missing.

- [ ] **Step 4.1.3: Add the bindings**

In `internal/tui/keys.go`:

```go
type keyMap struct {
    // ... existing fields ...
    NextProfile key.Binding
    PrevProfile key.Binding
}

func defaultKeyMap() keyMap {
    return keyMap{
        // ... existing fields ...
        NextProfile: key.NewBinding(
            key.WithKeys("ctrl+p"),
            key.WithHelp("ctrl+p", "next profile"),
        ),
        PrevProfile: key.NewBinding(
            key.WithKeys("ctrl+n"),
            key.WithHelp("ctrl+n", "prev profile"),
        ),
    }
}
```

Include the new keys in `ShortHelp` / `FullHelp` accessors only when `registry != nil && len(Names) > 1`. If the existing help builders are static, leave them for now — Task 4.4 will add the runtime gate when the model builds help output.

- [ ] **Step 4.1.4: Run tests**

Run: `go test ./internal/tui/... -v`

Expected: PASS.

- [ ] **Step 4.1.5: Commit**

```bash
git add internal/tui/keys.go internal/tui/keys_test.go
git commit -m "feat(tui): add Ctrl+P / Ctrl+N bindings for profile cycling"
```

---

### Task 4.2: Implement the switch flow

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/actions.go` (if the switch cmd belongs with other actions)
- Test: `internal/tui/model_test.go`

- [ ] **Step 4.2.1: Write the failing test**

Append to `internal/tui/model_test.go`. Use the existing test fixtures for `wtSvc`, `projSvc`, `sesSvc`, `tmuxClient`. Two scenarios:

```go
func TestSwitchProfile_cyclesForwardAndReusesCache(t *testing.T) {
    // Arrange: model with two profiles configured. Use in-memory fakes.
    reg := &config.ProfileRegistry{
        Active:      "personal",
        Names:       []string{"personal", "work"},
        ProfilesDir: t.TempDir(),
    }
    // Write a work.toml so LoadProfile succeeds for the "work" switch.
    workPath := filepath.Join(reg.ProfilesDir, "work.toml")
    if err := os.WriteFile(workPath, []byte(`[projects.b]
repo = "git@x:b"
`), 0o600); err != nil {
        t.Fatal(err)
    }
    // Seed the cache with "personal" so we can assert pointer reuse.
    personalCfg := &config.Config{ /* minimal */ }
    m := tui.NewModel(personalCfg, nil, nil, nil, nil, false, reg)

    m2 := m.SwitchProfileForTest(+1) // cycle forward
    if m2.Registry().Active != "work" {
        t.Errorf("after forward cycle, Active = %q, want work", m2.Registry().Active)
    }
    m3 := m2.SwitchProfileForTest(+1) // cycle forward again — back to personal
    if m3.Registry().Active != "personal" {
        t.Errorf("after second forward cycle, Active = %q, want personal", m3.Registry().Active)
    }
    // Cache reuse: the personal *Config pointer on m3 must equal m's.
    if m3.CurrentConfigForTest() != personalCfg {
        t.Error("expected personal *Config to be reused from cache")
    }
}
```

`SwitchProfileForTest`, `Registry`, and `CurrentConfigForTest` are test-only accessors (defined in `internal/tui/model_export_test.go` with `// Only used in tests.`). If the codebase prefers exported helpers in a `tui_internal_test.go` with build-tagged fakery, follow that convention — read one of the existing `_internal_test.go` files first.

- [ ] **Step 4.2.2: Run tests to verify failure**

Run: `go test ./internal/tui/... -run TestSwitchProfile -v`

Expected: FAIL — methods don't exist.

- [ ] **Step 4.2.3: Implement `switchProfile`**

This step adds a new import — `github.com/xico42/codeherd/internal/hooks` — to `internal/tui/model.go` (currently absent; `switchProfile` constructs a `worktree.Service` which requires `&hooks.NoOp{}`).

In `internal/tui/model.go`:

```go
// switchProfile cycles the active profile by `direction` (+1 forward,
// -1 backward). On success, returns a new Model with cfg/wtSvc/projSvc
// swapped. On failure, returns the receiver with statusMsg set.
func (m Model) switchProfile(direction int) (Model, tea.Cmd) {
    if m.registry == nil || len(m.registry.Names) < 2 {
        return m, nil
    }
    idx := indexOf(m.registry.Names, m.registry.Active)
    if idx < 0 {
        return m, nil
    }
    n := len(m.registry.Names)
    next := m.registry.Names[((idx+direction)%n + n) % n]

    bundle, ok := m.profileCache[next]
    if !ok {
        cfg, err := config.LoadProfile(m.registry.ProfilesDir, next)
        if err != nil {
            m.statusMsg = fmt.Sprintf("profile switch failed: %v", err)
            return m, nil
        }
        wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), m.tmuxClient, &hooks.NoOp{})
        projSvc := project.NewService(cfg, project.NewRealGitRunner(), &hooks.NoOp{})
        bundle = profileBundle{cfg: cfg, wtSvc: wtSvc, projSvc: projSvc}
        m.profileCache[next] = bundle
    }

    m.cfg = bundle.cfg
    m.wtSvc = bundle.wtSvc
    m.projSvc = bundle.projSvc
    // Mutates the shared *config.ProfileRegistry pointer (not a Model copy).
    // Intentional: the registry is owned by the TUI singleton and reads
    // from it must see the currently active profile regardless of which
    // Model value the Bubble Tea runtime is holding.
    m.registry.Active = next
    m.statusMsg = "Switched to profile " + next
    return m, m.refreshCmd()
}

func indexOf(names []string, target string) int {
    for i, n := range names {
        if n == target {
            return i
        }
    }
    return -1
}
```

Add a failing test for the error path (load failure → status message, no swap):

```go
func TestSwitchProfile_loadFailurePreservesActive(t *testing.T) {
    reg := &config.ProfileRegistry{
        Active:      "personal",
        Names:       []string{"personal", "bogus"},
        ProfilesDir: t.TempDir(), // no bogus.toml written — LoadProfile will fail
    }
    personalCfg := &config.Config{}
    m := tui.NewModel(personalCfg, nil, nil, nil, nil, false, reg)

    m2 := m.SwitchProfileForTest(+1)
    if m2.Registry().Active != "personal" {
        t.Errorf("Active = %q, want unchanged (personal) after load failure", m2.Registry().Active)
    }
    if m2.StatusMsgForTest() == "" {
        t.Error("expected statusMsg to describe the failure")
    }
}
```

Add matching test-only accessor in the export file:

```go
func (m Model) StatusMsgForTest() string { return m.statusMsg }
```

Wire it into `updateList` (next to the other `key.Matches` branches):

```go
case key.Matches(msg, m.keys.NextProfile):
    return m.switchProfile(+1)
case key.Matches(msg, m.keys.PrevProfile):
    return m.switchProfile(-1)
```

Both bindings must be ignored while the list filter is active (same guard that wraps the other key branches in `updateList`). Confirm by reading the existing switch block in `model.go:215-249`.

Add test-only accessors in `internal/tui/model_export_test.go` (or whichever file exports internals):

```go
// Test-only accessors — not part of the public API.
func (m Model) SwitchProfileForTest(direction int) Model {
    m2, _ := m.switchProfile(direction)
    return m2
}

func (m Model) Registry() *config.ProfileRegistry { return m.registry }

func (m Model) CurrentConfigForTest() *config.Config { return m.cfg }
```

- [ ] **Step 4.2.4: Run tests**

Run: `go test ./internal/tui/... -v`

Expected: PASS.

- [ ] **Step 4.2.5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_export_test.go internal/tui/model_test.go
git commit -m "feat(tui): cycle profiles with Ctrl+P / Ctrl+N and cache services per profile

Pressing Ctrl+P/Ctrl+N on the list screen rebuilds cfg/wtSvc/projSvc
for the next profile via config.LoadProfile; hits an in-memory cache
on repeat visits. Load failures abort the swap and surface via
statusMsg. sesSvc and tmuxClient are shared across profiles."
```

---

### Task 4.3: Filter session listing by active profile in `refreshCmd`

**Files:**
- Modify: `internal/tui/model.go` (the `refreshCmd` closure)
- Test: `internal/tui/model_test.go`

- [ ] **Step 4.3.1: Write the failing test**

Append to `internal/tui/model_test.go`:

```go
func TestRefresh_filtersSessionsByActiveProfile(t *testing.T) {
    // Build a fake tmux client returning sessions across multiple profiles.
    // (Use whatever fake tmux.Client infra the existing tests rely on.)
    // Assert refresh payload only includes sessions where Profile == active.
    t.Skip("TODO: wire once fake tmux.Client is accessible from test helpers")
}
```

If wiring the fake is straightforward (check `actions_test.go` for the pattern), fill in the test. Otherwise, leave it skipped with a TODO and cover this behavior in the integration test in Chunk 5.

- [ ] **Step 4.3.2: Update `refreshCmd`**

In `internal/tui/model.go`, where `refreshCmd` populates `agentSessions` / `shellSessions` from `tmuxClient.ListSessions()` (see `model.go:355-370`):

```go
if tmuxClient != nil {
    records, err := tmuxClient.ListSessions()
    if err == nil {
        active := ""
        if m.registry != nil {
            active = m.registry.Active
        }
        for _, r := range records {
            if active != "" && r.Profile != active {
                continue
            }
            switch r.SessionType {
            // ... existing branches ...
            }
        }
    }
}
```

Note: `m.registry` is captured into the closure — add `registry := m.registry` to the list of captures at the top of `refreshCmd` so the closure reads the value at tick time (matches the existing `wtSvc := m.wtSvc` / `cfg := m.cfg` pattern).

- [ ] **Step 4.3.3: Run tests**

Run: `go test ./internal/tui/... -v`

Expected: PASS (skipped test is skipped, others still pass).

- [ ] **Step 4.3.4: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): filter sessions by active profile in refreshCmd"
```

---

### Task 4.4: Title bar + help gating

**Files:**
- Modify: `internal/tui/model.go` (`viewList`)
- Modify: `internal/tui/keys.go` if help gating lives there

- [ ] **Step 4.4.1: Update `viewList` to render active profile**

In `internal/tui/model.go:277`:

```go
func (m Model) viewList() string {
    // Count agents for title bar.
    agentCount := 0
    for _, item := range m.list.Items() {
        if it, ok := item.(Item); ok && it.HasAgent {
            agentCount++
        }
    }

    title := "codeherd"
    if m.registry != nil {
        title = title + " · " + m.registry.Active
    }
    tb := titleStyle.Render(title)
    if agentCount > 0 {
        // ... existing right-justified counter logic ...
    }
    // ... rest unchanged ...
}
```

- [ ] **Step 4.4.2: Gate help display for profile keys**

Find where `keyMap.ShortHelp` / `FullHelp` are implemented (usually in `keys.go`). Either:

- Have `ShortHelp` / `FullHelp` always include `NextProfile` / `PrevProfile`, and let the help renderer skip empty bindings. OR
- Pass the registry into the help rendering path so it can conditionally include the pair.

Simplest: always include them, but disable the binding when the registry is nil or has <2 names. Disabled bindings don't show in help. All other `Model` methods in this file use value receivers (see `model.go:126,130,215,256,277,329`); match that style and return the updated `Model`:

```go
func (m Model) syncProfileKeyEnabled() Model {
    enabled := m.registry != nil && len(m.registry.Names) > 1
    m.keys.NextProfile.SetEnabled(enabled)
    m.keys.PrevProfile.SetEnabled(enabled)
    return m
}
```

Call it once at the end of `NewModel` (assign back to `m`) and once inside `switchProfile` after `m.registry.Active = next`. No per-tick call is needed because `registry.Names` does not change during the TUI's lifetime.

Add a unit test:

```go
func TestSyncProfileKeyEnabled_gate(t *testing.T) {
    // Nil registry → disabled.
    m := tui.NewModel(&config.Config{}, nil, nil, nil, nil, false, nil)
    if m.NextProfileEnabledForTest() {
        t.Error("NextProfile enabled with nil registry, want disabled")
    }
    // One profile → disabled.
    reg1 := &config.ProfileRegistry{Active: "a", Names: []string{"a"}, ProfilesDir: t.TempDir()}
    m = tui.NewModel(&config.Config{}, nil, nil, nil, nil, false, reg1)
    if m.NextProfileEnabledForTest() {
        t.Error("NextProfile enabled with one profile, want disabled")
    }
    // Two profiles → enabled.
    reg2 := &config.ProfileRegistry{Active: "a", Names: []string{"a", "b"}, ProfilesDir: t.TempDir()}
    m = tui.NewModel(&config.Config{}, nil, nil, nil, nil, false, reg2)
    if !m.NextProfileEnabledForTest() {
        t.Error("NextProfile disabled with two profiles, want enabled")
    }
}
```

Add matching test-only accessor:

```go
func (m Model) NextProfileEnabledForTest() bool { return m.keys.NextProfile.Enabled() }
```

- [ ] **Step 4.4.3: Run all tests and manually tick the TUI**

Run: `go test ./...`

Expected: PASS.

Manual smoke check (one-off during task, not in plan):

- With a temp `./tmp/` setup (see Chunk 5 manual validation), launch the TUI with `./ch --config=./tmp/config.toml --no-tmux`.
- Verify the title reads `codeherd · personal`.
- Press `Ctrl+P` → title reads `codeherd · work`.
- Press `?` to open help; confirm `ctrl+p` / `ctrl+n` appear.

- [ ] **Step 4.4.4: Commit**

```bash
git add internal/tui/model.go internal/tui/keys.go
git commit -m "feat(tui): render active profile in title and gate profile-cycle help"
```

---

## Chunk 5: Integration test and acceptance

### Task 5.1: In-process integration test

**Files:**
- Create: `cmd/profiles_integration_test.go`

- [ ] **Step 5.1.1: Copy setup idioms from `cmd/session_integration_test.go`**

Read `cmd/session_integration_test.go` to absorb the `//go:build integration` idiom, `runCmd` usage, and the `initBareRepo` helper. Follow the same patterns.

- [ ] **Step 5.1.2: Write the integration test**

Create `cmd/profiles_integration_test.go`:

```go
//go:build integration

package cmd_test

import (
    "bytes"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    "github.com/xico42/codeherd/internal/config"
)

func setupProfilesTree(t *testing.T) (mainPath string) {
    t.Helper()
    root := t.TempDir()
    personalProjects := filepath.Join(root, "personal-projects")
    workProjects := filepath.Join(root, "work-projects")

    // Write profile files.
    profilesDir := filepath.Join(root, "profiles")
    if err := os.MkdirAll(profilesDir, 0o700); err != nil {
        t.Fatal(err)
    }
    personal := `[defaults]
projects_dir = "` + personalProjects + `"
agent = "test-agent"

[agents.test-agent]
cmd = "true"

[projects.myapp]
repo = "git@github.com:u/myapp.git"
`
    work := `[defaults]
projects_dir = "` + workProjects + `"
agent = "test-agent"

[agents.test-agent]
cmd = "true"

[projects.other]
repo = "git@github.com:u/other.git"
`
    for name, body := range map[string]string{"personal.toml": personal, "work.toml": work} {
        if err := os.WriteFile(filepath.Join(profilesDir, name), []byte(body), 0o600); err != nil {
            t.Fatal(err)
        }
    }
    main := `[defaults]
profiles_enabled = true
profiles_dir = "` + profilesDir + `"
main_profile = "personal"
`
    mainPath = filepath.Join(root, "config.toml")
    if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
        t.Fatal(err)
    }
    return mainPath
}

// captureStdout redirects os.Stdout during f and returns what was written.
// Not safe for t.Parallel — mutates the package-global os.Stdout.
func captureStdout(t *testing.T, f func()) string {
    t.Helper()
    orig := os.Stdout
    r, w, _ := os.Pipe()
    os.Stdout = w
    done := make(chan string)
    go func() {
        var buf bytes.Buffer
        io.Copy(&buf, r)
        done <- buf.String()
    }()
    f()
    w.Close()
    os.Stdout = orig
    return <-done
}

func TestProfiles_listProjectScopedToActiveProfile(t *testing.T) {
    main := setupProfilesTree(t)

    out := captureStdout(t, func() {
        _ = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
    })
    if !strings.Contains(out, "myapp") || strings.Contains(out, "other") {
        t.Errorf("personal list project out = %q, want myapp only", out)
    }

    out = captureStdout(t, func() {
        _ = runCmd(t, "--config", main, "--profile", "work", "list", "project")
    })
    if !strings.Contains(out, "other") || strings.Contains(out, "myapp") {
        t.Errorf("work list project out = %q, want other only", out)
    }
}

func TestProfiles_defaultProfileMatchesMainProfile(t *testing.T) {
    main := setupProfilesTree(t)

    defaulted := captureStdout(t, func() {
        _ = runCmd(t, "--config", main, "list", "project")
    })
    explicit := captureStdout(t, func() {
        _ = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
    })
    if defaulted != explicit {
        t.Errorf("default output != explicit -p personal\ndefault=%q\nexplicit=%q", defaulted, explicit)
    }
}

func TestProfiles_noMainProfile_errors(t *testing.T) {
    main := setupProfilesTree(t)
    // Rewrite main without main_profile.
    content := `[defaults]
profiles_enabled = true
profiles_dir = "` + filepath.Join(filepath.Dir(main), "profiles") + `"
`
    if err := os.WriteFile(main, []byte(content), 0o600); err != nil {
        t.Fatal(err)
    }

    err := runCmd(t, "--config", main, "list", "project")
    if err == nil {
        t.Fatal("runCmd() error = nil, want error when no profile resolved")
    }
    if !strings.Contains(err.Error(), "main_profile") && !strings.Contains(err.Error(), "-p") {
        t.Errorf("error = %v, want message mentioning main_profile or -p", err)
    }
}

func TestProfiles_sessionIsolationAcrossProfiles(t *testing.T) {
    if _, err := exec.LookPath("tmux"); err != nil {
        t.Skip("tmux not available")
    }

    main := setupProfilesTree(t)
    root := filepath.Dir(main)
    personalProjects := filepath.Join(root, "personal-projects")
    workProjects := filepath.Join(root, "work-projects")
    initBareRepo(t, filepath.Join(personalProjects, "github.com", "u", "myapp"))
    initBareRepo(t, filepath.Join(workProjects, "github.com", "u", "other"))

    // Create sessions under each profile.
    // Use "feat" (not "main") as the branch — matches cmd/session_integration_test.go.
    // Using the repo's default branch name (often "main") would collide with
    // `git worktree add -b main` when the bare repo already has HEAD on main.
    if err := runCmd(t, "--config", main, "--profile", "personal", "create", "session", "myapp", "feat"); err != nil {
        t.Fatalf("personal create session: %v", err)
    }
    if err := runCmd(t, "--config", main, "--profile", "work", "create", "session", "other", "feat"); err != nil {
        t.Fatalf("work create session: %v", err)
    }
    defer exec.Command("tmux", "kill-session", "-t", "personal-myapp-feat").Run()
    defer exec.Command("tmux", "kill-session", "-t", "work-other-feat").Run()

    // Listing under personal must not include the work session.
    out := captureStdout(t, func() {
        _ = runCmd(t, "--config", main, "--profile", "personal", "list", "session")
    })
    if !strings.Contains(out, "myapp") || strings.Contains(out, "other") {
        t.Errorf("personal list session out = %q, want myapp only", out)
    }

    // tmux ls reveals the on-disk prefixed names.
    tmuxOut, _ := exec.Command("tmux", "ls").Output()
    if !strings.Contains(string(tmuxOut), "personal-myapp-feat") {
        t.Errorf("tmux ls did not show personal-myapp-feat:\n%s", tmuxOut)
    }
    if !strings.Contains(string(tmuxOut), "work-other-feat") {
        t.Errorf("tmux ls did not show work-other-feat:\n%s", tmuxOut)
    }
}

func TestProfiles_strayKeysWarning(t *testing.T) {
    main := setupProfilesTree(t)
    // Append a stray [projects.foo] to the main config.
    f, err := os.OpenFile(main, os.O_APPEND|os.O_WRONLY, 0o600)
    if err != nil {
        t.Fatal(err)
    }
    if _, err := f.WriteString("\n[projects.foo]\nrepo = \"git@x:foo\"\n"); err != nil {
        t.Fatal(err)
    }
    f.Close()

    // Intercept the warning via the explicit sink (set in Chunk 1
    // Task 1.3.3). Swapping os.Stderr wouldn't work because warningSink
    // caches os.Stderr as its default at package-init time.
    var buf bytes.Buffer
    config.SetWarningSink(&buf)
    defer config.SetWarningSink(nil)

    err = runCmd(t, "--config", main, "--profile", "personal", "list", "project")
    if err != nil {
        t.Fatalf("runCmd() error = %v, want nil (warning, not failure)", err)
    }
    if !strings.Contains(buf.String(), "profiles_enabled=true") {
        t.Errorf("warning sink = %q, want stray-keys warning", buf.String())
    }
}
```

- [ ] **Step 5.1.3: Run the integration test**

Run: `make test-integration`

Expected: PASS on a machine with `tmux` and `git` available. The tmux-requiring test skips when unavailable.

- [ ] **Step 5.1.4: Commit**

```bash
git add cmd/profiles_integration_test.go
git commit -m "test(integration): cover profile isolation across config, sessions, warnings"
```

---

### Task 5.2: Manual acceptance validation (one-off)

**Not automated.** Run once before merging the feature branch.

- [ ] **Step 5.2.1: Build**

Run: `make build`

Expected: `./ch` binary produced in repo root.

- [ ] **Step 5.2.2: Create the `./tmp/` tree**

Make:

```
./tmp/
├── config.toml
├── personal-projects/
├── work-projects/
└── profiles/
    ├── personal.toml
    └── work.toml
```

`./tmp/config.toml`:

```toml
[defaults]
profiles_enabled = true
profiles_dir = "./tmp/profiles"
main_profile = "personal"
```

`./tmp/profiles/personal.toml`:

```toml
[defaults]
projects_dir = "./tmp/personal-projects"
agent = "claude"

[projects.myapp]
repo = "git@github.com:example/myapp.git"

[agents.claude]
cmd = "true"
```

`./tmp/profiles/work.toml`:

```toml
[defaults]
projects_dir = "./tmp/work-projects"
agent = "claude"

[projects.other]
repo = "git@github.com:example/other.git"

[agents.claude]
cmd = "true"
```

- [ ] **Step 5.2.3: Run the binary and eyeball each case**

| Command | Expected |
|---|---|
| `./ch --config=./tmp/config.toml list project` | only `myapp` (personal is `main_profile`) |
| `./ch --config=./tmp/config.toml -p=work list project` | only `other` |
| `./ch --config=./tmp/config.toml -p=nope list project` | clear error, profile "nope" not found |
| remove `main_profile` from `./tmp/config.toml`; re-run `./ch --config=./tmp/config.toml list project` | clear error mentioning `main_profile` or `-p` |
| restore `main_profile`; run `./ch --config=./tmp/config.toml --no-tmux` and press `Ctrl+P` | title flips to `codeherd · work`; press `Ctrl+N` → back to `codeherd · personal` |

- [ ] **Step 5.2.4: Tear down**

Run:

```bash
rm -rf ./tmp
tmux kill-server 2>/dev/null || true
```

- [ ] **Step 5.2.5: Final verification gate**

Run: `make check`

Expected: `OK: <coverage>% >= 80%` and all targets pass.

If coverage drops below 80%, identify the gap via `go test -coverprofile=/tmp/cov.out ./... && go tool cover -func=/tmp/cov.out | sort -k3 -n | head -20` and add unit tests for the least-covered new functions before merging.

---

## Completion checklist

- [ ] All tasks in Chunks 1–5 complete and committed
- [ ] `make check` passes (coverage ≥ 80%, integration tests, lint, build)
- [ ] Manual acceptance validation (Task 5.2) completed and scratch dir removed
- [ ] `docs/plans/2026-04-23-profiles-design.md` and `docs/plans/2026-04-23-profiles-plan.md` both committed

---

## Skill references

- superpowers:test-driven-development — follow for every task
- superpowers:verification-before-completion — run `make check` before declaring done
