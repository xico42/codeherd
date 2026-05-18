# CI Pipeline Design

## Date
2026-05-18

## Status
Draft

## Summary
Add a GitHub Actions CI pipeline that runs unit tests (with 80% coverage gate), integration tests, and lint as three parallel jobs on every pull request. Pipeline commands map one-to-one to existing Makefile targets, preserving the invariant that everything CI runs is also runnable locally via `make`. Pin `golangci-lint` through Go 1.24's `tool` directive in `go.mod` so a single source of truth governs the lint version locally and in CI. Expand `make setup` to cover deps, vendoring, tool installs, and full `make check`, so first-time onboarding is a single command.

## Motivation
The repository has comprehensive automated testing (`make test`, `make test-integration`, `make lint`, plus an 80% coverage gate baked into `make coverage`) but no CI pipeline runs them on incoming changes. Pull requests merge without an automated check that the suite still passes, the coverage threshold still holds, or the linter is clean. The fix is a minimal GitHub Actions workflow that wraps existing make targets — no new test infrastructure, just CI plumbing.

## Goals
- Run unit tests + 80% coverage gate, integration tests, and lint on every PR.
- Execute the three check categories in parallel for fast feedback.
- Preserve full parity between CI commands and `make check` so contributors can reproduce CI failures locally with the same command.
- Pin the linter version reproducibly via Go-native tooling (no out-of-band install).
- Make first-time setup a single command (`make setup`).

## Non-Goals
- No build or release pipeline. CI is for verification only.
- No `push: main` trigger or scheduled runs — pull requests only.
- No coverage artifact upload, no PR coverage comments, no badge wiring.
- No `vendor/` committed to the repo — vendoring is a local-only developer convenience.
- No `toolchain` directive in `go.mod` — `go 1.26` minimum is sufficient.

## Design

### Workflow shape

A single workflow file at `.github/workflows/ci.yml`:

- **Trigger**: `pull_request` against any base branch.
- **Runner**: `ubuntu-latest` for all jobs.
- **Permissions**: workflow-level `contents: read`. No write tokens needed.
- **Concurrency**: group by `${{ github.workflow }}-${{ github.ref }}` with `cancel-in-progress: true`, so a new push to a PR cancels stale runs.
- **Jobs**: three independent jobs (`unit`, `integration`, `lint`) with no `needs:` relationships — they run fully in parallel.

### Job specs

All three jobs share the same first two steps (`actions/checkout@v5` + `actions/setup-go@v5` with `go-version-file: go.mod`), then diverge:

| Job | Final step(s) | Purpose |
|---|---|---|
| `unit` | `make coverage` | Runs `go test ./...` with `-coverprofile`, prints total coverage, exits non-zero if `< 80%`. |
| `integration` | `sudo apt-get update && sudo apt-get install -y tmux`<br>`make test-integration` | Installs tmux (required — without it `cmd/session_integration_test.go` and `cmd/profiles_integration_test.go` silently skip via `t.Skip("tmux not available")`), then runs `go test -tags integration ./...`. |
| `lint` | `make lint` | Runs `go tool golangci-lint run ./...` (after Makefile update — see below). |

**No `actions/cache` step is needed.** `actions/setup-go@v5` enables module and build caches automatically; the `go tool` build cache for `golangci-lint` is preserved by the same mechanism.

**No global git config is needed.** Integration tests configure `user.email` / `user.name` per-repo (e.g. `git -C <dir> config user.email ...` in `initBareRepo` helpers); they never depend on global git identity.

### Tool versioning via `go tool`

`golangci-lint` is pinned through Go 1.24+'s `tool` directive in `go.mod`:

```
tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint
```

Added via `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` at implementation time. The version resolves to the latest stable v2.x at that moment and is committed via `go.mod` + `go.sum`. Bumps thereafter use the same command with an explicit version.

**Local invocation**: `go tool golangci-lint run ./...` (or `make lint`).
**CI invocation**: identical.
**Cold-cache cost**: first invocation per machine / per CI cache window compiles the linter from source (~30–60s). Subsequent runs hit the Go build cache.

`actions/setup-go`'s build cache persists across CI runs, so the cold-cache cost is paid roughly once per cache eviction, not per PR.

### Makefile changes

Four targets touched / added:

```diff
 lint:
-	golangci-lint run ./...
+	go tool golangci-lint run ./...

-setup: deps check
+setup: deps vendor tools check
 	@echo "Setup complete"

+vendor:
+	go mod vendor
+
+tools:
+	go install tool
```

Execution order in `setup`:

1. `deps` (`go mod download`) — populate the module cache.
2. `vendor` (`go mod vendor`) — materialize `vendor/` for local browsing / offline work. Local-only; `vendor/` is gitignored.
3. `tools` (`go install tool`) — install every tool from `go.mod`'s `tool` block to `$GOBIN`.
4. `check` (existing) — `coverage` → `test-integration` → `lint` → `build`.

`vendor/` is added to `.gitignore`. CI never runs `make setup` and never sees `vendor/`; CI invokes the specific check targets directly. Local `go build` will auto-use `-mod=vendor` when `vendor/` exists, but `go.sum` enforces hash parity with the module-cache resolution CI uses — both paths produce byte-identical output.

### File changes summary

| Path | Change | Reason |
|---|---|---|
| `.github/workflows/ci.yml` | new | The pipeline itself. |
| `go.mod` | add `tool` directive | Pin `golangci-lint` version. |
| `go.sum` | regenerated | Hashes for tool transitive deps. |
| `Makefile` | edit `lint`, edit `setup`, add `vendor`, add `tools` | Use pinned linter; expand setup target. |
| `.gitignore` | add `vendor/` | Keep local vendoring out of the repo. |

## Testing approach

The CI workflow itself is verified by **running it**: opening the PR with these changes is the acceptance test. All three jobs must pass for the AC ("passing MR pipeline with all the checks executed locally by `make check`") to be met.

Local pre-push validation:

- `go get -tool ...` resolved cleanly and `go.sum` is consistent.
- `make lint` succeeds with the new `go tool golangci-lint` invocation.
- `make coverage` continues to pass the 80% gate.
- `make test-integration` passes locally (where tmux is installed).
- `make check` succeeds end-to-end.
- `make setup` works on a clean checkout (deps + vendor + tools + check all succeed).

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| `go tool golangci-lint` cold-build is slow on CI | `actions/setup-go` build cache makes it a once-per-cache-window cost; warm runs are fast. |
| `golangci-lint v2` rules differ from any v1 baseline in CI elsewhere | This repo has no prior CI; the v2 ruleset becomes the baseline. Fix or configure away violations as part of implementation. |
| Local `vendor/` + CI module cache could diverge | `go.sum` enforces hash parity; both paths verify against the same hashes. Risk is theoretical. |
| Adding `vendor/` to `.gitignore` while another contributor already commits one | Solo project — no other contributors. Non-issue. |
| Integration tests flake on the CI runner (timing, tmux behavior) | Tests already gate on `tmux` availability with `t.Skip`. If real flakes appear post-implementation, address case-by-case; out of scope for this design. |

## Open questions
None. All design decisions are settled.

## Out of scope
- Build / release / publish pipelines.
- Scheduled (`cron`) or `push: main` triggers.
- Coverage artifact upload, PR coverage comments, status badges in README.
- A `toolchain` directive in `go.mod` (deferred until a reproducibility incident makes the case).
- Replacing the integration tests' `t.Skip("tmux not available")` with a hard failure when tmux is required.
- Migrating to mise or another tool-version manager — `go tool` + `go.mod`'s `go` directive cover the current need.
