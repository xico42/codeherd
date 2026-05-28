# `creating-release` skill — design

**Status:** approved (brainstorming)
**Date:** 2026-05-28
**Tracks:** #13 (Create release pipeline) — skill portion only
**Out of scope:** the GitHub Actions release pipeline, target platform matrix, mise-backend artifact naming. Those are tracked separately and will be designed once the skill ships.

## 1. Goal

Build a maintainer-facing skill, `creating-release`, that takes the codeherd repo from "main has unreleased commits" to "ready-to-merge release-prep commit", semi-automatically.

The skill runs locally in a Claude Code session before the release is merged. It:

1. Reads the previous released tag and computes the commit range to release.
2. Classifies each commit into Keep a Changelog sections plus a Breaking flag.
3. Proposes the next SemVer tag from that classification.
4. Bumps the `VERSION` file at the repo root.
5. Rotates `CHANGELOG.md`'s `[Unreleased]` block into a versioned heading and refreshes the compare-link footer.
6. Generates two per-version release-notes files: `docs/release-notes/<version>-technical.md` (used later as the GitHub release body) and `docs/release-notes/<version>-user.md` (friendly announcement).
7. Stages every change, prints a summary diff, and waits for the maintainer's final approval before committing `chore: bump version <version>`.

The skill is autonomous on the happy path. It stops only for ambiguous input (no commits since last tag, dirty working tree, missing `CHANGELOG.md` on a non-first release) or to gather the maintainer's final approval before committing.

## 2. Skill home and distribution

Skills live in `.agents/skills/` so they are agent-runner-agnostic. Claude Code's local skill discovery path `.claude/skills/` is a symlink to `.agents/skills/`:

```
.claude/skills -> ../.agents/skills        # already created
.agents/skills/writing-clearly-and-concisely/   # already installed
.agents/skills/creating-release/                # this skill (to be created)
```

The symlink is committed to the repo so any clone gets the same Claude-visible layout without a setup step. Both the symlink and the `.agents/` tree are checked in.

The skill is maintainer-only: it is not packaged as a Claude Code plugin and not advertised through `.claude-plugin/marketplace.json`.

## 3. Bundled layout

```
.agents/skills/creating-release/
├── SKILL.md                          # orchestration prompt (entry point)
├── scripts/
│   ├── last-tag.sh                   # prints last released tag, or empty for v0.1.0
│   ├── commits-since.sh              # prints "%H%x09%s%x09%b%x1e" for <range>
│   ├── propose-bump.sh               # classification JSON -> next semver
│   ├── rotate-unreleased.sh          # CHANGELOG.md surgery
│   ├── write-version.sh              # bump VERSION + sanity-check
│   └── check-preconditions.sh        # dirty tree / branch / missing files
├── templates/
│   ├── changelog-section.tmpl.md     # block injected into CHANGELOG.md
│   ├── technical-notes.tmpl.md       # docs/release-notes/<v>-technical.md
│   └── user-notes.tmpl.md            # docs/release-notes/<v>-user.md
└── references/
    ├── keepachangelog.md             # condensed format rules
    ├── semver.md                     # condensed bump rules
    └── categorization.md             # commit -> section mapping table
```

Every shell script is POSIX `sh`, depends only on `git`, `awk`, `sed`, and `jq`, and uses `set -eu`. Scripts are independently runnable for testing (`bash scripts/propose-bump.sh < fixture.json`) and have no Claude-specific side effects.

## 4. Prose policy

All maintainer-facing prose the skill emits — release-notes copy, changelog entries, the final commit body, summary printed before approval — defers to the `writing-clearly-and-concisely` skill installed at `.agents/skills/writing-clearly-and-concisely/`.

`SKILL.md` must contain an explicit instruction:

> Before writing any prose into `CHANGELOG.md`, `docs/release-notes/<v>-technical.md`, `docs/release-notes/<v>-user.md`, or the final commit message body, invoke the `writing-clearly-and-concisely` skill and apply its rules. Use the Limited Context Strategy (subagent copyedit) if context is tight.

This is the single source of style for the release artifacts; no separate style guide lives inside `creating-release`.

