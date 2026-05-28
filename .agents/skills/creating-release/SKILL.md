---
name: creating-release
description: Use when preparing a new release of codeherd — bumping the VERSION file, rotating CHANGELOG.md's `[Unreleased]` block, drafting per-version release notes.
---

# Creating a Release

## Overview

This skill cuts a release of codeherd in one Claude Code session, semi-automatically. It analyzes commits since the previous tag, classifies them, picks the next SemVer, and writes three artifacts the GitHub release pipeline consumes later: `VERSION`, `CHANGELOG.md`, and a single `docs/release-notes/<version>.md` file. The maintainer reviews the staged diff and approves once at the end.

**Core principle:** the skill is deterministic about file shape and commit format. It uses judgment only where judgment is required — commit classification, the next-version pick when commit signals are mixed, and release-notes prose.

## When to Use

- Cutting any new release of codeherd, including the first (`0.1.0`).
- The current branch is `release/<version>` cut from `main` (recommended; not enforced).
- Working tree is clean and `git fetch --tags origin` is current.

## When NOT to Use

- Mid-feature commits — this is a release-prep skill, not a per-commit changelog adder.
- Pre-release / RC tags — out of scope for now.
- Any repo other than `xico42/codeherd`.

## Iron Requirements (do not skip)

These are the exact failure modes that occur when this work is done from memory. Treat them as hard rules.

1. **Write the `VERSION` file** at the repo root. One line, no leading `v`, SemVer 2.0.0. No exceptions.
2. **Release-note filename is `docs/release-notes/<version>.md`** — one file per release, no leading `v`, no `-user`/`-technical` suffix.
3. **Final commit is subject-only**: `chore: bump version <version>` — not `release:`, not anything else. No body. The release narrative lives in `docs/release-notes/<version>.md`; the bump commit is a marker, not a place to restate the release. This project does not use commit scopes; do not add one.
4. **CHANGELOG `[Unreleased]` body is reset to** `<!-- Add Unreleased entries here. -->` after rotation. Never left empty.
5. **CHANGELOG.md uses only the six Keep a Changelog 1.1.0 section names** (Added, Changed, Deprecated, Removed, Fixed, Security). No invented sections. Mark a breaking entry by prefixing it with `**BREAKING:**` inside its correct section — e.g. `### Changed` → `- **BREAKING:** rename ...`. The release-note file (which is not bound by Keep a Changelog above its `## Changes` trailer) keeps its own top-level `## Important: breaking changes` block; only `CHANGELOG.md` and the trailer are constrained to the six sections.
6. **Deprecations land in `### Deprecated`**, separately from `### Changed`, even when a single commit is both (e.g. a rename with a deprecation window — that commit appears in both `### Deprecated` for the old form and the relevant `### Added`/`### Changed` for the new form).
7. **Release-note `## Changes` trailer is copied verbatim from CHANGELOG.md.** After `rotate-unreleased.py` writes the `## [<version>] - <date>` block, extract that block's body (the `### Added`/`### Changed`/... subsections) and paste it under `## Changes` in the release note. No SHA links anywhere — CHANGELOG.md has none, so the trailer has none.
8. **Release note ends with** `**Full Changelog:** https://github.com/xico42/codeherd/compare/<prev-tag>...v<version>` after the trailer — or, for the first release, omit the line.
9. **Triage every `chore` and `docs` commit individually** — they are not auto-dropped. Inspect the body and the diff. A runtime dependency bump, a refactor that changes a user-visible log format, a CI change that ships new install artefacts, a docs change to CLI help text — each survives into CHANGELOG.md under the appropriate section. Pure repo upkeep (test additions, internal refactors, formatting, contributor-doc edits) drops. See `references/categorization.md` for the decision rules.
10. **Every prose fragment** (changelog summaries, release-note summary, highlights, upgrade notes, commit body) is authored with the `writing-clearly-and-concisely` skill applied. Use its Limited Context Strategy if context is tight.

## Workflow

Run these steps in order. Stop only at the explicit approval gate in step 9 or on a precondition failure.

### 1. Preconditions

```sh
test -z "$(git status --porcelain)" || { echo "dirty tree"; exit 2; }
git fetch --tags --quiet origin
```

If the working tree is dirty, print `git status --short` and stop. Do not stage anything.

### 2. Range

```sh
prev_tag=$(git tag --list 'v[0-9]*' --sort=-v:refname | head -n1)
```

- `prev_tag` empty → first release. Range is `<root-sha>..HEAD`. Proposed version starts at `0.1.0`.
- `prev_tag` non-empty → range is `${prev_tag}..HEAD`.

### 3. Collect commits

```sh
git log --no-merges --reverse \
        --format='%H%x1f%s%x1f%b%x1e' "${prev_tag:+$prev_tag..}HEAD"
```

Field separator: US (`0x1f`). Record separator: RS (`0x1e`). This survives subjects/bodies that contain tabs and newlines.

Skip any commit whose subject is `chore: bump version <semver>`.

