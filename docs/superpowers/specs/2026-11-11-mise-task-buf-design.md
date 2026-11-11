# Design: mise + Task tooling, then buf and the first protos

Date: 2026-11-11. Repo: github.com/xico42/codeherd (Go CLI `ch`).

## Intent

1. **Tooling (PR A).** The owner standardizes every project on mise for tool
   versions and go-task for commands. codeherd is the one project still on a
   Makefile plus `go tool` pins. Migrate it so it matches the others.
2. **buf (PR B).** Issue #27 still lacks "set up buf and code generation".
   Add buf, generate Go from `codeherd.v1alpha1`, and commit the shared
   message types that `docs/api/conventions.md` and `docs/api/errors.md`
   already settle. PR B closes #27.

The API docs PR (branch `docs/api-conventions`) is separate and lands first.
PR A comes next, then PR B on top of it.

## Workflow (decided by the owner)

- **One pass.** PR A and PR B are built in a single run of work, on top of
  the docs commit in the `docs/api-conventions` worktree. No waiting for a
  PR to merge before starting the next.
- **Commit as you go.** Small working commits after each step, PR A's work
  first, then PR B's. Each commit leaves the tree building; `task check`
  (or `make check` before the Taskfile exists) passes at the end of each PR's
  run of commits.
- **Squash and stack at the end.** Once everything passes, the working
  commits are squashed into one commit per PR (docs, tooling, buf), each with
  a message in the owner's commit style. Then three stacked branches and PRs
  are built from them: docs on `main`, tooling on docs, buf on tooling.
- Nothing is pushed until the owner reviews the squashed stack.

## Goals

- One `mise.toml` pins every tool; `mise.lock` makes versions exact on every
  machine and in CI.
- `Taskfile.yaml` replaces the Makefile, one task per current target.
- CI and the release workflow install tools through mise and call `task`.
- buf lints the protos and generates Go; CI fails when generated code is stale.
- The settled shared types exist as proto and compile into Go.

## Non-goals

- No services or RPCs yet: no `Hello`, no `Subscribe`, no per-noun services.
- No separate Go module for the generated code (deferred until a plugin needs
  it).
- No `buf breaking` (it starts at the `codeherd.v1` freeze).
- No CI redesign beyond swapping the tool setup and commands.

## Anti-goals

- A laptop and CI generating different code (patch drift in `protoc-gen-go`).
- Generated code lowering the 80% coverage gate.
- Contributors needing anything beyond mise and a global `task`.

## Approaches considered (tool installation)

| Approach | Trade-off | Verdict |
|---|---|---|
| Pin buf and generators as Go tools in `go.mod` | No install step, but a large dependency tree in go.mod; unlike the owner's other projects | rejected |
| **mise for every tool, with a lockfile** | Matches the owner's other projects; one pin file; exact versions | **chosen by the owner** |
| buf's remote (BSR) plugins | No local binaries, but network on every generate and rate limits | rejected |

---

## PR A: mise and Task

### Decided by the owner

- `mise.toml` with `[settings] lockfile = true`; `mise.lock` committed.
- go-task is **not** in `mise.toml`: the owner has it installed globally. CI
  installs it globally with `mise use -g task@3`.
- The Makefile becomes `Taskfile.yaml` in this PR; the Makefile is deleted.
- No vendoring: drop the `vendor` target and `vendor/` from `.gitignore`.
- `setup` is the minimum to get going: `mise install`, then `go mod download`.
  It does not run `check`.

### mise.toml

Pins follow the owner's arcanaprimaria style (major or minor, with a comment):

```toml
[tools]
go = "1.27"              # latest minor as of 2026-11-11; matches go.mod
golangci-lint = "2.15"   # latest minor as of 2026-11-11 (was the go.mod tool directive, v2.12.2)

[settings]
lockfile = true
```

