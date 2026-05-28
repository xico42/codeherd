# Commit categorization

Authoritative mapping from this project's commit prefixes to Keep a Changelog 1.1.0 sections. The skill's classification step (workflow step 4) reads this file.

This project uses six commit types: `feat`, `fix`, `sec`, `revert`, `docs`, `chore`. Four map mechanically; two (`chore` and `docs`) require per-commit judgment.

The release note (`docs/release-notes/<version>.md`) inherits the CHANGELOG.md verdict for its trailing `## Changes` section (it copies that block verbatim). The "In release note body" column below covers only the friendly top half — Summary, Highlights, Breaking, Fixes — where prose is hand-authored.

## Table

| Prefix      | Section in CHANGELOG.md            | In release note body | Notes                                                                                                                                |
| ----------- | ---------------------------------- | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `feat`      | Added                              | yes                  | `feat!` or `BREAKING CHANGE:` footer → also list under `## Important: breaking changes`.                                             |
| `fix`       | Fixed                              | yes                  |                                                                                                                                       |
| `sec`       | Security                           | yes                  |                                                                                                                                       |
| `revert`    | Removed                            | yes                  | Subject typically `revert: <original subject>`.                                                                                       |
| `docs`      | judgment per commit (default drop) | judgment             | Inspect: changes to user-facing docs (CLI help text, README upgrade guide, public-API reference) → `Changed`. Contributor docs, architecture notes, internal design specs → drop. |
| `chore`     | judgment per commit (default drop) | judgment             | Inspect every chore. See "Chore triage" below. Never silently drop without checking the body.                                         |
| no prefix   | Changed                            | (judgment)           | Add a classification warning at the approval gate so the maintainer can retag.                                                       |

Historical commits using prefixes outside the six (`refactor:`, `perf:`, `test:`, `ci:`, `build:`, `style:`, `security:`, `deprecate:`, etc.) are treated as a `chore` of the corresponding kind and triaged per the chore rules below — except `security:`, which maps to `sec` (the modern short form for the same intent).

## Chore triage

`chore` is a catchall. Every chore commit goes through this decision:

1. **Read the body.** Subject alone is not enough.
2. **Does it change user-visible behavior?** Examples that count: a runtime dependency bump on a library shipped with the binary (especially a CVE patch); a refactor that alters logged output, exit codes, or a wire format; a build/CI change that produces a new install path or a renamed artefact; a performance improvement large enough to mention. If yes → include under the matching Keep a Changelog section (`Changed`, `Security`, etc.).
3. **Is it pure repo upkeep?** Examples: test additions, internal refactor with zero observable side effect, formatting/lint fixes, contributor-doc edits, CI tweaks that only affect the maintainer's workflow. If yes → drop.
4. **When in doubt, include.** A bullet the maintainer removes at the approval gate is cheap; a silent drop the maintainer never sees is invisible.

This project does not use commit scopes, so the chore subject alone carries no hint about what kind of upkeep it is. Read the body and the diff — `go.mod`/`go.sum` touched means a dependency change; `.github/workflows/*` means CI; `*_test.go` only means tests; etc. The triage decision lives in the diff, not the subject.

## Breaking changes

A commit is breaking when:

- The subject uses `<type>!:`, e.g. `feat!: drop /v1`.
- The body contains a paragraph starting with `BREAKING CHANGE:`.
- A command is renamed or removed, even with a deprecation window — the old form is breaking for any caller that pinned it. If the author shipped it without `!` or `BREAKING CHANGE:`, surface the inconsistency at the approval gate and retag.

In `CHANGELOG.md`, every breaking commit appears under its underlying Keep a Changelog section (`### Added`, `### Changed`, `### Deprecated`, `### Removed`, `### Fixed`, or `### Security`) with a `**BREAKING:**` prefix on the bullet. Keep a Changelog 1.1.0 has no "Breaking" section, so do not invent one.

```markdown
### Changed

- **BREAKING:** rename `ch attach` to `ch session attach`; old form prints a deprecation warning for one release.
```

In the **release note's friendly top half**, every breaking commit also appears under `## Important: breaking changes` with a user-facing summary. The trailing `## Changes` block is the verbatim CHANGELOG.md copy, so the same bullet shows up there too with its `**BREAKING:**` prefix — that's expected, not a duplication bug.

## Deprecations

When a commit deprecates a public surface (a renamed CLI command, an exported Go API, a config key), the **old form** is listed under `### Deprecated`. The **new form** is listed under its own section (`### Added` or `### Changed`).

Example: a single commit that renames `ch attach` to `ch session attach` produces two classification records, one per surface:

- record A (old form) → section `Deprecated`, `breaking: true`. This is the record summarized under `## Important: breaking changes` in the release note's friendly top half.
- record B (new form) → section `Added` (or `Changed` if the form already existed under a different name), `breaking: false`. This record does *not* appear in any Breaking block.

**Anti-double-listing rule:** within `CHANGELOG.md`, every commit appears at most once per Keep a Changelog section. If you find yourself emitting the same bullet in two sections with the same `**BREAKING:**` marker, the second occurrence is a duplicate — collapse it. The two-record split above is correct because the two records represent two distinct user-visible surfaces (the deprecated old form and the new canonical form).

## Merging commits

When several commits share an obvious theme (same area, same feature), the **friendly top half** of the release note may merge them into one bullet or one Highlight. `CHANGELOG.md` (and therefore the trailing `## Changes` block) keeps one bullet per logical change.

## When in doubt

Pick `Changed` over silent drop. The maintainer reviews the diff at the approval gate.
