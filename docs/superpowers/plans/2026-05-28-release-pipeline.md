# Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up an automated, mise-compatible release pipeline that publishes Apache-2.0 licensed, sigstore-signed `codeherd` binaries for linux/darwin × amd64/arm64 whenever the `VERSION` file changes on `main`, and prove the build steps on every PR before merge.

**Architecture:** A `LICENSE` file plus three new `make` targets (`release-build`, `release-archive`, `release-checksums`) own all build logic. The existing PR-time `ci.yml` gains a matrix smoke job that calls those targets to catch cross-compile regressions early. A new `release.yml` workflow fires on `main` when `VERSION` is touched: it reads the version, no-ops if the tag already exists, runs the four builds sequentially in one job, signs everything via cosign keyless OIDC, then creates the tag and the GitHub release with the matching `docs/release-notes/<version>.md` as the body.

**Tech Stack:** GitHub Actions (`actions/checkout@v5`, `actions/setup-go@v5`, `sigstore/cosign-installer@v3`), Go toolchain, `make`, `tar`, `sha256sum`, `cosign`, `gh` CLI. Apache License 2.0.

**Spec:** `docs/superpowers/specs/2026-05-28-release-pipeline-design.md`

---

## File Structure

| Path | Status | Purpose |
| --- | --- | --- |
| `LICENSE` | new | Apache 2.0 boilerplate, copyright `2026 Francisco Rodrigues` |
| `Makefile` | modified | Adds `release-build`, `release-archive`, `release-checksums` targets; extends `clean`; adds them to `.PHONY` |
| `.github/workflows/ci.yml` | modified | New `release-build` job (matrix smoke) appended after `lint` |
| `.github/workflows/release.yml` | new | Single-job release publish triggered by `VERSION` change on `main` |
| `README.md` | modified | Apache 2.0 badge near the top; mise + manual install snippets in the existing Install section |

---

## Task 1: Add LICENSE file

**Files:**
- Create: `LICENSE`

- [ ] **Step 1: Write the LICENSE file with full Apache 2.0 text**

Create `LICENSE` with the exact contents below.

```
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for describing the origin of the Work and
      reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Support. While redistributing the Work or
      Derivative Works thereof, You may choose to offer, and charge a
      fee for, acceptance of support, warranty, indemnity, or other
      liability obligations and/or rights consistent with this License.
      However, in accepting such obligations, You may act only on Your
      own behalf and on Your sole responsibility, not on behalf of any
      other Contributor, and only if You agree to indemnify, defend,
      and hold each Contributor harmless for any liability incurred by,
      or claims asserted against, such Contributor by reason of your
      accepting any such warranty or support.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright 2026 Francisco Rodrigues

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
   implied. See the License for the specific language governing
   permissions and limitations under the License.
```

- [ ] **Step 2: Verify file is present and copyright line is correct**

Run: `head -5 LICENSE && echo "---" && grep -n "Copyright 2026" LICENSE`
Expected: first lines show "Apache License / Version 2.0..."; grep prints exactly one line: `199:   Copyright 2026 Francisco Rodrigues` (line number may differ if the boilerplate spacing shifts; the important part is exactly one match).

- [ ] **Step 3: Commit**

```bash
git add LICENSE
git commit -m "chore: add Apache 2.0 LICENSE"
```

---

## Task 2: Add release Makefile targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Read the current Makefile**

Run: `cat Makefile`
Expected: top of file shows `BIN_NAME := ch`, `LDFLAGS := -ldflags "-s -w -X main.version=$(shell git describe ...)"`, `.PHONY: build install test test-integration coverage lint clean deps setup check vendor tools`.

- [ ] **Step 2: Add DIST_DIR, VERSION, and RELEASE_LDFLAGS variables under the existing top-of-file vars**

Modify the top of `Makefile`. Replace lines 1–4 (`BIN_NAME ... COVERAGE_THRESHOLD := 80`) with:

```make
BIN_NAME        := ch
INSTALL         := $(HOME)/.local/bin/$(BIN_NAME)
LDFLAGS         := -ldflags "-s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"
DIST_DIR        := dist
VERSION         := $(shell cat VERSION)
RELEASE_LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
COVERAGE_THRESHOLD := 80
```

