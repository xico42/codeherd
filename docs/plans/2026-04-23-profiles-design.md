# Profiles Design

Add optional, fully-scoped "profiles" so one codeherd install can juggle
multiple independent contexts (personal, work, client A/B) without merging
their data. When profiles are enabled, the TUI cycles between them with
`shift+tab`; when disabled, behavior is unchanged.

## Context

Users run codeherd against disjoint project sets (personal repos, work
repos, client engagements). Today the single `~/.config/codeherd/config.toml`
forces all of them into one namespace: one `projects_dir`, one `[projects]`
map, one `[agents]` map. There is no way to switch contexts short of
rewriting the file.

This design adds a second layer: the main config file can opt in to
"profile mode," in which it becomes a small pointer file and the real
configuration lives in a directory of per-profile TOML files. Only one
profile is active at a time; profiles are **never merged**.

Related:

- Issue [#2](https://github.com/xico42/codeherd/issues/2).
- Previous refactor `docs/plans/2026-04-21-command-verbs-refactor-design.md`
  consolidated the CLI and removed the old half-built `ProfileConfig`
  (which was remote-phase state, unrelated to this design).

## Decisions

- Profile mode is **opt-in** via `defaults.profiles_enabled = true`. Default
  is `false`; non-profile users see zero behavior change.
- Profile files live in a flat directory: `<profiles_dir>/<name>.toml`.
  Default `profiles_dir` is `<main-config-dir>/profiles/`.
- Each profile file uses the **same schema** as today's `config.toml`:
  `[defaults] projects_dir`, `[defaults] agent`, `[projects.<name>]`,
  `[agents.<name>]`. Profile-meta fields inside a profile file are
  ignored.
- In profile mode, the main config file carries only
  `defaults.profiles_enabled`, `defaults.profiles_dir`,
  `defaults.main_profile`. Any `[projects]`, `[agents]`,
  `defaults.projects_dir`, `defaults.agent` present there is **ignored with
  a one-time stderr warning**.
- Profile resolution precedence: `--profile` flag → `main_profile` in main
  config → **error**. No implicit fallback.
- Fail-fast on misconfiguration: missing `profiles_dir`, missing profile
  file, parse error, unresolved profile name — all errors at `Load` time.
- `config.Load` stays the single loader. New signature:
  `Load(path, profileName string) (*Config, *ProfileRegistry, error)`.
  Existing callers pass `""` for `profileName`.
- CLI surface change: a new persistent flag `-p/--profile` on the root
  command. Every subcommand inherits it. No other CLI shape changes.
- Session naming becomes profile-aware via `internal/semconv`. When the
  active profile is `""` (profiles disabled) names are `<project>-<branch>`
  (unchanged). When profile is non-empty, names are
  `<profile>-<project>-<branch>`.
- The TUI gets a `shift+tab` binding that cycles profiles. The key is
  shown in help only when `registry != nil && len(Names) > 1`.
- Services that have **no `*Config` dependency** (`tmuxClient`, `sesSvc`,
  hooks, runners) are **shared** across profiles in the TUI. Services
  that bind to `*Config` (`wtSvc`, `projSvc`) are **rebuilt per profile**
  and cached by profile name for the TUI's lifetime.
- No file watching, no profile picker screen, no manual "reload profile"
  key in v1. All three are reasonable v2 additions.

## Config Surface

### Main config file, profile mode on

```toml
[defaults]
profiles_enabled = true
profiles_dir     = "~/custom/profiles"  # optional
main_profile     = "personal"           # optional but required at load time
                                        # if not overridden by -p
```

Any of the following in the main file in profile mode are **ignored** with
a one-time warning to stderr:

- `defaults.projects_dir`
- `defaults.agent`
- `[projects]`
- `[agents]`

The warning names the ignored keys so the user knows their intent didn't
take effect.

### Profile file, `<profiles_dir>/<name>.toml`

Identical schema to today's `config.toml` (non-profile mode):

```toml
[defaults]
projects_dir = "~/personal/projects"
agent        = "claude"

[projects.myapp]
repo = "git@github.com:user/myapp.git"

[agents.claude]
cmd  = "claude"
args = ["--dangerously-skip-permissions"]
```

The profile-meta keys (`profiles_enabled`, `profiles_dir`, `main_profile`)
if present inside a profile file are **ignored silently** — they are
meaningless in that context and do not warrant a warning.

### Non-profile mode (default)

Unchanged. `config.toml` holds `[defaults]`, `[projects]`, `[agents]`
exactly as today. `profiles_enabled` defaults to `false` and nothing else
happens.

## Package-Level Design

### `internal/config`

New type:

```go
// ProfileRegistry summarizes discovered profiles and the active one.
// Returned by Load alongside *Config. Commands ignore it; the TUI uses it.
type ProfileRegistry struct {
    Active      string   // "" when profile mode is off
    Names       []string // sorted; empty when off
    ProfilesDir string   // "" when off
}
```

New signature:

```go
// Load reads the main config and, when profiles are enabled, resolves and
// loads the active profile. profileName takes precedence over main_profile
// in the main file. When profiles are disabled, profileName is ignored.
func Load(path, profileName string) (*Config, *ProfileRegistry, error)
```

Existing callers (all in `cmd/root.go`) migrate to `Load(cfgFile,
profileFlag)`. No other file in the codebase calls `Load` today.

Additional helper used by the TUI:

```go
// LoadProfile parses one profile file (<profilesDir>/<name>.toml) and
// returns its Config. Used by the TUI when cycling to an uncached profile.
func LoadProfile(profilesDir, name string) (*Config, error)
```

Warning sink: `Load` writes the one-time stray-keys warning to
`os.Stderr` by default, injectable via an unexported package var for
tests.

### `internal/semconv`

Add:

```go
// SessionName returns the canonical tmux session name.
// profile == "" gives "<project>-<branch>" (backward-compatible).
// profile != "" gives "<profile>-<project>-<branch>".
func SessionName(profile, project, branch string) string

// ParseSessionName is the inverse; ok == false on malformed input.
func ParseSessionName(name string) (profile, project, branch string, ok bool)
```

All existing callsites that formatted `<project>-<branch>` inline move to
`SessionName`. The active profile string is threaded from the call site
(which has `cfg`/`registry` in scope) — services are not modified.

### `internal/session`

`Service.List` gains a filtering step: any session whose parsed profile
prefix doesn't match the caller's active profile is dropped. The active
profile is passed in at call time (new parameter) rather than stored on
the service, keeping the service constructor untouched.

`Service.Start`/`Stop`/`Show` accept the active profile as an additional
argument used only to build/parse names — they perform no merging or
cross-profile lookup.

### `internal/tui`

Model additions:

```go
type Model struct {
    // ... existing fields ...
    registry     *config.ProfileRegistry      // nil when profiles disabled
    profileCache map[string]profileBundle     // keyed by profile name
}

type profileBundle struct {
    cfg     *config.Config
    wtSvc   *worktree.Service
    projSvc *project.Service
}
```

**Shared across profile switches** (not rebuilt): `tmuxClient`, `sesSvc`,
`hooks.NoOp{}`, all runners.

**Rebuilt per profile** (cached): `cfg`, `wtSvc`, `projSvc`.

New key:

```go
Switch key.Binding // "shift+tab"
```

Bound to `tea.KeyShiftTab`. Shown in help only when `registry != nil &&
len(registry.Names) > 1`. Ignored on Form / Confirm / AgentPicker screens.

Switch flow (list screen, `shift+tab` pressed):

1. `next = registry.Names[(indexOf(active) + 1) % len(Names)]`.
2. Look up `next` in `profileCache`.
   - Hit: swap `m.cfg`, `m.wtSvc`, `m.projSvc` to the cached bundle.
   - Miss: `config.LoadProfile(registry.ProfilesDir, next)`; build fresh
     `wtSvc` and `projSvc` bound to that `*Config`; store in cache; swap.
3. `registry.Active = next`.
4. `m.statusMsg = "Switched to profile " + next`.
5. Return `m.refreshCmd()`.

Load failure aborts the swap, leaves the active profile unchanged, and
surfaces the error via `statusMsg`.

Title bar: when `registry != nil`, title reads
`codeherd · <active-profile>`. Unchanged when `registry == nil`.

Refresh filtering: `refreshCmd` enumerates tmux sessions and, when
`registry != nil`, drops sessions whose profile prefix (parsed via
`semconv.ParseSessionName`) doesn't match `registry.Active`.

### `cmd/`

- `cmd/root.go`: add persistent flag `-p, --profile`, pass it into
  `config.Load`. Store both `*Config` and `*ProfileRegistry` package-level
  (alongside existing `cfg`).
- `cmd/tui.go`: pass `registry` into `tui.NewModel`.
- Every other command file: **no change.** They keep using `cfg`.

## Error and Warning Messages

- `profiles_enabled=true but profiles_dir %q does not exist`
- `profiles_enabled=true but no profile specified: set defaults.main_profile in %s or pass -p/--profile`
- `profile %q not found at %s`
- `parsing profile %s: %w`
- Warning: `warning: %s sets profiles_enabled=true; ignoring [defaults.projects_dir | defaults.agent | projects | agents] in main config`
  (emitted once, lists only the keys actually present)

## Tests

### Unit tests

- `internal/config` — all `Load` branches in profile mode:
  resolution precedence, missing dir, missing file, malformed TOML, stray
  keys warning (capturing injected writer). `ProfileRegistry` enumeration
  and active name. `LoadProfile` round-trip.
- `internal/semconv` — `SessionName` / `ParseSessionName` for both the
  profile-on and profile-off shapes; round-trip; malformed-input rejection.
- `internal/session` — `List` filters by active profile; `Start`/`Stop`/
  `Show` unchanged when profile is `""`.
- `internal/tui` —
  - `shift+tab` on list screen cycles and rebuilds services; `sesSvc` /
    `tmuxClient` pointers unchanged.
  - Cache hit: second cycle back to a prior profile reuses the same
    `*Config` pointer.
  - `shift+tab` is a no-op on Form / Confirm / AgentPicker.
  - Title renders profile name when `registry != nil`.
  - Load failure on switch leaves active unchanged and sets `statusMsg`.

### Integration test (`//go:build integration`)

Satisfies issue #2's fourth eval criterion. File:
`cmd/profiles_integration_test.go`.

Setup: temp dir with a main config (`profiles_enabled=true`) and a
`profiles/` dir holding two distinct profile files (distinct
`projects_dir`, distinct agents, distinct `[projects]` maps). Build
`./ch`.

Assertions, each via the built binary:

- `ch -p=personal list project` lists only personal projects.
- `ch -p=work list project` lists only work projects.
- With `main_profile=personal` set, `ch list project` matches
  `ch -p=personal list project`.
- With `main_profile` unset and no `-p`, `ch list project` fails with a
  clear error mentioning `main_profile` / `-p`.
- Sessions created under `-p=personal` are invisible under `-p=work` and
  vice versa (start one in each, list under each, diff).
- A main config carrying stray `[projects]` in profile mode produces the
  warning on stderr but does not fail.

### Coverage

`make check` enforces 80% aggregate. The new code paths in
`internal/config`, `internal/semconv`, and the TUI switch path are the
hot spots and must exercise every branch above.

## Migration

None required. Profile mode is opt-in and `profiles_enabled` defaults to
`false`. Existing configs, session names, and TUI layouts are byte-for-byte
unchanged.

## Out of Scope

- Watching profile files for external edits. A restart re-reads.
- Dedicated profile-picker TUI screen.
- A manual "reload profile" key.
- Merging / inheriting config across profiles.
- Renaming/deleting/creating profiles from the CLI (users edit files
  directly, same as today's main config).
