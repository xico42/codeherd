# Release pipeline design

Status: approved
Date: 2026-05-28
Issue: [#13](https://github.com/xico42/codeherd/issues/13)
Related: [#15](https://github.com/xico42/codeherd/issues/15) (mise registry submission, follow-up)

## Problem

`codeherd` has no release pipeline. There is no published binary, no LICENSE file, and no automated path from a merged `chore: bump version` commit to a GitHub Release that downstream tools like [`mise`](https://mise.jdx.dev/dev-tools/backends/github.html) can consume.

The [`creating-release`](../../../.claude/skills/creating-release) skill already produces the prerequisites on the release branch: a bumped `VERSION` file, a rotated `CHANGELOG.md`, and a single `docs/release-notes/<version>.md` narrative. What is missing is the build pipeline that picks up from there.

## Goals

- Cut Apache 2.0 licensed binary releases automatically when a `VERSION` bump lands on `main`.
- Ship archives for `linux` and `darwin` on `amd64` and `arm64`, in a layout mise's GitHub backend can auto-detect.
- Sign archives so users can verify integrity.
- Catch cross-compile and packaging regressions on the pull request, before merge, not after release fires.
- Keep build commands reproducible locally — the workflow should be a thin wrapper around `make`.

## Non-goals

- BSD, Windows, and Solaris builds. Issue #13 lists these; the project depends on tmux which is not first-class on BSD/Solaris and is unavailable on Windows. Cutting an untested artifact for those platforms creates a worse user experience than not shipping at all. Revisit if real users ask.
- Replacing or extending the `creating-release` skill. This spec consumes the skill's output unchanged.
- Mise registry submission. Tracked separately in #15 — gated on at least one successful release from this pipeline.
- Goreleaser or a similar release-orchestration tool. The matrix is small enough that `make` + a GH workflow is simpler than introducing a new dependency.
- Re-running `make check` on the release commit. PR-time CI must already pass to merge; re-running on `main` wastes minutes and risks flake blocking a release.

## Files touched

| Path | Status | Purpose |
| --- | --- | --- |
| `LICENSE` | new | Apache License 2.0, full text, copyright `2026 Francisco Rodrigues` |
| `.github/workflows/release.yml` | new | Release publish workflow |
| `.github/workflows/ci.yml` | modified | New `release-build` smoke job |
| `Makefile` | modified | `release-build`, `release-archive`, `release-checksums` targets; `clean` extended |
| `README.md` | modified | License badge, mise install snippet |

## License: Apache 2.0

The CLI ships under Apache 2.0. The decision is documented here because it affects every released artifact.

- **Why not MIT:** Apache 2.0 adds an explicit patent grant and clearer contributor terms. Once outside contributions arrive — and once a company stands behind the project — the patent clause matters. Apache 2.0 is the de-facto standard for OSS CLIs that expect to grow.
- **Why not BSL or AGPL:** both target SaaS protection. The CLI runs client-side and never serves traffic; locking it down with copyleft or source-available terms scares off contributors and enterprise users without protecting any revenue. The future SaaS backend will live in a separate repo and can carry a different license (BSL is the likely candidate) without affecting the CLI.

## Makefile targets

Build logic lives in `make` so it is reproducible locally. The workflow only orchestrates.

```make
DIST_DIR        := dist
VERSION         := $(shell cat VERSION)
RELEASE_LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

release-build:
	mkdir -p $(DIST_DIR)/$(GOOS)-$(GOARCH)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -trimpath $(RELEASE_LDFLAGS) \
	  -o $(DIST_DIR)/$(GOOS)-$(GOARCH)/$(BIN_NAME) .

release-archive:
	cp LICENSE README.md $(DIST_DIR)/$(GOOS)-$(GOARCH)/
	tar -C $(DIST_DIR)/$(GOOS)-$(GOARCH) -czf \
	  $(DIST_DIR)/$(BIN_NAME)-$(VERSION)-$(GOOS)-$(GOARCH).tar.gz \
	  $(BIN_NAME) LICENSE README.md

release-checksums:
	cd $(DIST_DIR) && sha256sum *.tar.gz > checksums.txt
```

**Rationale:**

- `CGO_ENABLED=0` → static binaries that run across any libc. No glibc-version surprises across distros.
- `-trimpath` → reproducible builds. Embedded paths do not leak `/home/runner/...` from the build agent.
- A dedicated `RELEASE_LDFLAGS` reads the version from `VERSION` instead of from `git describe`. The release workflow creates the `v<version>` tag *after* the build step, so at build time `git describe --tags --always` would return a short SHA and `ch --version` would print the SHA rather than the semantic version. Sourcing from the `VERSION` file is authoritative and matches what the archive filename advertises. The default development `LDFLAGS` (used by `make build` / `make install`) is unchanged.
- Archive root is flat: `ch`, `LICENSE`, `README.md` side-by-side, no nested directory. Mise unpacks and finds `ch` immediately.
- Per-target staging dir (`dist/<goos>-<goarch>/`) keeps copies of `LICENSE` / `README.md` from racing across matrix-style sequential calls.

`make clean` extends to remove `dist/`.

**Local repro:**

```sh
make release-build   GOOS=linux GOARCH=amd64
make release-archive GOOS=linux GOARCH=amd64
```

## CI smoke job (PR side)

Add to `.github/workflows/ci.yml`:

```yaml
release-build:
  name: Release build smoke (${{ matrix.goos }}/${{ matrix.goarch }})
  runs-on: ubuntu-latest
  strategy:
    fail-fast: false
    matrix:
      goos: [linux, darwin]
      goarch: [amd64, arm64]
  steps:
    - uses: actions/checkout@v5
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
    - run: make release-build GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }}
    - run: make release-archive GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }}
    - name: Verify archive contents
      run: |
        tar -tzf dist/ch-$(cat VERSION)-${{ matrix.goos }}-${{ matrix.goarch }}.tar.gz \
          | sort | diff - <(printf "LICENSE\nREADME.md\nch\n")
```

**Properties:**

- Runs on every PR alongside the existing `unit`, `integration`, `lint` jobs. Blocks merge if cross-compile or archive packaging breaks.
- Four matrix cells in parallel, ~30s each → ~30s wall clock added.
- `fail-fast: false`: cross-compile breakage is often platform-specific (a darwin-only build tag, an arm64 issue). Surfacing all failing platforms in one PR cycle is worth the small CI cost on a four-cell matrix.
- Archive content verification catches "forgot to include LICENSE" regressions before they hit a release.
- Does NOT publish, sign, or tag. Those steps only matter for the released artifact.

## Release workflow

`.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    branches: [main]
    paths: [VERSION]

permissions:
  contents: write
  id-token: write

concurrency:
  group: release
  cancel-in-progress: false

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5

      - name: Read VERSION
        id: read
        run: echo "version=$(cat VERSION)" >> "$GITHUB_OUTPUT"

      - name: Skip if tag exists
        id: check
        run: |
          if git ls-remote --exit-code --tags origin "refs/tags/v${{ steps.read.outputs.version }}"; then
            echo "Tag v${{ steps.read.outputs.version }} already exists, skipping"
            echo "skip=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Verify release notes exist
        if: steps.check.outputs.skip != 'true'
        run: test -f docs/release-notes/${{ steps.read.outputs.version }}.md

      - uses: actions/setup-go@v5
        if: steps.check.outputs.skip != 'true'
        with: { go-version-file: go.mod }

      - name: Build + archive all targets
        if: steps.check.outputs.skip != 'true'
        run: |
          for goos in linux darwin; do
            for goarch in amd64 arm64; do
              make release-build   GOOS=$goos GOARCH=$goarch
              make release-archive GOOS=$goos GOARCH=$goarch
            done
          done

      - name: Checksums
        if: steps.check.outputs.skip != 'true'
        run: make release-checksums

      - uses: sigstore/cosign-installer@v3
        if: steps.check.outputs.skip != 'true'

      - name: Sign archives + checksums
        if: steps.check.outputs.skip != 'true'
        run: |
          cd dist
          for f in *.tar.gz checksums.txt; do
            cosign sign-blob --yes \
              --output-signature "$f.sig" \
              --output-certificate "$f.pem" "$f"
          done

      - name: Tag + create release
        if: steps.check.outputs.skip != 'true'
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          V=${{ steps.read.outputs.version }}
          git tag "v$V"
          git push origin "v$V"
          gh release create "v$V" \
            --title "v$V" \
            --notes-file "docs/release-notes/$V.md" \
            dist/*.tar.gz dist/*.sig dist/*.pem dist/checksums.txt
```

### Trigger

`push` to `main` filtered to changes touching `VERSION`. The skill workflow is:

1. Run `creating-release` skill on a release branch → bumps `VERSION`, rotates `CHANGELOG.md`, writes `docs/release-notes/<version>.md`, commits `chore: bump version <version>`.
2. Open PR → CI (including the new smoke job) must pass.
3. Merge → release workflow fires on `main`.

No tag push trigger. No `workflow_dispatch`. The skill commit is the single source of truth.

### Idempotency

The "Skip if tag exists" step lets the workflow no-op when re-run on a `VERSION` value that has already shipped. Concrete scenarios it covers:

- A force-push or rebase that rewrites the `VERSION` commit without changing its content.
- An unrelated commit accidentally rewriting the same `VERSION` value (e.g. a revert).
- A manual re-run from the Actions UI.

The check uses `git ls-remote` rather than the local tag list to be authoritative against the remote.

Every subsequent step gates on `steps.check.outputs.skip != 'true'`. The repetition is verbose but is the standard GitHub Actions idiom — there is no clean "early success exit" for a job.

### Pre-flight: release notes file

Before any expensive setup (`setup-go`, build matrix, cosign), the workflow verifies `docs/release-notes/<version>.md` exists. If the skill ran correctly the file is there; if a release commit got hand-edited and the file was lost, fail fast.

### Build phase

A single job runs the four `goos × goarch` combinations sequentially in one shell loop. Total wall clock is roughly 2 minutes — Go cross-compile is seconds per target. A matrix would parallelize to ~30 seconds but at the cost of an `actions/upload-artifact` + `actions/download-artifact` dance into a separate publish job. For a release that fires roughly weekly, the simpler shape wins.

A failing target halts the whole release. This is wanted: you cannot publish a partial release anyway.

### Checksums and signing

`make release-checksums` produces a single `checksums.txt` with sha256 sums for every archive.

[Sigstore cosign keyless signing](https://docs.sigstore.dev/cosign/signing/overview/) signs each archive and the checksums file. Keyless means signing happens via the workflow's OIDC token — no signing keys to manage, no secrets to rotate. Output per file: `<file>.sig` (signature) and `<file>.pem` (certificate). Users verify with:

```sh
cosign verify-blob \
  --certificate ch-0.1.0-linux-amd64.tar.gz.pem \
  --signature   ch-0.1.0-linux-amd64.tar.gz.sig \
  --certificate-identity-regexp 'https://github.com/xico42/codeherd' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ch-0.1.0-linux-amd64.tar.gz
```

### Tag and release creation

The workflow creates and pushes the `v<version>` tag, then `gh release create` uploads:

- Four archives: `ch-<version>-{linux,darwin}-{amd64,arm64}.tar.gz`.
- Eight signature files: `.sig` and `.pem` per archive.
- `checksums.txt` plus its `.sig` and `.pem`.

Release notes body comes verbatim from `docs/release-notes/<version>.md` via `--notes-file`. No body editing, no concatenation with the CHANGELOG section — single source of truth.

### Concurrency

`concurrency.group: release` with `cancel-in-progress: false` serializes simultaneous VERSION pushes. The second run waits for the first to finish, then either no-ops via the tag-exists check or proceeds for a different version. Prevents race conditions on tag creation when two release commits land back-to-back.

### Failure recovery

If a release fails partway:

- Before the tag step → safe, no state changed, fix and retrigger.
- After tag push but before `gh release create` succeeds → the tag exists with no release. Delete it manually (`git push --delete origin v<version>` plus `git tag -d v<version>` locally), fix the underlying issue, retrigger.
- After `gh release create` → the release exists, possibly with incomplete assets. Delete the release + tag manually, retrigger.

No automatic rollback. Releases are rare; manual recovery is fine.

## Mise compatibility

Asset filenames `ch-<version>-{linux,darwin}-{amd64,arm64}.tar.gz` match [mise's GitHub backend autodetection heuristics](https://mise.jdx.dev/dev-tools/backends/github.html): the OS substring (`linux` / `darwin`), the arch substring (`amd64` / `arm64`), and the `.tar.gz` extension all hit. The `v<version>` tag prefix is the documented default.

Direct install works without a registry entry once the first release ships:

```sh
mise use github:xico42/codeherd@0.1.0
```

Mise registry submission is tracked in #15 and gated on a successful first release.

## README additions

```md
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

## Installation

### Via mise

mise use github:xico42/codeherd@latest

### Manual

Download the appropriate archive from the [latest release](https://github.com/xico42/codeherd/releases/latest),
extract, and place `ch` on your PATH.
```

Once #15 lands, swap the mise snippet to `mise use codeherd@latest`.

## Acceptance criteria

- `LICENSE` (Apache 2.0) is at the repo root.
- A PR that breaks `make release-build` for any of the four targets fails CI before merge.
- Merging a `chore: bump version <version>` commit to `main` produces, without manual intervention:
    - A `v<version>` git tag.
    - A GitHub release titled `v<version>` whose body is `docs/release-notes/<version>.md`.
    - Four `ch-<version>-<goos>-<goarch>.tar.gz` archives attached.
    - A `checksums.txt` attached.
    - A `.sig` + `.pem` for every archive and for `checksums.txt`.
- Re-running the release workflow against a `VERSION` whose tag already exists exits successfully without creating a duplicate release.
- `mise use github:xico42/codeherd@<version>` installs the `ch` binary on linux-amd64, linux-arm64, darwin-amd64, and darwin-arm64.
- A `ch` binary extracted from any released archive prints the matching semantic version when invoked as `ch --version` (no embedded SHA leak).

## Out of scope, deferred follow-ups

- Mise official registry entry — #15.
- BSD / Solaris / Windows binaries — revisit when a user asks.
- SBOM generation, SLSA provenance attestation — cosign keyless is the practical floor for v0.x; layer on top later if supply-chain attestation becomes a requirement.
- Container image (`ghcr.io/xico42/codeherd`) — only useful once a SaaS backend exists.
