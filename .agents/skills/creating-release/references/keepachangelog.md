# Keep a Changelog 1.1.0 — guidelines

Source: https://keepachangelog.com/en/1.1.0/

## Guiding principles (verbatim)

- Changelogs are for humans, not machines.
- There should be an entry for every single version.
- The same types of changes should be grouped.
- Versions and sections should be linkable.
- The latest version comes first.
- The release date of each version is displayed.
- Mention whether you follow Semantic Versioning.

## Types of changes (verbatim)

- **Added** for new features.
- **Changed** for changes in existing functionality.
- **Deprecated** for soon-to-be removed features.
- **Removed** for now removed features.
- **Fixed** for any bug fixes.
- **Security** in case of vulnerabilities.

These six types are the **only** sections allowed inside a version block in `CHANGELOG.md`. Do not invent new sections. Do not split entries across custom blocks. Group by these names exactly.

## Marking breaking changes

Keep a Changelog has no separate "Breaking" section. To call out a breaking change, prefix the affected entry with `**BREAKING:**` and keep it under its correct type. Examples:

```markdown
### Changed

- **BREAKING:** rename `ch attach` to `ch session attach`; the old form prints a deprecation warning for one release.

### Removed

- **BREAKING:** drop the deprecated `--legacy-flag` argument from `ch create worktree`.
```

The per-version release note (`docs/release-notes/<version>.md`) is **not** a Keep a Changelog file in its top half; it carries a top-level `## Important: breaking changes` block to make the impact unmissable. Its trailing `## Changes` section is a verbatim copy of the CHANGELOG.md block and therefore still obeys the six-section rule.

## File shape

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

<!-- Add Unreleased entries here. -->

## [<version>] - YYYY-MM-DD

### Added
### Changed
### Deprecated
### Removed
### Fixed
### Security

[Unreleased]: https://github.com/xico42/codeherd/compare/v<version>...HEAD
[<version>]: https://github.com/xico42/codeherd/compare/v<prev>...v<version>
...
[<first-version>]: https://github.com/xico42/codeherd/releases/tag/v<first-version>
```

Omit any section that has no entries. Newest version on top. Footer compare-links keep every version linkable, satisfying the "Versions and sections should be linkable" principle.

## Style notes for entries

- Write for end users. No commit SHAs in this file.
- Present tense, third person ("renames `ch attach`...", not "we renamed").
- One bullet per logical change. Do not batch multiple unrelated changes into one bullet.
- All prose is authored with the `writing-clearly-and-concisely` skill applied.