## 5. Inputs, outputs, preconditions

### Inputs

- Current working tree at the repo root of `codeherd`.
- Optional flag the maintainer types when invoking the skill:
  - `release as patch|minor|major` — override the bump proposed from commit analysis.
  - `from <ref>` — override the lower bound of the commit range (default: last tag, or repo root for v0.1.0).

### Preconditions (`check-preconditions.sh`)

The skill aborts with a clear error if any precondition fails:

1. The working tree has no uncommitted changes (`git status --porcelain` is empty).
2. The current branch is *not* `main`. The skill is intended to run on a `release/<version>` branch cut from `main`. (Naming is a convention; not enforced.)
3. `CHANGELOG.md` exists with an `## [Unreleased]` heading, *unless* there are no tags yet (first release).
4. `git fetch --tags origin` has been run recently enough for `last-tag.sh` to be accurate. The skill performs `git fetch --tags --quiet origin` itself before reading tags.

### Outputs (file system, before commit)

- `VERSION` — single line, no leading `v`, e.g. `0.1.0\n`.
- `CHANGELOG.md` — `[Unreleased]` block rotated under `## [<version>] - YYYY-MM-DD`; a fresh empty `[Unreleased]` block is inserted at the top; footer compare-links updated.
- `docs/release-notes/<version>-technical.md` — created.
- `docs/release-notes/<version>-user.md` — created.

### Final commit

```
chore: bump version <version>

<one paragraph summary — same prose used in user-notes opening>
```

Author and trailer follow this project's commit-message conventions (the maintainer's git identity; no `Co-Authored-By` unless the maintainer enables it).

## 6. Workflow (`SKILL.md` orchestration)

The skill must execute the steps in order and never skip the final approval gate.

1. **Preconditions.** Run `scripts/check-preconditions.sh`. On non-zero exit, print its stderr verbatim and stop.
2. **Range.** Run `scripts/last-tag.sh`. If empty, this is the first release; range is the repo root..`HEAD` and proposed version is `0.1.0` (overridable).
3. **Collect commits.** Run `scripts/commits-since.sh <prev-tag>..HEAD`. The script prints one record per commit using `%x1e` (RS) as the record separator and `%x09` (TAB) between fields: `sha`, `subject`, `body`. This format survives commit messages that contain newlines.
4. **Classify.** Read `references/categorization.md`. For each commit, produce a JSON record:
   ```json
   {
     "sha": "...",
     "subject": "...",
     "section": "Added|Changed|Deprecated|Removed|Fixed|Security",
     "breaking": false,
     "user_visible": true,
     "summary": "<one-sentence rewrite, present tense, omit needless words>",
     "user_summary": "<rewrite for an end user; omitted when user_visible is false>"
   }
   ```
   `summary` is the technical/changelog phrasing (still readable, no jargon for its own sake). `user_summary` is the friendly phrasing for `<v>-user.md` and is only generated when `user_visible` is true.
   - Use the conventional-commit prefix as a strong hint (`feat → Added`, `fix → Fixed`, etc.). The mapping table in `references/categorization.md` is authoritative.
   - Mark `breaking: true` for `<type>!:` subjects, or commits whose body contains `BREAKING CHANGE:`.
   - Mark `user_visible: false` for `chore:`, `ci:`, `build:`, `test:`, `style:`, `refactor:` that have no user-observable effect. These are kept out of `<v>-user.md` but still appear in `CHANGELOG.md` and `<v>-technical.md` (under Changed if material; otherwise omitted entirely per Keep a Changelog guidance).
   - Each `summary` field is written with the `writing-clearly-and-concisely` skill applied.
5. **Propose bump.** Pipe the classification JSON into `scripts/propose-bump.sh`. The script applies SemVer rules:
   - any `breaking: true` → MAJOR (or, while `0.x`, MINOR per SemVer §4)
   - else any `Added` → MINOR
   - else any `Fixed` or `Security` → PATCH
   - else → PATCH (no-op-style releases still bump PATCH)
   First release is always `0.1.0` regardless of bump signal.
   If the maintainer passed `release as <kind>`, that overrides the script's result; the skill prints both and uses the override.
