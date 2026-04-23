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
  `defaults.projects_dir`, `defaults.agent` present there is **ignored
  with a one-time stderr warning, per `Load(mainPath)` call** (not globally
  cached — each top-level `Load` emits the warning if applicable).
- Profile resolution precedence: `--profile` flag → `main_profile` in main
  config → **error**. No implicit fallback.
- Fail-fast on misconfiguration: missing `profiles_dir`, missing profile
  file, parse error, unresolved profile name — all errors at `Load` time.
- `config.Load` stays the single loader. New signature:
  `Load(path, profileName string) (*Config, *ProfileRegistry, error)`.
  Only `cmd/root.go` calls `Load`; no service test is affected by this
  signature change.
- CLI surface change: a new persistent flag `-p/--profile` on the root
  command. Every subcommand inherits it. No other CLI shape changes.
- Session naming becomes profile-aware via `internal/semconv`. When the
  active profile is `""` (profiles disabled) names stay
  `<project>-<branch>` (unchanged). When profile is non-empty, names are
  `<profile>-<project>-<branch>` — this is for tmux-level uniqueness so
  two profiles with the same `(project, branch)` don't collide in tmux.
  **Profile identity is not recovered by parsing the name**; parsing
  `<profile>-<project>-<branch>` is ambiguous when any component contains
  `-` (branches often do). Instead, profile is stored as a dedicated tmux
  user option `@codeherd_profile` on each session, read back verbatim
  into the existing `tmux.SessionRecord`. The name is just the tmux
  identifier; the option is the source of truth for filtering.
- The TUI gets a `shift+tab` binding that cycles profiles. The key is
  shown in help only when `registry != nil && len(Names) > 1`.
- Services that have **no `*Config` dependency** (`tmuxClient`, `sesSvc`,
  hooks, runners) are **shared** across profiles in the TUI. Services
  that bind to `*Config` (`wtSvc`, `projSvc`) are **rebuilt per profile**
  and cached by profile name for the TUI's lifetime. The cache is
  monotonic — no eviction — which is fine at N < ~20 profiles.
- **Existing `internal/session` method signatures do not change.** Profile
  awareness enters the service only via **additive** struct fields
  (`StartRequest.Profile`, `SessionInfo.Profile`) and a new tmux user
  option (`@codeherd_profile`). `List` is unchanged; callers filter by
  reading `SessionInfo.Profile`. `Show`/`Stop` keep their
  `(project, branch, sessionType)` signatures — disambiguation between
  profiles with the same `(project, branch)` is achieved by the
  profile-prefixed tmux session name (unique per tmux server), resolved
  via the existing canonical-name lookup that now naturally includes the
  prefix.
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

The existing `SessionName(project, branch)` becomes:

```go
// SessionName returns the canonical tmux session name.
// profile == "" gives "<project>-<branch>" (backward-compatible).
// profile != "" gives "<profile>-<project>-<branch>" (tmux-level uniqueness).
func SessionName(profile, project, branch string) string
```

The name is used as the tmux identifier only. The existing tmux user
option `@codeherd_canonical_name` continues to store the name verbatim
for identity lookups — no parsing required.

New constant:

```go
// TmuxOptionProfile stores the active profile on a session ("" if none).
const TmuxOptionProfile = "@codeherd_profile"
```

No `ParseSessionName` is introduced — profile recovery goes through the
tmux option, not string parsing.

### `internal/tmux`

`tmux.SessionRecord` gains one additive field:

```go
type SessionRecord struct {
    // ... existing fields ...
    Profile string // value of @codeherd_profile, "" when unset
}
```

`ListSessions` reads `@codeherd_profile` as part of its existing
`display-message -p` option plumbing (alongside `@codeherd_canonical_name`,
`@codeherd_session_type`, etc.). No method signature change.

### `internal/session`

**No method signatures change.** Profile awareness is additive:

```go
type StartRequest struct {
    // ... existing fields ...
    Profile string // "" when profiles are disabled
}

type SessionInfo struct {
    // ... existing fields ...
    Profile string
}
```

`Service.Start`:
- Builds the tmux session name with the profile prefix via
  `semconv.SessionName(req.Profile, req.Project, req.Branch)`.
- Writes `@codeherd_profile = req.Profile` on the new tmux session (via
  the same `SetOption` path that already writes `@codeherd_status`,
  `@codeherd_canonical_name`, etc.).

`Service.List`:
- Unchanged signature. Populates `SessionInfo.Profile` from
  `record.Profile`. **Does not filter** — filtering is the caller's
  responsibility. This keeps the service stateless w.r.t. the active
  profile.

`Service.Show` / `Service.Stop`:
- **Existing signatures unchanged.** They continue to take
  `(project, branch, sessionType)` and internally call
  `semconv.SessionName(project, branch)`. With the new semconv signature
  `SessionName(profile, project, branch)`, these existing callsites pass
  through with `profile=""` — fully backward-compatible for no-profile
  mode.
- **Two new additive methods** on the same `*Service` cover the
  profile-aware case:

  ```go
  // StopByName stops the session with the given tmux session name.
  func (s *Service) StopByName(name, sessionType string) error

  // ShowByName returns the SessionInfo for the session with the given name.
  func (s *Service) ShowByName(name, sessionType string) (*SessionInfo, error)
  ```

  Both are thin: they find the record by `name + sessionType` in
  `List()`'s output and delegate to the same tmux operations the existing
  methods use. The cmd layer, which holds the active profile, builds the
  name via `semconv.SessionName(activeProfile, project, branch)` and
  calls the `*ByName` variant.

Net effect on `internal/session` existing tests: they keep passing
unmodified — only **additions** are made (new fields on `StartRequest`
and `SessionInfo`, two new methods, new assertions about the
`@codeherd_profile` option exercised by tests that opt in via
`StartRequest.Profile`).

### `cmd/` session verb dispatch

Session verb commands (`create session`, `delete session`, `show
session`, `attach session`) read `registry.Active` (empty string when
profiles are disabled) and dispatch:

- Profile empty → existing `Start(req)` / `Stop(p, b, t)` / `Show(p, b, t)`.
- Profile non-empty → `Start(req)` with `req.Profile` set; Stop/Show go
  through the `*ByName` variants, with the name built from
  `semconv.SessionName(profile, project, branch)`.

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
`registry != nil`, drops any session whose `record.Profile` (read from
the `@codeherd_profile` tmux option) doesn't equal `registry.Active`.
No name parsing is involved.

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
  keys warning (capturing injected writer, asserting it fires once per
  `Load` call). `ProfileRegistry` enumeration and active name.
  `LoadProfile` round-trip. **Explicit test**: profile-meta keys
  (`profiles_enabled`, `profiles_dir`, `main_profile`) placed inside a
  profile file are silently ignored with no warning.
- `internal/semconv` — `SessionName("", p, b)` yields `p-b`
  (backward-compatible); `SessionName("prof", p, b)` yields `prof-p-b`.
- `internal/tmux` — `ListSessions` populates `SessionRecord.Profile`
  from `@codeherd_profile`; missing option → empty string.
- `internal/session` — existing `Start`/`Stop`/`Show`/`List` tests run
  unchanged against the new code. New test cases:
  - `Start` with `StartRequest.Profile=""` leaves `@codeherd_profile`
    unset (or empty) and produces a `p-b` name.
  - `Start` with `StartRequest.Profile="work"` sets `@codeherd_profile=work`
    and produces a `work-p-b` name.
  - `List` populates `SessionInfo.Profile` verbatim from records.
  - `ShowByName` and `StopByName` match on the given name + type.
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
- `tmux ls` output confirms profile-prefixed session names on disk
  (e.g. a session listed as `personal-myapp-main`), not just filtered at
  the TUI layer.
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
