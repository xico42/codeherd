# mise + Task tooling, then buf and the first protos: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move codeherd's tooling to mise + go-task + lefthook (Go 1.27, golangci-lint 2.15), then add buf with the settled `codeherd.v1alpha1` shared types, and end with three squashed, stacked commits (docs, tooling, buf).

**Architecture:** `mise.toml` + `mise.lock` pin every tool except go-task (global). `Taskfile.yaml` is the single definition of every command; lefthook and CI only call `task`. buf generates Go from `proto/` into `api/` in the main module; `proto:check` regenerates into a temp dir and diffs, so stale generated code fails lint.

**Tech Stack:** Go 1.27, mise (lockfile), go-task v3, lefthook 2, golangci-lint 2.15, buf v2 config, protoc-gen-go, protoc-gen-go-grpc, GitHub Actions with `jdx/mise-action@v4`.

**Spec:** `docs/superpowers/specs/2026-11-11-mise-task-buf-design.md`

## Global Constraints

- Work happens in `/home/chico/dev/github.com/xico42/codeherd__worktrees/docs-api-conventions`, on top of commit `docs: define the v1alpha1 api conventions`. Never `cd` to the main checkout. Never push.
- Commit after every task with a short working message (`wip(tooling): ...`, `wip(buf): ...`). Task 8 squashes them; working messages are throwaway.
- Every commit leaves the tree building (`go build ./...`).
- Pins: `go = "1.27"`, `golangci-lint = "2.15"`, `lefthook = "2"`, `buf = "1"`, `protoc-gen-go = "1.36"`, `protoc-gen-go-grpc = "1.6"`; `[settings] lockfile = true`; `mise.lock` committed, generated for `linux-x64`.
- go-task is NOT in `mise.toml`. CI installs it with `mise use -g task@3`.
- `go.mod`: `go 1.27`; no `tool` directive.
- Git hooks never modify files: no `stage_fixed`, no `--fix`, no `-w` in `lefthook.yml`. Every formatter has a writing task and a `:check` task; hooks call only the `:check` one.
- No emoji or decorative UTF-8 in any file, task output or message.
- Final commit messages follow the owner's writing-commit-messages skill: no trailers (no Co-Authored-By), exactly two body paragraphs, at most 200 body words, 72-column wrap.
- Generated code: `api/codeherd/v1alpha1/*.pb.go`, committed, excluded from the 80% coverage figure.

## Review Focus

1. **Fresh clone.** `task setup` on a clean clone with only mise and task installed must leave `task check` passing and hooks installed. Pinned in Task 5, step "Fresh-clone check".
2. **`git describe` failing** (no tags, shallow clone): `task build` must still build with version `dev`, as the Makefile did. Pinned in Task 2, step "Build without tags".
3. **A hook that would rewrite a file.** A badly formatted staged `.go` or `.proto` must fail the hook and leave the file byte-identical. Pinned in Task 3 and Task 7.
4. **A deleted proto leaving stale generated code.** `proto:check` must fail when `api/` has a `.pb.go` with no matching proto. Pinned in Task 6, step "Stale-file check".
5. **Release archive contents.** `task release-archive` must produce exactly `LICENSE`, `README.md`, `ch`, which CI's "Verify archive contents" step asserts. Pinned in Task 2, step "Release smoke".

---

## Part A: tooling (squashes into the tooling PR)

### Task 1: Commit the design and this plan

**Files:**
- Add: `docs/superpowers/specs/2026-11-11-mise-task-buf-design.md`, `docs/superpowers/plans/2026-11-11-mise-task-buf.md`

- [ ] **Step 1: Commit**

```bash
git add docs/superpowers
git commit -m "wip(tooling): add design and plan"
```

### Task 2: mise, Go 1.27, golangci-lint 2.15, Taskfile

**Files:**
- Create: `mise.toml`, `mise.lock`, `Taskfile.yaml`
- Modify: `go.mod`, `go.sum`, `.gitignore` (remove the `vendor/` line), any Go file the new vet/linters flag
- Delete: `Makefile`

**Interfaces:**
- Produces: tasks `default setup deps build install test test-integration coverage lint format format:check check clean release-build release-archive release-checksums`. `format` and `format:check` take file paths after `--` (`{{.CLI_ARGS}}`), defaulting to `.`. Release tasks take `GOOS`/`GOARCH` as task vars.