6. **Render templates.** Fill `templates/changelog-section.tmpl.md`, `templates/technical-notes.tmpl.md`, and `templates/user-notes.tmpl.md` with the classification data. Prose fields use `writing-clearly-and-concisely`. Template variables are listed in §8.
7. **Write files.**
   - `scripts/write-version.sh <version>` updates `VERSION` and asserts the new value parses as SemVer.
   - `scripts/rotate-unreleased.sh <version> <YYYY-MM-DD>` rewrites `CHANGELOG.md`: copy the existing `[Unreleased]` body, replace it with the rendered new section under `## [<version>] - <date>`, insert a fresh empty `[Unreleased]` block above, and refresh the footer compare-links (`[Unreleased]`, `[<version>]`). For the first release, footer links use the repo-root SHA as the lower bound.
   - Write `docs/release-notes/<version>-technical.md` and `docs/release-notes/<version>-user.md`. Create `docs/release-notes/` if missing.
8. **Stage and summarize.** `git add` the four touched paths. Print:
   - the proposed version and bump reason;
   - the resulting `git diff --staged --stat`;
   - the head of each new file (first ~20 lines).
9. **Approval gate.** Ask the maintainer in one message: *"Release prep staged for v<version>. Commit as `chore: bump version <version>` (y/n)? Edit anything first if needed."* Wait for explicit `y`.
10. **Commit.** On `y`, run `git commit -m "chore: bump version <version>" -m "<summary paragraph>"`. On `n`, stop without unstaging — the maintainer keeps the staged tree to edit.

## 7. Commit classification (`references/categorization.md`)

| Prefix              | Default section | Notes                                                                 |
| ------------------- | --------------- | --------------------------------------------------------------------- |
| `feat`              | Added           | `feat!` or `BREAKING CHANGE:` → mark breaking                         |
| `fix`               | Fixed           |                                                                       |
| `perf`              | Changed         | Note magnitude in summary if commit body has measurements             |
| `refactor`          | Changed         | `user_visible: false` unless body says otherwise                      |
| `revert`            | Removed         | Subject form: `revert: <original subject>` — point to the reverted SHA |
| `docs`              | omit            | Skipped unless the commit changes user-facing docs (READMEs, man-style help) — judgment call |
| `style`             | omit            |                                                                       |
| `test`              | omit            |                                                                       |
| `chore`             | omit            | Exception: `chore(deps):` bumps that affect runtime go in Changed     |
| `ci`, `build`       | omit            |                                                                       |
| `security`          | Security        | Custom prefix; reserved for security fixes                            |
| `deprecate`         | Deprecated      | Custom prefix; reserved for deprecation notices                       |
| no recognised prefix| Changed         | Skill flags these in the summary so the maintainer can re-tag          |

Rules:

- Commits with subject `chore: bump version <v>` are skipped automatically.
- In `CHANGELOG.md` (Keep a Changelog 1.1.0), breaking changes are marked by prefixing the bullet with `**BREAKING:**` and keeping it under its underlying section (`### Changed`, `### Removed`, etc.). Keep a Changelog has no separate "Breaking" section, so we do not invent one. In the two per-version note files — which are not Keep a Changelog — breaking changes still get their own top-level block (`## Breaking Changes` / `## Important: breaking changes`).
- Multiple commits that share a clear theme (same scope or same feature area) may be merged into a single bullet in `<v>-user.md`, with the technical file still listing each commit separately.

## 8. Templates

All three templates use Go `text/template`-style placeholders for clarity; the skill renders them by string substitution since there is no template engine — Claude reads the template, substitutes by hand, applies prose rules. Template variables:

