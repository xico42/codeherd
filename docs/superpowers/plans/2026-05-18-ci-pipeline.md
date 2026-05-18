# CI Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions CI pipeline that runs unit tests (with 80% coverage gate), integration tests, and lint as three parallel jobs on every pull request, with all CI commands mapping 1:1 to existing Makefile targets.

**Architecture:** Single workflow file `.github/workflows/ci.yml` triggered on `pull_request`. Three independent jobs (no `needs:`) each invoke a Makefile target. `golangci-lint` is pinned via Go 1.24+'s `tool` directive in `go.mod` so the same version runs locally and in CI. `make setup` expands to cover deps + vendor + tool install + full check, making first-time setup one command.

**Tech Stack:** GitHub Actions, `actions/checkout@v5`, `actions/setup-go@v5`, Go 1.26, `go tool` directive, golangci-lint v2, tmux, GNU Make.

**Spec:** `docs/superpowers/specs/2026-05-18-ci-pipeline-design.md` (commit `7e2dee1`).

**Pre-existing state worth knowing:**
- `.golangci.yml` already exists with `version: "2"` — compatible with the linter version we'll pin.
- `vendor/` is already in `.gitignore` — no `.gitignore` change required.
- `go.mod` has `go 1.26`, no `toolchain` directive (intentional per spec).
- `Makefile` has `coverage`, `test-integration`, `lint`, `check`, `setup`, `deps`, `build` targets. We will edit `lint` and `setup`, and add `vendor` and `tools`.

---

## File Structure

| Path | Action | Responsibility |
|---|---|---|
| `go.mod` | modify | Add `tool` directive for `golangci-lint`. |
| `go.sum` | modify (auto) | Hashes for linter transitive deps. |
| `Makefile` | modify | Switch `lint` to `go tool`; expand `setup`; add `vendor`/`tools` targets. |
| `.github/workflows/ci.yml` | create | Pipeline: unit + integration + lint jobs in parallel. |

No new Go source files. No deletions.

---

## Task 1: Pin `golangci-lint` via `go.mod` tool directive

**Files:**
- Modify: `go.mod` (adds `tool` block)
- Modify: `go.sum` (regenerated)
- Modify: `Makefile:33-34` (the `lint` target)

This task makes `make lint` use the version pinned in `go.mod` instead of whatever `golangci-lint` happens to be on PATH. After this task, contributors do not need to install `golangci-lint` separately.

- [ ] **Step 1: Capture current lint baseline**

Run: `make lint`

Expected: one of:
- Succeeds with no output and exit 0 (clean). Proceed.
- Fails because `golangci-lint` isn't on PATH (`make: golangci-lint: No such file or directory`). Proceed — this is exactly the problem the task fixes.
- Succeeds but reports lint violations. Stop and fix violations first by editing offending code or adjusting `.golangci.yml`, commit those fixes separately, then re-run `make lint` until it is clean. Do **not** start Step 2 until `make lint` exits 0.

The reason: this task must not silently change which violations are reported. Establishing a clean baseline first means any new failures after the switch are caused by the version change, not pre-existing drift.

- [ ] **Step 2: Add the tool directive**

Run: `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`

Expected output (example, version will vary):

```
go: added tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint v2.x.y
```

This command will:
- Add a `tool` line (or `tool (...)` block) to `go.mod`.
- Add the linter and its transitive deps to `go.sum`.
- Mark the linter's direct module as a normal `require` with `// indirect` (Go module mechanics — leave it).

- [ ] **Step 3: Tidy and verify the modules graph**

Run: `go mod tidy`

Expected: no error output. `go.mod` should now contain a line like:

```
tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint
```

Inspect with: `grep -A1 '^tool' go.mod`

Expected: shows the tool line (or `tool (` block followed by the linter path).

- [ ] **Step 4: Confirm the linter runs via `go tool`**

Run: `go tool golangci-lint --version`

Expected: prints a version string like `golangci-lint has version 2.x.y built ...`. First run may take ~30–60s as Go compiles the linter from source into the build cache. Subsequent runs are fast.

- [ ] **Step 5: Update the Makefile `lint` target**

Edit `Makefile`. Find lines 33-34:

```
lint:
	golangci-lint run ./...
```

Replace with:

```
lint:
	go tool golangci-lint run ./...
```

- [ ] **Step 6: Verify `make lint` works via `go tool`**

Run: `make lint`

