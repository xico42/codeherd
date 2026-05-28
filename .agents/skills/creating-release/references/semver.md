# Semantic Versioning 2.0.0 — condensed

Source: https://semver.org/spec/v2.0.0.html

## Version format

```
<MAJOR>.<MINOR>.<PATCH>[-<prerelease>][+<build>]
```

- `<MAJOR>`, `<MINOR>`, `<PATCH>` are non-negative integers without leading zeros.
- Prerelease and build metadata are optional. This skill does not produce them.

## When to bump

| Bump  | Trigger                                                                                              |
| ----- | ---------------------------------------------------------------------------------------------------- |
| MAJOR | Incompatible / breaking changes to the public API.                                                   |
| MINOR | Backwards-compatible feature additions.                                                              |
| PATCH | Backwards-compatible bug fixes (including security fixes that don't change behavior).                |

When you bump a higher level, lower levels reset to zero. `1.4.7` + breaking change → `2.0.0`, not `2.4.7`.

## The 0.x convention

SemVer §4 says:

> Major version zero (0.y.z) is for initial development. Anything MAY change at any time. The public API SHOULD NOT be considered stable.

Common practice during `0.y.z` — and what this skill applies — is:

| Signal at this level                  | Bump while major == 0 |
| ------------------------------------- | --------------------- |
| Breaking change (would be MAJOR ≥ 1)  | **MINOR**             |
| Feature addition (MINOR ≥ 1)          | MINOR                 |
| Bug fix / security                    | PATCH                 |

The maintainer can override at invocation time (`release as major` while still at 0.x to declare 1.0.0 explicitly).

## First release

The first tagged release of this project is `0.1.0`. Not `0.0.1` and not `1.0.0`. This is hard-coded in the skill's workflow step 5.

## Precedence rules used by `write-version.sh` (informational)

- Compare `<major>.<minor>.<patch>` numerically left to right.
- A version without prerelease metadata has higher precedence than the same version with prerelease metadata: `1.0.0` > `1.0.0-rc.1`.
- Build metadata is ignored for precedence.

The skill rejects any new version that is not strictly greater than the current `VERSION` by these rules.