| Name              | Type        | Source                                                |
| ----------------- | ----------- | ----------------------------------------------------- |
| `Version`         | string      | proposed semver, no leading `v`                        |
| `Date`            | string      | `YYYY-MM-DD`, local date                              |
| `PrevTag`         | string      | previous tag with `v` prefix, or empty for first release |
| `RepoSlug`        | string      | `xico42/codeherd` (hard-coded; this skill is repo-specific) |
| `ReleaseKind`     | string      | `Major`, `Minor`, or `Patch`, derived from the bump   |
| `Sections`        | map         | section name → list of `{Summary, UserSummary, SHA, Breaking}` records (`Sections.Fixed` etc. are addressable by section name) |
| `Breaking`        | list        | breaking-change records across all sections; each has `Summary`, `UserSummary`, `SHA` |
| `Highlights`      | list        | top 1–3 user-visible items, each `{Title, Body}`, written by Claude for `<v>-user.md` |
| `HasFixed`        | bool        | true when `Sections.Fixed` is non-empty               |
| `Summary`         | paragraph   | 2–4 sentence release summary, written for both files  |
| `UpgradeNotes`    | paragraph   | omitted when empty (no manual steps, no config changes) |

### 8.1 `templates/changelog-section.tmpl.md`

Injected into `CHANGELOG.md` under `## [<Version>] - <Date>`. Mirrors Keep a Changelog 1.1.0 exactly: one heading per non-empty section, bullet per commit, breaking changes prefixed with `**BREAKING:**`.

```markdown
## [{{Version}}] - {{Date}}

{{if Breaking}}### Breaking

{{range Breaking}}- **BREAKING:** {{.Summary}} ({{.SHA}})
{{end}}
{{end}}{{range $name, $items := Sections}}### {{$name}}

{{range $items}}- {{.Summary}} ({{.SHA}})
{{end}}
{{end}}
```

### 8.2 `templates/technical-notes.tmpl.md`

Adapted from the changelog-generator "Technical Release Notes" template. Used verbatim as the GitHub release body by the future pipeline.

```markdown
# Release v{{Version}}

**Release Date:** {{Date}}
**Type:** {{ReleaseKind}}  <!-- Major | Minor | Patch -->
**Previous:** {{PrevTag or "(initial release)"}}

## Summary

{{Summary}}

{{if Breaking}}## Breaking Changes

{{range Breaking}}- {{.Summary}} ({{.SHA}})
{{end}}
{{end}}## Changes

{{range $name, $items := Sections}}### {{$name}}

{{range $items}}- {{.Summary}} ([{{.SHA}}](https://github.com/{{RepoSlug}}/commit/{{.SHA}}))
{{end}}
{{end}}{{if UpgradeNotes}}## Upgrade Notes

{{UpgradeNotes}}
{{end}}**Full Changelog:** https://github.com/{{RepoSlug}}/compare/{{PrevTag}}...v{{Version}}
```

### 8.3 `templates/user-notes.tmpl.md`

Adapted from the changelog-generator "User-Friendly Release Notes" template. Friendly prose, no SHAs.

```markdown
# What's new in codeherd v{{Version}}

{{Summary}}

{{if Highlights}}## Highlights

{{range Highlights}}### {{.Title}}

{{.Body}}

{{end}}{{end}}{{if Breaking}}## Important: breaking changes

{{range Breaking}}- {{.UserSummary}}
{{end}}
{{end}}{{if HasFixed}}## Fixes

{{range Sections.Fixed}}- {{.UserSummary}}
{{end}}
{{end}}{{if UpgradeNotes}}## Upgrading

{{UpgradeNotes}}
{{end}}
```

`HasFixed` is true when `Sections.Fixed` is non-empty. `Highlights[].Title` and `Highlights[].Body` are authored by Claude per release, not lifted from commit subjects.

## 9. `CHANGELOG.md` invariant

At rest (between releases) the file is:

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

<!-- entries appended by maintainers during normal development -->

## [<version-N>] - YYYY-MM-DD

...

## [<version-1>] - YYYY-MM-DD

...