Expected: exits 0 with no violations (since we established the clean baseline in Step 1, and the linter version may differ slightly but `.golangci.yml` is `version: "2"`, so the same ruleset applies). If new violations appear due to a minor-version bump in the linter, fix the code or adjust `.golangci.yml` in this same task.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum Makefile
git commit -m "$(cat <<'EOF'
build: pin golangci-lint via go.mod tool directive

Replace the implicit PATH lookup in `make lint` with `go tool
golangci-lint`, pinning the version in go.mod/go.sum. Contributors no
longer need to install golangci-lint separately.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add `vendor` and `tools` Makefile targets; expand `setup`

**Files:**
- Modify: `Makefile` (the `.PHONY` line, the `setup` target, plus two new targets)

This task makes `make setup` cover deps + local vendoring + tool installation + full check, so a fresh clone is one command away from a working dev environment. `vendor/` is already gitignored, so this is Makefile-only work.

- [ ] **Step 1: Update the `.PHONY` line**

Edit `Makefile` line 6. Current:

```
.PHONY: build install test test-integration coverage lint clean deps setup check
```

Replace with:

```
.PHONY: build install test test-integration coverage lint clean deps setup check vendor tools
```

- [ ] **Step 2: Add the `vendor` target**

Edit `Makefile`. After the `lint:` target block (around line 34), before `clean:`, insert:

```makefile

vendor:
	go mod vendor
```

(One blank line above, no blank line below `vendor:` body — match the surrounding style.)

- [ ] **Step 3: Add the `tools` target**

Immediately after the new `vendor` target, insert:

```makefile

tools:
	go install tool
```

- [ ] **Step 4: Update the `setup` target**

Edit `Makefile`. Find:

```
setup: deps check
	@echo "Setup complete"
```

Replace with:

```
setup: deps vendor tools check
	@echo "Setup complete"
```

- [ ] **Step 5: Verify the Makefile parses and targets resolve**

Run: `make -n setup`

Expected: prints the dry-run command sequence for `deps`, then `vendor`, then `tools`, then `check` (which itself expands to `coverage`, `test-integration`, `lint`, `build`), ending with `echo "Setup complete"`. No errors.

- [ ] **Step 6: Run `make vendor` standalone to verify it works**

Run: `make vendor`

Expected: `go mod vendor` runs and creates a `vendor/` directory. `ls vendor/` should show `modules.txt` plus subdirectories per module. Since `vendor/` is already gitignored, `git status` should still show a clean tree (or only the in-progress Makefile change).

- [ ] **Step 7: Run `make tools` standalone to verify it works**

Run: `make tools`

Expected: `go install tool` resolves the linter from `go.mod`'s `tool` block and installs it to `$GOBIN` (typically `$HOME/go/bin`). On success, there is no output; verify with:

```bash
ls "$(go env GOBIN 2>/dev/null || echo $HOME/go/bin)/golangci-lint"
```

Expected: file exists.

- [ ] **Step 8: Run `make setup` end-to-end**

Run: `make setup`

Expected: all four phases (`deps`, `vendor`, `tools`, `check`) succeed in order. `check` runs `coverage` (80% gate), `test-integration`, `lint`, `build`. Final line: `Setup complete`.

If `make setup` fails because of a non-Makefile issue (e.g. integration test failure, coverage drop), stop and investigate — that is a real bug surfaced by the new full-flow target, not a plan issue.

- [ ] **Step 9: Commit**

```bash
git add Makefile
git commit -m "$(cat <<'EOF'
build: expand `make setup` to cover vendor and tool install

Add `vendor` (go mod vendor) and `tools` (go install tool) targets and
chain them into `setup` so a fresh clone reaches a working dev
environment in one command. vendor/ remains gitignored — local
convenience only.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add the GitHub Actions CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

This task adds the pipeline itself: three parallel jobs invoking `make coverage`, `make test-integration`, and `make lint`.

- [ ] **Step 1: Create the workflow directory**

Run: `mkdir -p .github/workflows`

Expected: directory exists. (`.github/` is new; no prior workflows.)

- [ ] **Step 2: Create the workflow file**

Create `.github/workflows/ci.yml` with the following exact content:

```yaml
name: CI

on:
  pull_request:

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  unit:
    name: Unit tests + coverage
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: make coverage

  integration:
    name: Integration tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install tmux
        run: sudo apt-get update && sudo apt-get install -y tmux
      - run: make test-integration

  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: make lint