Generate `mise.lock` for `linux-x64` (the owner's machine and CI runners).

### go.mod

Bump the `go` directive to `go 1.27` (decided by the owner: a full move to
1.27, not only the toolchain pin). Fix anything the new Go version or
golangci-lint 2.15 flags (new vet checks, new linter findings) in the same PR,
so `task check` stays green.

Remove the `tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint` line
and run `go mod tidy`, which drops the indirect requirements only that tool
needed. `.golangci.yml` is unchanged.

### Taskfile.yaml (one task per Makefile target)

| Task | Does | Notes |
|---|---|---|
| `default` | `task --list` | silent |
| `setup` | `mise install`, `go mod download`, `lefthook install` | |
| `deps` | `go mod download` | |
| `build` | `go build -ldflags "-s -w -X main.version=<git describe --tags --always --dirty, or dev>" -o ch .` | |
| `install` | `build`, then move `ch` to `~/.local/bin/ch` | |
| `test` | `go test ./...` | |
| `test-integration` | `go test -tags integration ./...` | |
| `coverage` | coverprofile, total, fail under 80% | same logic as the Makefile |
| `lint` | `golangci-lint run ./...` | binary from mise, not `go tool` |
| `format` | `gofmt -s -w` on `{{.CLI_ARGS}}`, or `.` when none | writes |
| `format:check` | `gofmt -s -l` on `{{.CLI_ARGS}}` or `.`; lists unformatted files and exits non-zero if any | never writes |
| `check` | format:check, coverage, test-integration, lint, build, then "All checks passed" | run in that order, sequentially |
| `clean` | `rm -rf ch coverage.out dist` | |
| `release-build` | needs `GOOS`, `GOARCH` vars; `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=<VERSION file>" -o dist/<os>-<arch>/ch .` | called as `task release-build GOOS=linux GOARCH=arm64` |
| `release-archive` | copy LICENSE, README.md; tar to `dist/ch-<VERSION>-<os>-<arch>.tar.gz` | same vars |
| `release-checksums` | `sha256sum *.tar.gz > checksums.txt` in `dist` | |

`tools` and `vendor` are dropped.

### CI (.github/workflows/ci.yml, release.yml)

- Replace every `actions/setup-go@v5` with `jdx/mise-action@v4`, then a step
  `mise use -g task@3`.
- Replace `make X` with `task X`. Job structure, triggers and the tmux apt
  install stay as they are.
- The release workflow keeps `sigstore/cosign-installer`; only Go setup and
  the `make` calls change.
- Builder's call: whether to add a Go build cache (`actions/cache` on
  `~/.cache/go-build` and `~/go/pkg/mod`, keyed on go.sum) as arcanaprimaria
  does. `setup-go` cached these implicitly; mise-action does not.

### Docs

Switch `make` to `task` in CLAUDE.md (the gate becomes `task check`; mention
`mise install` in setup), README.md's development section (lines ~297 and
~361-364) and `docs/project.md`'s tree (Makefile -> Taskfile.yaml, add
mise.toml/mise.lock). README lines ~64 and ~263 (`post-clone = "make deps"`)
are examples of a user's project hook config, not codeherd's own commands:
leave them.

### lefthook (decided by the owner: adopt it in this PR)

Hooks delegate to `task`, so the Taskfile stays the one definition of
formatting and linting.

**Hooks never modify files (decided by the owner).** They only check and
report. The owner fixes by running the writing task (`task format`). No
`stage_fixed`, no `--fix`, no `-w` in any hook. The owner will move the other
projects to this pattern later.