[Unreleased]: https://github.com/xico42/codeherd/compare/v<version-N>...HEAD
[<version-N>]: https://github.com/xico42/codeherd/compare/v<version-N-1>...v<version-N>
...
[<first-version>]: https://github.com/xico42/codeherd/releases/tag/v<first-version>
```

`rotate-unreleased.sh` is responsible for:

1. Locating the `## [Unreleased]` line.
2. Copying its body up to the next `## [` heading (or EOF).
3. Inserting `## [<new>] - <date>` immediately above the old `[Unreleased]` heading, with the body that was rendered from §8.1 (the rendered body **replaces** whatever was in `[Unreleased]`; the skill's classification is the source of truth for the release block).
4. Replacing the `[Unreleased]` body with a single placeholder comment `<!-- Add Unreleased entries here. -->`.
5. Rewriting the footer link references:
   - `[Unreleased]: ...compare/v<new>...HEAD`
   - prepending `[<new>]: ...compare/v<prev>...v<new>` (or `releases/tag/v<new>` for the first release).

For the first release there is no `[Unreleased]` block yet; the script seeds the entire `CHANGELOG.md` with the header, the rendered `[<new>]` block, an empty `[Unreleased]`, and the footer.

The script is deterministic: same inputs, identical bytes out. Trailing newline preserved.

## 10. `VERSION` file

- Single line, `<major>.<minor>.<patch>`, optional `-<prerelease>` and `+<build>` per SemVer 2.0.0.
- No leading `v`. The `v` prefix is added only for git tags and footer links.
- `write-version.sh` rejects anything that fails the SemVer regex from semver.org. The current value must be strictly less than the new value by SemVer precedence (§11).

## 11. Edge cases and failure modes

- **First release with no prior tag.** `last-tag.sh` returns empty; range becomes `<root-sha>..HEAD`. Proposed version is `0.1.0`. `CHANGELOG.md` is created from scratch. The footer link for `[0.1.0]` points to `releases/tag/v0.1.0`.
- **No commits in range.** Skill prints "no commits since v<prev>; nothing to release" and exits zero without writing anything.
- **Dirty tree.** Skill prints `git status --short` and exits non-zero before touching anything.
- **Mid-release abort.** If the maintainer answers `n` at the approval gate, files stay staged. The maintainer may unstage with `git reset` or amend before committing.
- **Re-run on the same branch.** `check-preconditions.sh` detects an already-bumped `VERSION` that equals the proposed value and exits with an explanatory message.
- **Unrecognised commit prefix.** Classified as Changed; the skill emits a warning in the summary and asks the maintainer to retag before approval.
- **`writing-clearly-and-concisely` not installed.** Skill warns once and proceeds, but the spec recommends installing it before running.

## 12. Out of scope

- The GitHub Actions release pipeline. A separate spec will cover triggering on `VERSION` change, building binaries, target matrix, and GitHub release publication.
- mise-backend artifact naming. Decided alongside the pipeline.
- Cross-platform build details.
- Publishing the skill outside this repo. It is repo-specific; `RepoSlug` is hard-coded.

## 13. Acceptance criteria

The skill is complete when:

1. Running it on a clean `release/0.1.0` branch cut from `main` produces, with no manual edits, a staged change set containing `VERSION` (`0.1.0`), a new `CHANGELOG.md`, `docs/release-notes/0.1.0-technical.md`, and `docs/release-notes/0.1.0-user.md`.
2. The maintainer can approve and the resulting commit message is `chore: bump version 0.1.0`.
3. Running the skill a second time on the same branch refuses to proceed and prints a clear reason.
4. Every script under `scripts/` runs standalone with documented inputs and exits non-zero on error.
5. Every prose fragment authored by the skill was produced with `writing-clearly-and-concisely` applied (verifiable by spot-check of the diff against Strunk's rules).
6. The symlink `.claude/skills -> ../.agents/skills` is committed and the skill is discoverable as `.claude/skills/creating-release/SKILL.md`.

## 14. References

- Keep a Changelog 1.1.0 — https://keepachangelog.com/en/1.1.0/
- Semantic Versioning 2.0.0 — https://semver.org/spec/v2.0.0.html
- changelog-generator skill (template source) — https://github.com/claude-office-skills/skills/tree/main/changelog-generator
- changelog-automation skill (template source) — https://github.com/wshobson/agents/tree/main/plugins/documentation-generation/skills/changelog-automation
- `.agents/skills/writing-clearly-and-concisely/SKILL.md` — prose authority for every artifact this skill produces.