```

- [ ] **Step 3: Lint the YAML locally**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"`

Expected: no output, exit 0. (Confirms parseable YAML. If Python isn't available, skip — GitHub will report parse errors on push.)

- [ ] **Step 4: Sanity-check each job's command runs locally**

The pipeline is faithful to local make targets, so each command should already pass after Tasks 1 and 2. Re-verify:

```bash
make coverage
make test-integration
make lint
```

Expected: all three exit 0. If any fails, stop and fix — CI will fail for the same reason.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "$(cat <<'EOF'
ci: add GitHub Actions workflow for PR checks

Run unit tests (with 80% coverage gate), integration tests, and lint as
three parallel jobs on every pull request. Each job invokes the
matching make target so CI commands stay in lockstep with `make check`.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Verify on a pull request

**Files:** none (validation only).

The CI workflow's acceptance test is a passing PR run. This task is **manual verification** — no code changes.

- [ ] **Step 1: Push the branch and open a PR**

Push the current branch to the remote, then open a pull request against `main` via the GitHub UI or `gh pr create`. The workflow will trigger because the trigger is `pull_request`.

- [ ] **Step 2: Confirm all three jobs run in parallel**

In the PR's Checks tab, verify three jobs appear: **Unit tests + coverage**, **Integration tests**, **Lint**. Their start times should be within seconds of each other (parallel, not serial).

- [ ] **Step 3: Confirm all three jobs pass**

Wait for completion. All three must be green.

If a job fails:
- **Unit job fails on coverage gate** (`FAIL: X% < 80%`): coverage dropped. Either add tests or revert offending production code; do not lower the threshold.
- **Integration job fails because of tmux**: re-check the `apt-get install -y tmux` step ran before `make test-integration`.
- **Lint job fails**: same violations should reproduce locally via `make lint`. Fix and push.
- **Any other failure**: reproduce locally with the matching make target; CI parity is intentional so the same command yields the same result.

- [ ] **Step 4: Confirm cancel-on-update behavior**

Push an empty commit to the same PR: `git commit --allow-empty -m "test: trigger CI re-run" && git push`. The previous in-flight run should show as **Cancelled** in the Actions tab; a new run starts. This validates the `concurrency` block.

- [ ] **Step 5: Mark the spec as Approved**

After the PR has a passing run, edit `docs/superpowers/specs/2026-05-18-ci-pipeline-design.md` line 6:

```diff
-Draft
+Approved
```

Commit:

```bash
git add docs/superpowers/specs/2026-05-18-ci-pipeline-design.md
git commit -m "$(cat <<'EOF'
docs: mark CI pipeline spec as Approved

Acceptance criteria met: PR-triggered pipeline passes with parallel
unit/integration/lint jobs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review checklist

- [x] **Spec coverage:** Trigger (pull_request only) → Task 3 Step 2. Three parallel jobs → Task 3 Step 2 (no `needs:`). Coverage 80% gate → Task 3 uses `make coverage`, which the existing Makefile enforces. Integration tmux install → Task 3 Step 2. Linter via `go tool` → Task 1. Makefile `lint` update → Task 1 Step 5. Setup expansion → Task 2. `.gitignore` for vendor → already done pre-task (noted in plan header). `toolchain` directive → intentionally omitted per spec. Permissions / concurrency → Task 3 Step 2.
- [x] **Placeholder scan:** No TBD/TODO/"implement later". Every code block is complete. Commands have expected output.
- [x] **Type consistency:** Make target names (`coverage`, `test-integration`, `lint`, `vendor`, `tools`, `setup`, `check`) match between Makefile edits and the CI workflow file. `actions/setup-go@v5` consistent across all three jobs. `go-version-file: go.mod` consistent.

---

## Notes for the implementer

- The plan assumes a clean working tree on branch `feat/control-plane` (the current branch where the spec was committed). If unrelated changes are staged, stash them before starting Task 1.
- The order matters: Task 1 must complete before Task 2 (because `make setup` calls `make check` which calls `make lint`, which uses the new `go tool` invocation). Task 3 depends on Tasks 1 and 2 (CI calls `make lint` and assumes the linter is pinned).
- Each task ends with a commit — four commits total (one per task; Task 4 commits the spec status flip).
- If the linter version bump in Task 1 surfaces new violations, prefer fixing the code over relaxing `.golangci.yml`. If you must relax a rule, note why in the commit message.
