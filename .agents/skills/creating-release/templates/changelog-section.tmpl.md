{{!-- Body injected into CHANGELOG.md under the version heading.
     Render by direct text substitution. No template engine.
     - {{Version}}, {{Date}} are required.
     - Iterate {{Sections}} in fixed Keep a Changelog order: Added, Changed, Deprecated, Removed, Fixed, Security.
     - Omit any section that has no entries (do not print empty headings).
     - For each entry in a section, if {{.Breaking}} is true, prefix the bullet with `**BREAKING:**`.
     - Entries are human-readable prose, no commit SHAs (per Keep a Changelog).
     - Apply writing-clearly-and-concisely to every .Summary.
--}}
## [{{Version}}] - {{Date}}

### Added

- {{#if .Breaking}}**BREAKING:** {{/if}}{{.Summary}}

### Changed

- {{#if .Breaking}}**BREAKING:** {{/if}}{{.Summary}}

### Deprecated

- {{#if .Breaking}}**BREAKING:** {{/if}}{{.Summary}}

### Removed

- {{#if .Breaking}}**BREAKING:** {{/if}}{{.Summary}}

### Fixed

- {{.Summary}}

### Security

- {{.Summary}}