- [ ] **Step 1: Write `mise.toml`**

```toml
[tools]
go = "1.27"              # latest minor as of 2026-11-11; matches go.mod
golangci-lint = "2.15"   # latest minor as of 2026-11-11 (was the go.mod tool directive, v2.12.2)
lefthook = "2"           # git hooks; hooks only check, never write

[settings]
lockfile = true
```

- [ ] **Step 2: Install and lock**

Run: `mise install && mise lock --platform linux-x64 && mise exec -- go version`
Expected: `go version go1.27.x linux/amd64`; `mise.lock` lists go, golangci-lint, lefthook with exact versions.

- [ ] **Step 3: Bump go.mod and drop the tool directive**

Set `go 1.27`, delete `tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint`, run `mise exec -- go mod tidy`.
Expected: `grep -c '// indirect' go.mod` drops from ~200 to a handful; `grep tool go.mod` prints nothing.

- [ ] **Step 4: Write `Taskfile.yaml`**

`version: '3'`. One task per row of the spec's Taskfile table, same commands as the Makefile. Pinned details:
- `build`: version var `sh: git describe --tags --always --dirty 2>/dev/null || echo dev`.
- `release-build`/`release-archive`: version from `sh: cat VERSION`; `requires: vars: [GOOS, GOARCH]`.
- `coverage`: same awk threshold logic as the Makefile, `COVERAGE_THRESHOLD: 80`, removes `coverage.out` after.
- `format:check`: runs `gofmt -s -l` on `{{if .CLI_ARGS}}{{.CLI_ARGS}}{{else}}.{{end}}`; if output is non-empty, prints the files, then `run 'task format' to fix`, and exits 1.
- `check`: `cmds:` (not `deps:`, so it runs sequentially) `format:check`, `coverage`, `test-integration`, `lint`, `build`, then `echo "All checks passed"`.
- `setup`: `mise install`, `go mod download`, `lefthook install`.
- `default`: `task --list`, `silent: true`. Every other task has a `desc:`.

Delete `Makefile`; remove `vendor/` from `.gitignore`.

- [ ] **Step 5: Format check rejects without writing**

```bash
printf 'package main\nfunc  x() {}\n' > zz_fmt.go
sha256sum zz_fmt.go > /tmp/zz.sum
task format:check -- zz_fmt.go; echo "exit=$?"
sha256sum -c /tmp/zz.sum
rm zz_fmt.go
```
Expected: lists `zz_fmt.go`, prints the `task format` hint, `exit=1`, checksum `OK`.

- [ ] **Step 6: Build without tags**

Run: `git clone -q --no-tags . /tmp/ch-notags && (cd /tmp/ch-notags && git tag -l | xargs -r git tag -d && task build && ./ch --version); rm -rf /tmp/ch-notags`
Expected: builds; version prints a short commit hash or `dev`, never an error.

- [ ] **Step 7: Release smoke**

```bash
task release-build GOOS=linux GOARCH=arm64
task release-archive GOOS=linux GOARCH=arm64
tar -tzf dist/ch-$(cat VERSION)-linux-arm64.tar.gz | LC_ALL=C sort
task clean
```
Expected: exactly `LICENSE`, `README.md`, `ch`.

- [ ] **Step 8: Run the gate and fix findings**

Run: `task check`
Expected: `All checks passed`. Fix whatever Go 1.27 vet or golangci-lint 2.15 reports in the code (never by loosening `.golangci.yml`); keep each fix minimal.

- [ ] **Step 9: Commit**

```bash
git add -A mise.toml mise.lock Taskfile.yaml go.mod go.sum .gitignore Makefile
git add -u   # code fixes from step 8
git commit -m "wip(tooling): mise, go 1.27, golangci-lint 2.15, taskfile"
```

### Task 3: lefthook

**Files:**
- Create: `lefthook.yml`

- [ ] **Step 1: Write `lefthook.yml`** exactly as in the spec's lefthook section (`output: [failure]`; pre-commit `parallel: true` (hooks never write, so checks cannot race) with jobs `format` -> `task format:check -- {staged_files}` and `lint` -> `task lint`, both `glob: "*.go"`; pre-push job `test` -> `task test`).