- [ ] **Step 3: Extend `.PHONY` to include the new targets**

Replace line 6 of `Makefile` (`.PHONY: build install test test-integration coverage lint clean deps setup check vendor tools`) with:

```make
.PHONY: build install test test-integration coverage lint clean deps setup check vendor tools release-build release-archive release-checksums
```

- [ ] **Step 4: Add the three release targets after the existing `format` target**

Append these targets to `Makefile`, after the `format` block (currently ending at line 46 `gofmt -s -w .`). Insert them before `check:`:

```make
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

- [ ] **Step 5: Extend `clean` to remove `dist/`**

Replace the `clean` block (currently lines 42–43):

```make
clean:
	rm -f $(BIN_NAME) coverage.out
```

with:

```make
clean:
	rm -rf $(BIN_NAME) coverage.out $(DIST_DIR)
```

- [ ] **Step 6: Sanity-check the modified Makefile parses**

Run: `make -n release-build GOOS=linux GOARCH=amd64`
Expected: prints the planned commands (`mkdir -p dist/linux-amd64`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o dist/linux-amd64/ch .`) without error. The `$(VERSION)` substitution must show `0.1.0` (current VERSION contents), not `$(VERSION)` literal.

- [ ] **Step 7: Commit**

```bash
git add Makefile
git commit -m "feat(make): add release-build, release-archive, release-checksums targets"
```

---

## Task 3: Verify Makefile targets end-to-end locally

This task is verification-only: it proves Task 2's targets actually produce a valid archive before any CI/workflow code references them.

**Files:** none modified.

- [ ] **Step 1: Clean state**

Run: `make clean`
Expected: `dist/`, `ch`, and `coverage.out` removed; command exits 0.

- [ ] **Step 2: Build and archive for linux/amd64**

Run:
```bash
make release-build   GOOS=linux GOARCH=amd64
make release-archive GOOS=linux GOARCH=amd64
```
Expected: `dist/linux-amd64/ch` exists (~9 MB), and `dist/ch-0.1.0-linux-amd64.tar.gz` exists.

- [ ] **Step 3: Verify archive content layout**

Run: `tar -tzf dist/ch-0.1.0-linux-amd64.tar.gz | sort`
Expected output (exactly three lines):
```
LICENSE
README.md
ch
```

- [ ] **Step 4: Verify the embedded version string**

Run:
```bash
mkdir -p /tmp/release-verify
tar -xzf dist/ch-0.1.0-linux-amd64.tar.gz -C /tmp/release-verify
/tmp/release-verify/ch --version
```
Expected: output contains `0.1.0` (not a git SHA, not `dev`). The exact format depends on how `main.version` is printed (`ch --version` exists per the `ch` CLI surface).

- [ ] **Step 5: Run all four archs to confirm the matrix builds**

Run:
```bash
make clean
for goos in linux darwin; do
  for goarch in amd64 arm64; do
    make release-build   GOOS=$goos GOARCH=$goarch
    make release-archive GOOS=$goos GOARCH=$goarch
  done
done
make release-checksums
ls dist/
cat dist/checksums.txt
```
Expected: `ls dist/` shows four `.tar.gz` files plus `checksums.txt`; the checksums file lists exactly four lines, one per archive, each with a 64-hex sha256.

- [ ] **Step 6: Clean up**

Run: `make clean`
Expected: `dist/` removed.

- [ ] **Step 7: No commit (verification only)**

Nothing to commit. Move on if all steps above passed.

---

## Task 4: Add CI smoke job for release build

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Append the `release-build` job to `ci.yml`**

