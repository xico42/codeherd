# Profiles Design

Add optional, fully-scoped "profiles" so one codeherd install can juggle
multiple independent contexts (personal, work, client A/B) without merging
their data. When profiles are enabled, the TUI cycles between them with
`Ctrl+P` (next) / `Ctrl+N` (previous); when disabled, behavior is
unchanged.

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
- The TUI gets `Ctrl+P` (next profile) and `Ctrl+N` (previous profile)
  bindings. They're shown in help only when `registry != nil &&
  len(Names) > 1`. `Tab` / `Shift+Tab` remain reserved for a future
  intra-profile feature (cycling between sessions / worktrees / projects
  lists).
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

New keys:

```go
NextProfile key.Binding // "ctrl+p"
PrevProfile key.Binding // "ctrl+n"
```

Shown in help only when `registry != nil && len(registry.Names) > 1`.
Ignored on Form / Confirm / AgentPicker screens (user is mid-action).
Ignored while the list filter is active (the list owns keypresses during
filtering).

Switch flow (list screen, `Ctrl+P` or `Ctrl+N` pressed):

1. Compute target:
   - `Ctrl+P`: `next = Names[(indexOf(active) + 1) % len(Names)]`
   - `Ctrl+N`: `next = Names[(indexOf(active) - 1 + len(Names)) % len(Names)]`
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
  - `Ctrl+P` on list screen cycles forward and rebuilds services;
    `sesSvc` / `tmuxClient` pointers unchanged.
  - `Ctrl+N` cycles backward.
  - Cache hit: second cycle back to a prior profile reuses the same
    `*Config` pointer.
  - `Ctrl+P` / `Ctrl+N` are no-ops on Form / Confirm / AgentPicker
    screens and while the list filter is active.
  - Title renders profile name when `registry != nil`.
  - Load failure on switch leaves active unchanged and sets `statusMsg`.

### Integration test (`//go:build integration`)

File: `cmd/profiles_integration_test.go`. Uses the same in-process
convention as today's `cmd/session_integration_test.go`: `runCmd`
(`os.Args` + `cmd.Execute()`), real `t.TempDir()` config + profiles
dir, real `git init` for projects, real tmux sessions cleaned up on
teardown, skip when tmux is absent.

Setup: a temp main config with `profiles_enabled=true` and a temp
`profiles/` dir holding two distinct profile files (distinct
`projects_dir`, distinct agents, distinct `[projects]` maps).

Assertions, each via `runCmd`:

- `--profile=personal list project` lists only personal projects
  (stdout captured).
- `--profile=work list project` lists only work projects.
- With `main_profile=personal` in the main config, `list project` with
  no `-p` matches `--profile=personal list project`.
- With `main_profile` unset and no `-p`, `list project` fails with a
  clear error mentioning `main_profile` / `-p`.
- Sessions created under `--profile=personal` are invisible under
  `--profile=work` and vice versa (start one in each, list under each,
  diff). Teardown kills any tmux sessions the test created.
- `tmux ls` confirms profile-prefixed session names on disk (e.g.
  `personal-myapp-main`), verified by calling `tmux ls` directly after
  the `create session` run — not just by TUI filtering.
- A main config carrying stray `[projects]` in profile mode produces
  the warning on stderr (captured via `os.Stderr` swap) but does not
  fail the command.

### Coverage

`make coverage` measures **only unit tests** — it does not pass
`-tags integration`, and `test-integration` runs without
`-coverprofile`. The 80% threshold is therefore satisfied **entirely
by unit tests**; the integration test above contributes functional
assurance but zero coverage points.

Every new branch in this design must be exercised by a unit test:

- `internal/config`: all `Load` branches in profile mode (precedence,
  missing dir, missing file, parse error, stray-keys warning on/off),
  `LoadProfile` round-trip, profile-meta keys silently ignored inside
  a profile file.
- `internal/semconv`: `SessionName` for profile-on and profile-off
  shapes.
- `internal/tmux`: `SessionRecord.Profile` populated from the
  `@codeherd_profile` option; empty when unset.
- `internal/session`: `Start` writing `@codeherd_profile` for the
  profile-on and profile-off cases; `List` populating
  `SessionInfo.Profile`; `StopByName`/`ShowByName` matching on
  `(name, sessionType)`.
- `internal/tui`: `Ctrl+P`/`Ctrl+N` cycling behavior, cache hit,
  no-op on non-list screens and during filtering, title rendering,
  load-failure fallback.

### One-off manual acceptance validation

A non-automated check performed once during implementation (not wired
into `make check`). The fake config lives at `./tmp/` — a repo-local
folder next to the `ch` binary so paths are short, predictable, and
easy to inspect. Add `/tmp/` to `.gitignore` as part of the
implementation.

After the implementation is otherwise complete:

1. `make build` to produce `./ch`.
2. Create the fake config tree:
   - `./tmp/config.toml` with `profiles_enabled = true`,
     `profiles_dir = "./tmp/profiles"`, `main_profile = "personal"`.
   - `./tmp/profiles/personal.toml` and `./tmp/profiles/work.toml`
     with distinct `projects_dir` (e.g. `./tmp/personal-projects`
     and `./tmp/work-projects`), distinct agents, and distinct
     `[projects]` maps.
3. Run, eyeballing each output:
   - `./ch --config=./tmp/config.toml list project`
   - `./ch --config=./tmp/config.toml -p=work list project`
   - `./ch --config=./tmp/config.toml -p=nope list project` (expect
     clear error)
   - With `main_profile` removed from `./tmp/config.toml`,
     `./ch --config=./tmp/config.toml list project` (expect clear
     error mentioning `main_profile` / `-p`)
4. Run `./ch --config=./tmp/config.toml --no-tmux` and exercise
   `Ctrl+P`/`Ctrl+N` in the TUI to confirm the cycle.
5. Tear down: `rm -rf ./tmp`, and `tmux kill-server` if any stray
   sessions remain.

This check exists to satisfy issue #2's fourth eval criterion
literally ("using the built `ch` binary") and to catch regressions
that an in-process test might mask — e.g., init-time globals, flag
parsing ordering, or the persistent flag behaving differently under
`cobra.Execute()` vs a subcommand call. It is not re-run on every
change; it's a one-off acceptance gate.

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

## Known Caveats

- `Ctrl+P` / `Ctrl+N` overlap with Emacs/readline "previous/next
  history" shortcuts. Inside a Bubble Tea TUI those shell bindings don't
  fire, but users with heavily customized terminals may see conflicts.
  If it becomes a recurring complaint, the binding is easy to swap in a
  follow-up.