- `mise.toml`: `lefthook = "2"`.
- `task setup` also runs `lefthook install`.
- `lefthook.yml` (Claude's call, owner may adjust):

```yaml
output: [failure]

pre-commit:
  parallel: true    # hooks never write, so checks cannot race
  jobs:
    - name: format
      glob: "*.go"
      run: task format:check -- {staged_files}
    - name: lint
      glob: "*.go"
      run: task lint

pre-push:
  jobs:
    - name: test
      run: task test
```

- `format:check` prints the offending files and a hint to run `task format`.
- `task lint` lints the whole module: golangci-lint needs whole packages, and
  its cache keeps a repeat run fast.
- Integration tests and the coverage gate stay in CI and `task check`, not in
  hooks (they need tmux and take longer).
- PR B adds a `*.proto` job running `task proto:check`.

### Done when

`task check` passes locally; `lefthook run pre-commit` passes; CI passes on
the PR with no `setup-go` or `make` left in `.github/`.

---

## PR B: buf and the shared types

### Decided by the owner

- Tools through mise: buf, `protoc-gen-go`, `protoc-gen-go-grpc`, locked
  exactly via `mise.lock`.
- Generated code stays in the main module (no second go.mod).
- Proto content is "option 2": the shared types already settled, no services.

### Layout (builder may adjust names, not the split)

```
buf.yaml                 # v2; modules: [{path: proto}]; deps: googleapis; lint: STANDARD
buf.gen.yaml             # v2; managed mode; local plugins
buf.lock                 # committed
proto/codeherd/v1alpha1/*.proto
api/codeherd/v1alpha1/*.pb.go   # generated, committed
```

- `api/`, not `internal/`: splitting into its own module later is then just
  adding `api/go.mod`.
- Managed mode sets `go_package_prefix: github.com/xico42/codeherd/api`, with
  googleapis excluded from the override. Plugins run locally (`protoc-gen-go`,
  `protoc-gen-go-grpc`, both from mise), `out: api`,
  `opt: paths=source_relative`. `protoc-gen-go-grpc` emits nothing until a
  service exists; it is configured now so the first service needs no setup.
- `google.rpc.Code` comes from `buf.build/googleapis/googleapis` (BSR,
  network needed on first resolve and in CI). The Go side then needs
  `google.golang.org/genproto/googleapis/rpc`.

### Proto content

Transcribe exactly from the docs (field names, numbers, enum values):

| Source | Messages |
|---|---|
| conventions.md section 6 | `Outcome`, `OutcomeStatus` |
| errors.md "Structure" | `Error`, `Cause`, `Warning` |
| conventions.md section 3 | `ObjectMeta` (with `delete_time = 8`) |
| conventions.md section 5 | `Scope`, `Selector`, `LabelRequirement` (+ nested `Operator`) |
| conventions.md 7.2, 7.4 | `Operation`, `OperationState`, `OperationResult`, `ItemResult`, `Progress` |
| conventions.md section 11 | `DeleteOptions`, `Propagation` |

- `ItemResult`'s oneof carries only `google.protobuf.Any extension = 100` for
  now. The resource arms (`workspace`, `session`, `deleted`) wait until those
  kinds are defined; numbers 2-99 stay free for them.
- Not included: `SubmitResponse` (an RPC response; also conflicts with
  buf's STANDARD rule that responses be unique per RPC, a question for the
  first service), the Subscribe event shape (still a proposal), any resource
  kind.
- File split is the builder's call (for example `outcome.proto`,
  `error.proto`, `meta.proto`, `selector.proto`, `operation.proto`).
- Each message and field gets a short comment taken from the docs.

### Tasks

- `proto:generate`: `buf generate`.
- `proto:check`: `buf format --diff --exit-code`, `buf lint`, then
  `buf generate -o <temp dir>` and `diff -r` the result against `api/`.
  This checks "the generated code matches the protos", independent of git
  state, so it passes with uncommitted but regenerated changes (Claude's call:
  a git-status check would fail every local run while a proto change is in
  progress).
- `lint` also runs `proto:check`, so CI's lint job covers it.
- `task proto:format` writes (`buf format -w`); `proto:check` only checks.
- lefthook gains a pre-commit job on `*.proto` running `task proto:check`
  (checks only, like every hook).

### Coverage

Generated packages are excluded from the 80% figure: the `coverage` task
filters `github.com/xico42/codeherd/api/` lines out of `coverage.out` before
computing the total. golangci-lint already skips generated files.

### Docs

`docs/api/conventions.md` 7.6: replace "The generated Go code lives in its
own small module" with: the generated code lives in `api/` in the main module;
it moves to its own module when a plugin needs to import it without the CLI.
Mention `task proto:generate` in CLAUDE.md.

### Done when

`task check` passes; `buf lint` is clean; regenerating produces no diff; CI
green; the PR body closes #27 (decided by the owner) and notes that the
`Hello` handshake is tracked as an M0 open question in epic #26.

## Left to the builder

- Proto file split and comment wording.
- Whether to add the Go build cache in CI.
- Task descriptions (`desc:`) and internal task structure.