### 4. Classify

Read `references/categorization.md` for the mapping. For each commit produce:

```json
{
  "sha": "...",
  "subject": "...",
  "section": "Added | Changed | Deprecated | Removed | Fixed | Security",
  "breaking": false,
  "user_visible": true,
  "summary":     "<technical phrasing, present tense, omit needless words>",
  "user_summary":"<friendly phrasing; only when user_visible is true>"
}
```

Rules:

- The project uses six commit types: `feat`, `fix`, `sec`, `revert`, `docs`, `chore`. `feat!`/`<type>!` or a `BREAKING CHANGE:` footer sets `breaking: true`.
- Mechanical mapping: `feat` → `Added`, `fix` → `Fixed`, `sec` → `Security`, `revert` → `Removed`.
- `chore` and `docs` require **per-commit analysis** — they are not auto-dropped. Read each body and the diff. User-visible changes (a runtime dependency bump, a refactor with a changed log format, a CI change that ships new install artefacts, a docs change to CLI help text) ship under the matching section. Pure repo upkeep (test additions, internal refactors, formatting, contributor-doc edits) drops. See `references/categorization.md`.
- A renamed/removed command goes in **both** `Deprecated` (old form, for the deprecation window) and `Added`/`Changed` (new form).
- Unrecognised prefix (historical commits using `refactor:`, `ci:`, `test:`, `perf:`, `build:`, `style:`, `security:`, `deprecate:`, etc.) → treat as the `chore` of the corresponding kind (or, for the old `security:` prefix, as `sec`) and triage per the rules above. Flag at the approval gate so the maintainer can confirm.
- `summary` and `user_summary` are authored with `writing-clearly-and-concisely`.

### 5. Propose bump

Apply this table to the classification:

| Signal                                                    | Bump (when prev_tag major ≥ 1) | Bump (while prev_tag major == 0) |
| --------------------------------------------------------- | ------------------------------ | -------------------------------- |
| Any `breaking: true`                                       | MAJOR                          | MINOR                            |
| Else any `Added`                                            | MINOR                          | MINOR                            |
| Else any `Fixed`/`Security`                                 | PATCH                          | PATCH                            |
| Else                                                       | PATCH                          | PATCH                            |

First release with no `prev_tag` → `0.1.0` (regardless of signal).

The maintainer may override with `release as patch|minor|major` at invocation time; print both the override and the auto-derived bump in the approval summary.

### 6. Render templates

Source: `templates/changelog-section.tmpl.md`, `templates/release-notes.tmpl.md`.

Variables:

| Name               | Source                                                                                                   |
| ------------------ | -------------------------------------------------------------------------------------------------------- |
| `Version`          | proposed semver, no leading `v`                                                                          |
| `Date`             | `YYYY-MM-DD`, local                                                                                      |
| `PrevTag`          | `prev_tag` (with `v`), empty for first release                                                           |
| `ReleaseKind`      | `Major`, `Minor`, or `Patch` from step 5                                                                 |
| `Sections`         | section name → list of `{Summary, UserSummary, Breaking}` (CHANGELOG.md uses `.Summary`; no SHA links)   |
| `Breaking`         | every record with `breaking: true`                                                                       |
| `Highlights`       | 1–3 hand-written `{Title, Body}` entries. Author with `writing-clearly-and-concisely`.                   |
| `HasFixed`         | true when `Sections.Fixed` is non-empty                                                                  |
| `Summary`          | 2–4 sentence release summary. Author with `writing-clearly-and-concisely`.                               |
| `UpgradeNotes`     | paragraph; omit when there are no migration steps.                                                       |
| `ChangelogSection` | body of the `## [<Version>] - <Date>` block in CHANGELOG.md after rotation — slurped in step 7, not authored. |

Render each template by direct text substitution — no template engine. Empty conditional blocks are removed entirely (no blank section headers).

Render `release-notes.tmpl.md` in two passes:
- Pass A (now): fill every variable except `ChangelogSection`. Leave the `{{ChangelogSection}}` placeholder literal in the buffer.
- Pass B (step 7, after rotation): replace the placeholder with the slurped CHANGELOG.md body.

### 7. Write files

```sh
printf '%s\n' "<version>" > VERSION
python3 scripts/rotate-unreleased.py "<version>" "<YYYY-MM-DD>" \
    <rendered-section-file> "<prev_tag-or-empty>"
mkdir -p docs/release-notes
# Slurp the rotated CHANGELOG.md section body (the ### subsections under
# `## [<version>] - <date>`, stopping at the next `## [` or footer link).
awk -v v="<version>" '
    $0 ~ "^## \\[" v "\\]" {grab=1; next}
    grab && /^## \[/ {exit}
    grab && /^\[[^]]+\]:[[:space:]]/ {exit}
    grab {print}
