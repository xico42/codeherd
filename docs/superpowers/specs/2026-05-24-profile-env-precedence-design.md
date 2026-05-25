# Make `ch` respect `CODEHERD_PROFILE` (issue #10)

## Problem

When `ch` runs inside a codeherd agent or shell session, it does not default to
that session's active profile. A command like `ch run <agent>` (used to launch
another agent from within a session) ignores the profile the session was started
under, so it resolves agents/projects from the wrong config scope.

codeherd already stamps `CODEHERD_PROFILE` into every profile-mode session's
environment (`internal/session/session.go`), but nothing consumes it on the way
back in.

## Goal

Make profile selection context-aware. Resolve the active profile by precedence
(lowest to highest):

1. `defaults.main_profile` in the config file
2. `CODEHERD_PROFILE` environment variable
3. `--profile` / `-p` CLI flag

## Non-goals

- No change to `--profile` flag semantics. It remains the highest-precedence
  source; we only add a middle tier below it.
- No use of other `CODEHERD_*` vars as context. Scope is profile selection only.
- No change to behavior when `profiles_enabled = false`.

## Background

`config.Load(path, profileName string)` already resolves the active profile as
`profileName → defaults.main_profile → error`. The `--profile` flag value is
passed as `profileName` from `cmd/root.go`'s `PersistentPreRunE`.

`CODEHERD_PROFILE` is exported into a session's environment only when the session
was started with a non-empty profile (`session.go:132-134`,
`semconv.EnvProfile = "CODEHERD_PROFILE"`).

Only one caller invokes `config.Load`: `cmd/root.go`. The TUI's profile cycling
uses `config.LoadProfile` directly and is unaffected.

## Design

### Resolution point: `cmd/root.go`

Resolve flag-or-env in `PersistentPreRunE`, then pass the result to
`config.Load` as `profileName`. Because `config.Load` already falls back to
`defaults.main_profile`, this yields the exact precedence chain with no change to
the `config` package:

```go
profileArg := profileFlag
if profileArg == "" {
    profileArg = os.Getenv(semconv.EnvProfile)
}
cfg, registry, err = config.Load(cfgFile, profileArg)
```

- Flag wins: env is consulted only when the flag is empty.
- `config.Load` then falls back to `main_profile` when both flag and env are empty.
- The `config` package stays pure — no `os.Getenv` buried inside it, and the
  doc comment on `Load` (`profileName → main_profile → error`) stays accurate.

Extract the flag-or-env step into a small testable helper so it can be unit
tested without driving cobra:

```go
// resolveProfileArg returns the profile name to hand to config.Load:
// the --profile flag if set, otherwise $CODEHERD_PROFILE.
func resolveProfileArg(flag string) string {
    if flag != "" {
        return flag
    }
    return os.Getenv(semconv.EnvProfile)
}
```

### Behavior in edge cases

| Situation | Behavior | Rationale |
|---|---|---|
| `profiles_enabled = false`, env set | env ignored | `config.Load` returns early before consulting `profileName`; matches `--profile` today |
| env names a missing/deleted profile | error `profile %q not found` | identical to passing `--profile` to a missing profile; surfaces misconfig loudly |
| flag and env both set | flag wins | stated precedence |
| neither set, `main_profile` set | `main_profile` | config default tier |
| `CODEHERD_PROFILE` empty string | treated as unset | empty flag and empty env both fall through |

### Covered automatically

`ch run <agent>`, every CLI verb, and the no-arg TUI launch all flow through
`PersistentPreRunE`, so all become context-aware with no per-command work.

## Documentation updates

These are part of the change, not follow-ups.

### CLI flag help (`cmd/root.go`)

Current:

```
"profile to load (when profiles_enabled=true)"
```

New — state the precedence so `ch --help` reflects it:

```
"profile to load; overrides $CODEHERD_PROFILE and defaults.main_profile (requires profiles_enabled=true)"
```

### README

The README documents the `CODEHERD_PROFILE` env var row but has no profiles
section and no precedence statement. Add a concise **Profiles** subsection under
`## Configuration` covering:

- `profiles_enabled`, `profiles_dir`, `defaults.main_profile`
- the active-profile precedence chain: `main_profile` < `CODEHERD_PROFILE` env < `--profile` flag
- that `CODEHERD_PROFILE` is stamped into each profile-mode session, so nested
  `ch` invocations (e.g. `ch run <agent>`) inherit the session's profile

Keep it short and consistent with the existing section style. Update the note on
the `CODEHERD_PROFILE` row in the session-environment table to cross-reference
the precedence behavior.

## Testing

Unit-test `resolveProfileArg` and the end-to-end resolution precedence. Use
`t.Setenv` for env cases. Cases:

- flag only → flag value
- env only → env value
- flag and env both set → flag wins
- neither set, `main_profile` set → `main_profile`
- env set, `profiles_enabled = false` → env ignored, profile mode off
- env names a missing profile → error `profile ... not found`
- `CODEHERD_PROFILE` empty string → treated as unset

Run `make check` (coverage ≥ 80%, integration, lint, build) before completion.

## Scope

- `cmd/root.go`: ~5-line resolution + `resolveProfileArg` helper + flag help text
- `README.md`: Profiles subsection + env-table note
- tests for the resolution helper/precedence

No new files, no config schema change, no `config`-package change.