Append this block to the end of `.github/workflows/ci.yml` (after the existing `lint:` job, preserving the file's final newline):

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

- [ ] **Step 2: YAML syntax sanity-check**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: prints `OK`. If Python is unavailable, fall back to `go tool golangci-lint --help >/dev/null` (no-op proxy) and visually inspect indentation — the new job must align with the existing `unit:`, `integration:`, `lint:` jobs (4-space indent under `jobs:`).

- [ ] **Step 3: Optional — run actionlint if available**

Run: `actionlint .github/workflows/ci.yml || echo "actionlint not installed, skipping"`
Expected: either `actionlint not installed, skipping`, or no findings reported.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add release-build smoke job to PR pipeline"
```

---

## Task 5: Add release publish workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write the release workflow**

Create `.github/workflows/release.yml` with this content:

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
        with:
          go-version-file: go.mod

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

- [ ] **Step 2: YAML syntax sanity-check**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo OK`
Expected: prints `OK`.

- [ ] **Step 3: Optional — run actionlint**

Run: `actionlint .github/workflows/release.yml || echo "actionlint not installed, skipping"`
Expected: either `actionlint not installed, skipping`, or no findings reported.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add release workflow triggered by VERSION change"
```

---

## Task 6: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add Apache 2.0 badge under the title**

Modify `README.md` to insert a license badge immediately after the title line. Replace lines 1–2 (`# codeherd\n\n`) with:

```markdown
# codeherd

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

```

- [ ] **Step 2: Replace the existing Install section with mise + manual options**

Locate the `## Install` section (currently around lines 274–280, content: `Requires Go 1.22+, git, and tmux.` followed by the `make install` snippet). Replace the whole `## Install` section (from `## Install` through the closing of the `make install` code block) with:

```markdown
## Install

### Via mise

```bash
mise use github:xico42/codeherd@latest
```

Once codeherd lands in the [mise official registry](https://github.com/jdx/mise/tree/main/registry), this becomes `mise use codeherd@latest`.

### Manual

Download the appropriate archive from the [latest release](https://github.com/xico42/codeherd/releases/latest), extract, and place `ch` on your `PATH`. Each release ships archives for `linux` and `darwin` on `amd64` and `arm64`, with sigstore signatures and a `checksums.txt`.

### From source

Requires Go 1.22+, git, and tmux.

```bash
make install    # builds and installs to ~/.local/bin/ch
```
```

- [ ] **Step 3: Verify the changes render reasonably**

Run: `head -5 README.md && echo "---" && sed -n '/^## Install/,/^## Shell Completion/p' README.md`
Expected: title block shows the badge, and the Install section shows three subheadings in order: `### Via mise`, `### Manual`, `### From source`, with the existing `## Shell Completion` heading following immediately after.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(readme): add license badge and install instructions for mise + binaries"
```

---

## Task 7: Final end-to-end local verification

This task is verification-only: re-runs the full build + checksum + (mock) sign sequence locally to prove the assembled pipeline pieces still cooperate after all edits.

**Files:** none modified.

- [ ] **Step 1: Clean state and rebuild every archive**

Run:
```bash
make clean
for goos in linux darwin; do
  for goarch in amd64 arm64; do
    make release-build   GOOS=$goos GOARCH=$goarch
    make release-archive GOOS=$goos GOARCH=$goarch
  done
done
make release-checksums
```
Expected: command sequence completes; `dist/` contains four `.tar.gz` archives plus `checksums.txt`.

- [ ] **Step 2: Verify every archive contains exactly LICENSE + README.md + ch**

Run:
```bash
for f in dist/*.tar.gz; do
  echo "== $f =="
  tar -tzf "$f" | sort | diff - <(printf "LICENSE\nREADME.md\nch\n") && echo "OK"
done
```
Expected: each archive prints `OK`; no `diff` output.

- [ ] **Step 3: Verify checksums file is well-formed**

Run: `cd dist && sha256sum -c checksums.txt && cd ..`
Expected: each archive prints `OK`; command exits 0.

- [ ] **Step 4: Verify embedded version on every binary**

Run:
```bash
for goos in linux darwin; do
  for goarch in amd64 arm64; do
    tmp=$(mktemp -d)
    tar -xzf dist/ch-0.1.0-$goos-$goarch.tar.gz -C "$tmp"
    if [ "$goos-$goarch" = "linux-amd64" ]; then
      "$tmp/ch" --version
    else
      # Cannot exec other arches on this host; just check the binary is non-empty
      test -s "$tmp/ch" && echo "$goos-$goarch: binary present"
    fi
    rm -rf "$tmp"
  done
done
```
Expected: the linux-amd64 binary prints a version line containing `0.1.0`; the other three print `<goos>-<goarch>: binary present`.

- [ ] **Step 5: Confirm workflows pass static checks**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); yaml.safe_load(open('.github/workflows/release.yml'))" && echo "yaml OK"
```
Expected: prints `yaml OK`.

- [ ] **Step 6: Clean up**

Run: `make clean`
Expected: `dist/` removed.

- [ ] **Step 7: Push the branch and open a PR**

Run:
```bash
git push -u origin chore/release-skill   # or whatever branch you are on
gh pr create --fill --base main
```
Expected: PR URL printed. Confirm in the PR's Checks tab that the new `Release build smoke` matrix runs four cells (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) and all pass alongside the existing `unit`, `integration`, `lint` jobs. Merge once green.

- [ ] **Step 8: Observe the release workflow firing**

After merge, watch the Actions tab for the `Release` workflow. Expected: it triggers automatically because the merge commit touches `VERSION`. Wait for completion (~90s), then verify:

```bash
gh release view v0.1.0
gh release view v0.1.0 --json assets --jq '.assets[].name' | sort
```
Expected: release body matches `docs/release-notes/0.1.0.md` verbatim; assets list contains exactly:
```
ch-0.1.0-darwin-amd64.tar.gz
ch-0.1.0-darwin-amd64.tar.gz.pem
ch-0.1.0-darwin-amd64.tar.gz.sig
ch-0.1.0-darwin-arm64.tar.gz
ch-0.1.0-darwin-arm64.tar.gz.pem
ch-0.1.0-darwin-arm64.tar.gz.sig
ch-0.1.0-linux-amd64.tar.gz
ch-0.1.0-linux-amd64.tar.gz.pem
ch-0.1.0-linux-amd64.tar.gz.sig
ch-0.1.0-linux-arm64.tar.gz
ch-0.1.0-linux-arm64.tar.gz.pem
ch-0.1.0-linux-arm64.tar.gz.sig
checksums.txt
checksums.txt.pem
checksums.txt.sig
```

If the release fails partway:
- Tag exists but release does not → `git push --delete origin v0.1.0 && git tag -d v0.1.0`, fix the failing step, re-trigger the workflow by touching `VERSION` again (whitespace is fine — the next run with a present tag would no-op, so the tag must be deleted first).
- Release exists with missing assets → `gh release delete v0.1.0 -y`, then delete the tag as above, then re-trigger.

- [ ] **Step 9: Smoke-test mise install (optional, off-tree)**

Run on a clean shell (after release publishes):
```bash
mise use github:xico42/codeherd@0.1.0
ch --version
```
Expected: mise downloads the correct archive for the host platform; `ch --version` prints `0.1.0`. This validates the asset filename pattern is mise-compatible, which unblocks issue #15 (registry submission).

- [ ] **Step 10: Update the auto-memory if anything surprising surfaced**

If the release workflow needed adjustments not foreseen in the spec (e.g. cosign step required an extra flag on this repo, or `git push origin v$V` needed a different ref format), capture the surprise in a feedback or project memory entry so future release pipeline work benefits.

---

## Notes for the executing engineer

- **Reproducibility:** every `release-*` target works locally with the same inputs CI uses. If the workflow fails, reproduce locally with the same `GOOS`/`GOARCH` first — only investigate GitHub-specific causes if local repro succeeds.
- **Cosign keyless requires `id-token: write`:** already set in `release.yml`. If you ever fork the workflow to a private repo without OIDC enabled, signing will fail; remove the cosign steps or wire up a real key.
- **`gh release create` needs `GH_TOKEN`:** the workflow passes `secrets.GITHUB_TOKEN` via env. No extra setup needed.
- **VERSION-bump trigger and `chore: bump version`:** the `creating-release` skill commits this message. Any other commit that touches `VERSION` will also fire the release workflow — that is by design; the tag-exists guard makes it safe.
- **Idempotency manual test:** to confirm the guard works, push a no-op whitespace change to `VERSION` after a successful release. The workflow should run and exit cleanly without creating a duplicate release. Skip this test if you trust the spec.
- **Adjusting copyright holder:** the LICENSE bottom block reads `Copyright 2026 Francisco Rodrigues`. If the user prefers a different attribution (e.g. organisation name), change that single line and re-commit before opening the PR.