' CHANGELOG.md > /tmp/ch-section.md
# Pass B: substitute {{ChangelogSection}} in the rendered release note with
# /tmp/ch-section.md, then write docs/release-notes/<version>.md.
```

`rotate-unreleased.py` handles CHANGELOG.md surgery deterministically (creates the file on first release, otherwise rotates `[Unreleased]` and refreshes footer compare-links).

### 8. Stage and summarize

```sh
git add VERSION CHANGELOG.md docs/release-notes/<version>.md
git diff --cached --stat
```

Then print, in this order:

- Proposed version and the SemVer reasoning sentence.
- Any classification warnings (unknown prefixes, mixed signals).
- The first ~20 lines of `CHANGELOG.md` and of `docs/release-notes/<version>.md`.

### 9. Approval gate

Ask the maintainer exactly once:

> Release prep staged for v\<version\>. Commit as `chore: bump version <version>` (y/n)? Edit files first if needed; staged set will be re-read on `y`.

Wait for `y`. On `n`, stop without unstaging.

### 10. Commit

```sh
git commit -m "chore: bump version <version>"
```

Subject is exact. Body is the same `Summary` paragraph used at the top of the release note. No `Co-Authored-By` unless the maintainer set it globally.

## Prose Policy

Before writing any sentence into `CHANGELOG.md`, `docs/release-notes/<version>.md`, or the commit body: invoke the `writing-clearly-and-concisely` skill at `.agents/skills/writing-clearly-and-concisely/` and apply its rules. Use its Limited Context Strategy (subagent copyedit) when context is tight.

This is the single source of style for every release artifact this skill produces. No second style guide lives inside `creating-release`.

## Rationalizations to Reject

| Excuse                                                   | Reality                                                                                             |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| "I'll skip the `VERSION` file — the tag is the source of truth." | The pipeline triggers off `VERSION` changing. No bump, no release. Write the file.                  |
| "Leading `v` in filenames is more familiar."             | The pipeline globs `docs/release-notes/<version>.md`. Leading `v` (or any `-suffix`) breaks the glob. No `v`, no suffix. |
| "Two files — user + technical — are easier to read."      | One file. The friendly body comes first, the CHANGELOG.md slurp comes last under `## Changes`. Don't resurrect the split. |
| "`release:` (or any variant) is the conventional subject." | This repo's convention is `chore: bump version <v>` — see git log. Match it. The project does not use commit scopes, so `chore(release):` and similar are also wrong. |
| "I can fold the breaking note into Changed without the `**BREAKING:**` prefix." | The prefix is the marker readers (and the release-notes pipeline) look for. Keep the bullet under its correct Keep a Changelog section (`Changed`, `Removed`, etc.) and prefix it with `**BREAKING:**`. Do not invent a `### Breaking` section — Keep a Changelog 1.1.0 has none. The release-note's friendly top half has its own `## Important: breaking changes` block; the CHANGELOG.md trailer it copies keeps the `**BREAKING:**` prefix in place. |
| "Every `chore` and `docs` commit belongs in the changelog for completeness." | Keep a Changelog 1.1.0 is for changes users notice. A `chore` or `docs` commit ships in CHANGELOG.md only when its body proves user-visible behaviour changed — see "Chore triage" in `references/categorization.md`. Pure repo upkeep stays in `git log` alone. |
| "I'll write prose first and copyedit later if there's time." | Defer to `writing-clearly-and-concisely` at draft time, not as a backstop. Strunk's rules are non-optional. |
| "First release, so CHANGELOG.md doesn't need a `[Unreleased]` block." | Yes it does. `rotate-unreleased.py` seeds it on the first release. Future bumps depend on it.        |

## Red Flags — Stop and Reread

- Subject of the bump commit isn't exactly `chore: bump version <version>`, or the commit carries a body.
- `VERSION` file isn't in the staged set.
- A breaking change is in CHANGELOG.md without the `**BREAKING:**` prefix.
- CHANGELOG.md contains a section that isn't one of Added/Changed/Deprecated/Removed/Fixed/Security.
- The release-note `## Changes` trailer disagrees with CHANGELOG.md's `[<version>]` block — it must be a verbatim copy.
- The release-note filename starts with `v`, has a `-user`/`-technical` suffix, or you wrote two files.
- Prose was written without `writing-clearly-and-concisely` applied.
- A `chore` or `docs` commit appears in CHANGELOG.md without its body justifying a user-visible change.
- A `chore` or `docs` commit was dropped without its body being read.

Any of these → fix before the approval gate. Do not ship.

## References

- `references/categorization.md` — commit prefix → section table, authoritative.
- `references/keepachangelog.md` — Keep a Changelog 1.1.0 condensed.
- `references/semver.md` — SemVer 2.0.0 bump rules including the 0.x convention.
- `templates/changelog-section.tmpl.md` — body injected into `CHANGELOG.md`.
- `templates/release-notes.tmpl.md` — `docs/release-notes/<version>.md` (single file; friendly body + verbatim CHANGELOG.md trailer).
- `scripts/rotate-unreleased.py` — CHANGELOG.md surgery (idempotent, deterministic).
