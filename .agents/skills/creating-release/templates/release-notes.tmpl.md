{{!-- docs/release-notes/<Version>.md
     Single release-note file. Used verbatim as the GitHub release body.
     Render by direct text substitution. Empty conditional blocks are removed
     entirely — do not leave bare headings.

     Top half (Summary → Highlights → Breaking → Fixes → Upgrading) is the
     friendly announcement. Apply writing-clearly-and-concisely to {{Summary}},
     each {{.Body}}, each {{.UserSummary}}, and {{UpgradeNotes}}.

     Trailing ## Changes section is copied verbatim from the CHANGELOG.md
     release block written by rotate-unreleased.py. The skill workflow does
     the copy after rotation; do not author it here.
--}}
# What's new in codeherd v{{Version}}

{{Summary}}

{{#if Highlights}}## Highlights

{{#each Highlights}}### {{.Title}}

{{.Body}}

{{/each}}{{/if}}{{#if Breaking}}## Important: breaking changes

{{#each Breaking}}- {{.UserSummary}}
{{/each}}

{{/if}}{{#if HasFixed}}## Fixes

{{#each Sections.Fixed}}- {{.UserSummary}}
{{/each}}

{{/if}}{{#if UpgradeNotes}}## Upgrading

{{UpgradeNotes}}

{{/if}}## Changes

{{!-- Inserted by the skill workflow after rotate-unreleased.py runs:
     copy the body of the `## [<Version>] - <Date>` block from CHANGELOG.md
     (the ### Added/Changed/... subsections), then the compare link below.
--}}
{{ChangelogSection}}
{{#if PrevTag}}**Full Changelog:** https://github.com/xico42/codeherd/compare/{{PrevTag}}...v{{Version}}{{/if}}