- [ ] **Step 2: Install and prove the hook only reports**

```bash
lefthook install
printf 'package main\nfunc  y() {}\n' > zz_hook.go && git add zz_hook.go
sha256sum zz_hook.go > /tmp/zzh.sum
git commit -m probe; echo "exit=$?"
sha256sum -c /tmp/zzh.sum; git diff --cached --stat
git reset -q zz_hook.go && rm zz_hook.go
```
Expected: commit refused (`exit=1`), output names `zz_hook.go` and the `task format` hint; checksum `OK`; the staged content is unchanged.

- [ ] **Step 3: Clean hook run**

Run: `lefthook run pre-commit --all-files`
Expected: both jobs pass.

- [ ] **Step 4: Commit**

```bash
git add lefthook.yml && git commit -m "wip(tooling): lefthook with check-only hooks"
```

### Task 4: CI and release workflows

**Files:**
- Modify: `.github/workflows/ci.yml`, `.github/workflows/release.yml`

- [ ] **Step 1: Swap tool setup and commands**

In every job, replace `actions/setup-go@v5` (and its `with:`) with `jdx/mise-action@v4`, followed by a step `run: mise use -g task@3`. Replace every `make X` with `task X` (release matrix: `task release-build GOOS=... GOARCH=...`). In release.yml keep the `if: steps.check.outputs.skip != 'true'` guards on the new steps. Leave triggers, job names, the tmux apt install and cosign untouched. Build cache: add `actions/cache@v4` on `~/.cache/go-build` and `~/go/pkg/mod` keyed on `hashFiles('go.sum')` to the `unit`, `integration` and `lint` jobs (the builder's call in the spec; taken here because `setup-go` cached these and dropping it slows every PR).

- [ ] **Step 2: Verify**

Run: `grep -rnE 'setup-go|make ' .github/ ; mise exec actionlint@latest -- actionlint`
Expected: grep prints nothing; actionlint reports no errors.

- [ ] **Step 3: Commit**

```bash
git add .github && git commit -m "wip(tooling): ci and release through mise and task"
```

### Task 5: Docs and fresh-clone check

**Files:**
- Modify: `CLAUDE.md`, `README.md` (development section around lines 297 and 361-364 only; leave `post-clone = "make deps"` examples at ~64 and ~263), `docs/project.md` (tree around line 221)

- [ ] **Step 1: Update docs**

CLAUDE.md: the command block lists `task setup` (mise install, go mod download, lefthook install), `task build`, `task install`, `task test`, `task test-integration`, `task lint`, `task format` / `task format:check`, `task check`, `task clean`; the coverage gate says `task check`; add one line: tools are pinned in `mise.toml`/`mise.lock`, go-task is installed globally, hooks only check. README: `make` -> `task` in the development section. project.md tree: `Makefile` -> `Taskfile.yaml`, add `mise.toml`, `mise.lock`, `lefthook.yml`.

Run: `grep -rnw make CLAUDE.md README.md docs/project.md`
Expected: only the two `post-clone = "make deps"` examples in README.md.

- [ ] **Step 2: Fresh-clone check**

```bash
git clone -q . /tmp/ch-fresh && cd /tmp/ch-fresh && task setup && task check && test -x .git/hooks/pre-commit && echo HOOKS_OK
cd - && rm -rf /tmp/ch-fresh
```
Expected: `All checks passed` and `HOOKS_OK`.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md README.md docs/project.md && git commit -m "wip(tooling): docs use task"
```

---

## Part B: buf (squashes into the buf PR)

### Task 6: buf pipeline with the first proto

**Files:**
- Modify: `mise.toml`, `mise.lock`, `Taskfile.yaml`, `lefthook.yml`
- Create: `buf.yaml`, `buf.gen.yaml`, `buf.lock`, `proto/codeherd/v1alpha1/outcome.proto`, `api/codeherd/v1alpha1/outcome.pb.go` (generated)

**Interfaces:**
- Produces: tasks `proto:generate`, `proto:format` (writes), `proto:check` (never writes); `lint` runs `proto:check` first. Go import path `github.com/xico42/codeherd/api/codeherd/v1alpha1`.

- [ ] **Step 1: Pin the tools**

Add to `mise.toml` `[tools]`: `buf = "1"`, `protoc-gen-go = "1.36"`, `protoc-gen-go-grpc = "1.6"` (one comment each). Run `mise install && mise lock --platform linux-x64`.
Expected: `mise.lock` has exact versions for all three.

- [ ] **Step 2: Write buf config**

`buf.yaml` (v2): `modules: [{path: proto}]`, `deps: [buf.build/googleapis/googleapis]`, `lint: {use: [STANDARD]}`. Run `buf dep update` to create `buf.lock`.
`buf.gen.yaml` (v2): `managed: {enabled: true, override: [{file_option: go_package_prefix, value: github.com/xico42/codeherd/api}], disable: [{module: buf.build/googleapis/googleapis}]}`; plugins `local: protoc-gen-go` and `local: protoc-gen-go-grpc`, each `out: api`, `opt: paths=source_relative`; `clean: true`.

- [ ] **Step 3: First proto**

`proto/codeherd/v1alpha1/outcome.proto`: `package codeherd.v1alpha1;` with `Outcome` and `OutcomeStatus` transcribed from `docs/api/conventions.md` section 6. `Outcome.error` is type `Error` (field 2) and `warnings` is `repeated Warning` (field 3), so define `Error`, `Cause`, `Warning` in `error.proto` in this same step, transcribed from `docs/api/errors.md` "Structure" (needs `import "google/rpc/code.proto"`, `google/protobuf/any.proto`). Short comments from the docs on every message and field.

- [ ] **Step 4: Tasks**

`proto:generate`: `buf generate`. `proto:format`: `buf format -w`. `proto:check`: `buf format --diff --exit-code`, `buf lint`, then generate into a temp dir with `buf generate -o "$tmp"` and `diff -r "$tmp/api" api`; remove `$tmp` on exit (`trap`). Prepend `task: proto:check` to `lint`. In `coverage`, filter generated code before computing the total: `grep -v '^github.com/xico42/codeherd/api/' coverage.out > coverage.tmp && mv coverage.tmp coverage.out` (keep the `mode:` line).
Add a lefthook pre-commit job: `name: proto`, `glob: "*.proto"`, `run: task proto:check`.

- [ ] **Step 5: Generate and build**

Run: `task proto:generate && mise exec -- go mod tidy && go build ./... && task proto:check`
Expected: `api/codeherd/v1alpha1/{outcome,error}.pb.go` exist; `go.mod` gains `google.golang.org/genproto/googleapis/rpc` and `google.golang.org/protobuf` as direct requirements; check passes.

- [ ] **Step 6: Stale-file check**

```bash
cp api/codeherd/v1alpha1/outcome.pb.go api/codeherd/v1alpha1/zz_stale.pb.go
task proto:check; echo "exit=$?"
rm api/codeherd/v1alpha1/zz_stale.pb.go
echo '// drift' >> api/codeherd/v1alpha1/outcome.pb.go
task proto:check; echo "exit=$?"
git checkout -- api/codeherd/v1alpha1/outcome.pb.go 2>/dev/null || task proto:generate
```
Expected: both runs `exit=1`, the first naming `zz_stale.pb.go` (`Only in api/...`), the second showing the diff.

- [ ] **Step 7: Gate**

Run: `task check`
Expected: `All checks passed`; the coverage total is unchanged from Task 2 (generated code filtered).

- [ ] **Step 8: Commit**

```bash
git add -A mise.toml mise.lock Taskfile.yaml lefthook.yml buf.yaml buf.gen.yaml buf.lock proto api go.mod go.sum
git commit -m "wip(buf): buf pipeline, outcome and error types"
```

### Task 7: The remaining shared types

**Files:**
- Create: `proto/codeherd/v1alpha1/meta.proto` (`ObjectMeta`), `selector.proto` (`Scope`, `Selector`, `LabelRequirement` with nested `Operator`), `operation.proto` (`Operation`, `OperationState`, `OperationResult`, `ItemResult`, `Progress`), `delete.proto` (`DeleteOptions`, `Propagation`); their generated `.pb.go`

- [ ] **Step 1: Write the protos**

Transcribe field names, numbers and enum values exactly from `docs/api/conventions.md`: section 3 (`ObjectMeta`, including `delete_time = 8`; `annotations` stays a comment), section 5 (`Scope`, `Selector`, `LabelRequirement`), 7.2 and 7.4 (`Operation`, `OperationState`, `OperationResult`, `Progress`), section 11 (`DeleteOptions`, `Propagation`). `ItemResult` is `Outcome outcome = 1;` plus `oneof result { google.protobuf.Any extension = 100; }` with a comment that core kinds take numbers 2-99. Do not add `SubmitResponse`, Subscribe types or any resource kind.

- [ ] **Step 2: Hook rejects a misformatted proto without writing**

```bash
printf 'syntax = "proto3";\npackage codeherd.v1alpha1;\nmessage   Zz {}\n' > proto/codeherd/v1alpha1/zz.proto
git add proto/codeherd/v1alpha1/zz.proto && sha256sum proto/codeherd/v1alpha1/zz.proto > /tmp/zzp.sum
git commit -m probe; echo "exit=$?"
sha256sum -c /tmp/zzp.sum
git reset -q proto/codeherd/v1alpha1/zz.proto && rm proto/codeherd/v1alpha1/zz.proto
```
Expected: `exit=1`, checksum `OK`.

- [ ] **Step 3: Generate and gate**

Run: `task proto:generate && go build ./... && task check`
Expected: `buf lint` clean (fix the proto, not the lint config, if STANDARD complains); `All checks passed`.

- [ ] **Step 4: Docs**

`docs/api/conventions.md` 7.6: replace "The generated Go code lives in its own small module (for example `github.com/xico42/codeherd/api`), so plugins do not depend on the CLI, tmux or git packages." with: the generated code lives in `api/` in the main module (`github.com/xico42/codeherd/api/codeherd/v1alpha1`); it moves to its own module when a plugin needs to import it without the CLI. CLAUDE.md: add `task proto:generate`, `task proto:format`, `task proto:check`, and a package-layout line for `proto/` and `api/` (generated, never hand-edited).

- [ ] **Step 5: Commit**

```bash
git add proto api docs/api/conventions.md CLAUDE.md && git commit -m "wip(buf): remaining shared types and docs"
```

---

## Part C: squash and stack

### Task 8: Squash into three commits

- [ ] **Step 1: Record the boundaries**

`D` = the docs commit (`docs: define the v1alpha1 api conventions`); `T` = the last `wip(tooling)` commit; `B` = HEAD.

- [ ] **Step 2: Rebuild the history**

```bash
git branch backup/mise-buf-wip HEAD
git checkout -q --detach T && git reset --soft D && git commit -q -m tooling      # squashes D..T
git restore --source=backup/mise-buf-wip --staged --worktree -- . && git commit -q -m buf   # squashes T..B
git diff --exit-code HEAD backup/mise-buf-wip && echo SAME_TREE
```
`git restore --source` also removes files absent from the source, so deletions carry over. Expected: `SAME_TREE`. Messages are replaced in step 3 (`git commit --amend` on the buf commit; `git rebase -i` is unavailable, so reword the tooling commit by rebuilding: `git checkout <tooling sha>`, `git commit --amend -F msg`, then `git cherry-pick <buf sha>` and amend its message).

- [ ] **Step 3: Write the two messages**

Invoke the writing-commit-messages skill for each. Tooling: `chore: ...` (no Links). Buf: `feat: ...` with `Links:` to `https://github.com/xico42/codeherd/issues/27`. No trailers.

- [ ] **Step 4: Verify each commit**

For each of the three commits: `git checkout <sha> && task check` (docs commit: `make check`, the Makefile still exists there).
Expected: passes at every commit.

### Task 9: Stacked branches

- [ ] **Step 1: Create branches**

`docs/api-conventions` -> docs commit; `chore/mise-task` -> tooling commit; `feat/api-buf` -> buf commit. Return the worktree to `feat/api-buf`.

- [ ] **Step 2: Hand over**

Show the owner `git log --oneline main..feat/api-buf` and the three messages. Do not push or open PRs until the owner approves; then PRs are docs on `main`, tooling on `docs/api-conventions`, buf on `chore/mise-task`, the buf PR body closing #27 and noting that `Hello` is tracked as an M0 open question in epic #26.
