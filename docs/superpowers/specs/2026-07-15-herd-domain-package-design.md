# `herd` domain package — design

**Status:** proposed (brainstorming)
**Date:** 2026-07-15
**Out of scope:** the build-time plugin/extension model (no clear model yet — see §11); `filecopy` and `herdtemplate` internals; the TUI's visual design.

## 1. Goal

Collapse `internal/session`, `internal/worktree`, and `internal/project` into one domain package, `internal/herd`, that owns both the primitives and the multi-step operations built on them. Reduce `cmd/` and `internal/tui/` to thin front ends that parse input, call one `herd` operation, and render the result.

This eliminates a class of defects, not a single defect. The bug that prompted the work — a deleted worktree leaving its agent's tmux session and process tree alive — is one of at least four instances of the same root cause shipping today.

## 2. Background: the defect and what it revealed

`internal/tui/actions.go`'s `confirmDeleteAll` killed the shell session by its tmux session ID and the agent session by a *rebuilt session name*. The rebuilt name missed, the error was discarded with `_ =`, and the worktree was force-deleted anyway — orphaning the agent process against a directory that no longer existed.

The name missed for two independent reasons:

1. **Diverged HEAD.** `Stop` rebuilt the name from `Item.Branch`, which holds the *display* branch. `items.go:88-108` deliberately lets that differ from the identity branch the session was named after (introduced by b51f560).
2. **Active profile.** `session.Service.Stop` hardcodes an empty profile via `semconv.SessionName("", …)`, so under a profile it searches for `myapp-feat` while the session is `work-myapp-feat`.

The fix on this branch kills both sessions by tmux session ID. It resolves the symptom in one function and leaves the cause untouched.

### 2.1 The structural cause

Compare the three constructors:

```go
func worktree.NewService(cfg *config.Config, git WorktreeRunner, tmux *tmux.Client, hook hooks.Hook)
func project.NewService(cfg *config.Config, git GitRunner, hook hooks.Hook)
func session.NewService(tmux *tmux.Client, hook hooks.Hook)   // no cfg
```

`session.Service` is the only core service without config. It cannot know the active profile. So every profile decision moved up into its callers, and the API records the asymmetry:

```go
type StartRequest struct { …; Profile string }              // the write path takes a profile
func (s *Service) Start(req StartRequest) (string, error)

func (s *Service) Show(project, branch, sessionType string) (*SessionInfo, error)  // no profile
func (s *Service) Stop(project, branch, sessionType string) error                  // no profile
```

**You can create a session you cannot address.** No call site could have gotten this right; the parameter does not exist. `ShowByName`/`StopByName` are the escape hatch someone added later, and `cmd/services.go` exists only to choose between the two variants:

```go
prof := activeProfile()
if prof == "" {
    return svc.Show(project, branch, sessionType)
}
return svc.ShowByName(semconv.SessionName(prof, project, branch), sessionType)
```

That shim is the missing `cfg` field in disguise.

### 2.2 The boundary is already broken

`internal/worktree` imports `internal/tmux` and uses it for exactly three things, all of them session operations:

```go
worktree.go:594:  if running, _ := s.tmux.HasSession(candidate); running {
worktree.go:636:  running, err := s.tmux.HasSession(name)
worktree.go:644:  if err := s.tmux.KillSession(name); err != nil {
```

It also declares `ErrSessionRunning` and builds session names with `semconv.SessionName`. **The worktree package manages sessions.** Nothing prevented it from importing `session` (no cycle exists), but it reimplemented the logic against the raw tmux client instead — without the profile, and therefore wrongly.

The domain object is a *workspace*: a worktree together with its sessions. Splitting it across packages that cannot see each other forced one to smuggle the other's logic in through the mechanism layer. The enforced boundary caused the defect.

## 3. Evidence: the duplication this creates

Counts are non-test occurrences.

| Pattern | Count | Locations |
|---|---|---|
| `semconv.SessionName("", …)` — the profile-blind literal | 9 | `cmd/template.go:83`, `cmd/worktree.go:157,179`, `worktree.go:593,631,632`, `session.go:206,295`, `tui/actions.go:471` |
| `ListSessions()` + match on `CanonicalName` + `SessionType` | 6 | `session.go:97,212,242,274,302,330` |
| `tmux.NewClient(tmux.NewRealRunner())` | 10 | 8 in `cmd/`, plus `tui/`, `plugin.go` |
| `h := hooks.New(projCfg.Hooks)` — per-project hook binding | 13 | 5 in `cmd/` (`session.go:192`, `template.go:72`, `worktree.go:105`, `project.go:108,135`); 8 in `internal/tui/` (`actions.go:65,107,200,231,263,410`, `agent_picker.go:94`, `form.go:137`) |
| "clone → worktree → copy → template → session" chain | 3 | `cmd/worktree.go:112-198`, `cmd/session.go:194-266`, `tui/actions.go` (×4 via `runFileCopyAndTemplate`) |

### 3.1 Live defects beyond the one fixed

| Defect | Evidence |
|---|---|
| `.herd` templates render a different `SessionName` depending on which command created the worktree | `cmd/session.go:233` passes `activeProfile()`; `cmd/worktree.go:157`, `cmd/template.go:83`, `tui/actions.go:471` pass `""` |
| `ch list worktree`'s "(running)" marker never appears under a profile | `worktree.go:593` hardcodes `""` |
| The kill loop in `worktree.Delete` is always dead code | The TUI kills by ID first; `Delete:635` then re-runs a profile-less lookup that either misses (profile on) or no-ops (profile off) |

### 3.2 Dependency injection already collapsed

`cmd/tui.go:117-121` injects three services built with `&hooks.NoOp{}`. The actions need `hooks.New(projCfg.Hooks)`, so every action rebuilds its own service — 8 times inside `internal/tui` alone. `Model.sesSvc` and `Model.projSvc` are **assigned and never read**: dead fields. Per-project hook binding defeated injection.

This is the constraint that decides §6: `Herd` can hold pre-built state only because it holds `cfg` and resolves hooks per operation itself. Any design that binds hooks at construction reproduces these dead fields.

### 3.3 The test that should have caught it

`cmd/profiles_integration_test.go:136`, `TestProfiles_sessionIsolationAcrossProfiles`, runs against real tmux and covers profile × {create, list}. It never stops or deletes anything. The bug lives in profile × {stop, delete, show} — the quadrant nobody wrote.

## 4. Decision

Merge `internal/session`, `internal/worktree`, and `internal/project` into `internal/herd`. Keep the exec-boundary interfaces that already exist. Front ends depend only on `herd`.

The rule that decides membership:

> **Domain** = needs `cfg`, the profile, or identity to make a decision → `herd`.
> **Mechanism** = does not → stays a support package.

`filecopy` and `herdtemplate` stay out. They never needed the profile, which is why they were never implicated.

## 5. Package structure

```
main.go                 thin — unchanged (three lines)
cmd/                    front end + composition root
internal/tui/           front end
internal/herd/          DOMAIN — session.go worktree.go project.go launch.go teardown.go list.go
internal/tmux/          mechanism — Runner interface + Client
internal/git/           mechanism — WorktreeRunner + CloneRunner, rehoused from two packages
internal/config/  internal/semconv/  internal/hooks/  internal/filecopy/  internal/herdtemplate/
```

`cmd/` remains outside `internal/` and `main.go` remains thin. Both are deliberate and unchanged.

`herd` must never import `cmd` or `tui`. That discipline costs nothing now and keeps a future promotion of `herd` out of `internal/` a rename.

`internal/git` rehouses the two git-exec abstractions that live in separate packages today. Both interfaces keep their current shape:

```go
package git

type WorktreeRunner interface { … }   // 13 methods — was worktree.WorktreeRunner
type CloneRunner    interface { Clone(repo, path, branch string) error }  // was project.GitRunner
type Runner         interface { WorktreeRunner; CloneRunner }   // wiring convenience

type RealRunner struct{}   // implements Runner
```

This rehouses rather than redesigns. `WorktreeRunner` is already a 13-method interface; widening `Deps.Git` to the `Runner` union means a fake must satisfy all 14, so `herd`'s test package carries one shared fake with per-test overrides rather than each test hand-rolling a runner. Splitting these interfaces further is out of scope — they sit at the exec boundary where exactly one real implementation exists.

## 6. The `herd` API

Inside one cohesive package, `session.go` calls `worktree.go` directly. No internal interfaces. The only interfaces are `tmux.Runner`, the git runners, and `hooks.Hook` — all pre-existing, all at the exec boundary.

Only the exported surface is a design commitment:

```go
package herd

// ── types ──
type Herd struct{ … }               // cfg + profile + runners
type Ref struct{ Profile, Project, Branch string }   // Branch is ALWAYS the identity branch
type Handle struct{ ID string; Ref Ref; Type SessionType; Status, Annotation string }
type Workspace struct {
    Ref  Ref
    Path string
    IsMain bool
    DisplayBranch string   // derived for rendering
    HeadHint      string   // "detached" | "on <branch>"
    Agent, Shell  *Handle  // nil when not running
}
type Project struct{ … }
type RemoteBranch struct{ … }
type SessionType string             // SessionTypeAgent | SessionTypeShell

type Deps struct{ Tmux tmux.Runner; Git git.Runner }
type EnsureOpts, LaunchOpts, StopOpts, TeardownOpts struct{ … }

// ── constructor ──
func New(cfg *config.Config, profile string, deps Deps) *Herd

// ── identity ──
func (h *Herd) Ref(project, branch string) Ref     // supplies the profile
func (h *Herd) WithProfile(name string) (*Herd, error)

// ── query ──
func (h *Herd) List(project string) ([]Workspace, error)   // "" = all projects
func (h *Herd) Resolve(ref Ref, t SessionType) (Handle, error)
func (h *Herd) Projects() []Project
func (h *Herd) RemoteBranches(project string) ([]RemoteBranch, error)

// ── mutate ──
func (h *Herd) EnsureWorkspace(ref Ref, opts EnsureOpts) (Workspace, error)
func (h *Herd) Launch(ref Ref, opts LaunchOpts) (Handle, error)
func (h *Herd) StopSessions(ref Ref, opts StopOpts) ([]Handle, error)
func (h *Herd) Teardown(ref Ref, opts TeardownOpts) error
func (h *Herd) Clone(project string) error
func (h *Herd) Provision(ref Ref) error            // `ch template`
func (h *Herd) SetStatus(sessionName, status, annotation string) error

// ── errors ──
var ErrNotCloned, ErrAlreadyCloned, ErrWorktreeNotFound, ErrWorktreeExists,
    ErrSessionNotFound, ErrSessionRunning error
type SessionExistsError struct{ … }
```

`hooks` does not appear. `Herd` holds `cfg`, so it builds `hooks.New(cfg.Projects[p].Hooks)` per operation itself. Tests pass a `cfg` with no hooks configured and nothing fires.

### 6.1 `Ref` and the convention

`Ref` has exported fields. The gate is convention: **obtain a `Ref` from `h.Ref(…)` or from `Workspace.Ref`, never by hand.**

This convention is stronger than the one it replaces. `SessionName("", p, b)` failed nine times because the profile was a positional string where `""` is easy to type, legitimately valid, and the only available path. `h.Ref(p, b)` takes no profile at all, so the shortest path is the correct one, and a hand-built `herd.Ref{Project: p, Branch: b}` is visibly missing a field under review.

An opaque `Ref` (unexported fields) was considered and rejected in §11.

### 6.2 `Workspace` separates identity from display

`Ref.Branch` is identity. `DisplayBranch` is derived for rendering. Today both are the same `string` in `Item.Branch`, which is exactly how a rendering value round-tripped into `wtSvc.Delete`. Here no path back exists: `Teardown` takes a `Ref`, and a `DisplayBranch` will not compile into one.

`Workspace` also collapses a duplicated join. `worktree.Service.List` computes identity and a session name, then discards both into the display string `"proj-branch (running)"` (`worktree.go:40`), forcing `items.go` to recompute the same join with the correct profile. One `List` returns structure both front ends consume.

## 7. Operations

`herd` owns only flows that are multi-step and duplicated across front ends today. Single-step verbs stay direct calls. **A `herd` method that merely forwards is wrong.**

| Operation | Steps |
|---|---|
| `EnsureWorkspace` | clone (optional) → create worktree if missing → provision (filecopy + templates) |
| `Launch` | resolve path → resolve agent → start session → attach (optional) |
| `StopSessions` | list handles matching `ref` → stop each **by ID** |
| `Teardown` | `StopSessions` → delete worktree |
| `List` | list worktrees → list sessions → join on `Ref` |
| `Resolve` | find the live handle for `ref` + type |

`EnsureWorkspace` and `Launch` stay separate. `ch create session` calls both. Two lines of composition per front end beats a `LaunchOpts` carrying a worktree-creation sub-struct, and the TUI needs `EnsureWorkspace` alone for its create-worktree form.

### 7.1 Every divergence becomes a named argument

`herd` owns mechanics; front ends pass policy explicitly. Behaviours that differ between CLI and TUI today become visible arguments rather than being silently reconciled.

| Divergence today | Named as | CLI | TUI |
|---|---|---|---|
| TUI auto-clones on attach; CLI never does | `EnsureOpts.AutoClone` | `false` | `true` |
| TUI hardcodes `Force: true`; CLI honours `ErrSessionRunning` | `TeardownOpts.Force` | `--force` | `true` |
| `--from` / `--track` | `EnsureOpts.StartPoint` / `.Track` | flags | form fields |
| `--shell` | `StopOpts.Type` | flag | menu choice |
| `--attach` | `LaunchOpts.Attach` | flag | always |

## 8. Data flow

### 8.1 Composition root

The only place any service is constructed:

```go
// cmd/root.go
var h *herd.Herd     // replaces the cfg + registry globals

PersistentPreRunE: func(c *cobra.Command, args []string) error {
    cfg, registry, err := config.Load(cfgFile, resolveProfileArg(profileFlag))
    if err != nil {
        return fmt.Errorf("loading config: %w", err)
    }
    h = herd.New(cfg, registry.Active, herd.Deps{
        Tmux: tmux.NewRealRunner(),
        Git:  git.NewRealRunner(),
    })
    return nil
}
```

`cmd/tui.go` passes `h` to `tui.NewModel` instead of three services. The TUI's `profileCache` of rebuilt service bundles (`model.go:605-626`) becomes `h.WithProfile(name)`.

### 8.2 `ch create session myapp feat --agent claude`

Today: `cmd/session.go:160-285`, roughly 125 lines, with copy and template blocks near-verbatim duplicated from `cmd/worktree.go:133-160`. After:

```go
func (c *CreateSessionCmd) Run(cmd *cobra.Command, args []string) error {
    ref := h.Ref(args[0], args[1])

    if _, err := h.EnsureWorkspace(ref, herd.EnsureOpts{
        AutoClone: false,   // CLI never auto-clones — previously implicit, now stated
        Provision: true,
    }); err != nil {
        return herdErr(cmd, err)
    }

    handle, err := h.Launch(ref, herd.LaunchOpts{Type: c.sessionType(), Agent: agentName, Attach: c.Attach})
    if err != nil {
        return herdErr(cmd, err)
    }
    fmt.Fprintf(cmd.OutOrStdout(), "Started %s\n", handle.Ref.CanonicalName())
    return nil
}
```

The template `SessionName` divergence dies here: `Provision` builds `ProcessContext` from `ref` in one place, so no site can pass `""` while another passes the profile.

### 8.3 The TUI delete that started this

```go
func (m Model) confirmDeleteAll() (tea.Model, tea.Cmd) {
    ref := m.confirm.target.Ref     // identity from herd.List — never a display string
    h := m.herd
    m.confirm, m.screen = nil, screenList

    return m, func() tea.Msg {
        if err := h.Teardown(ref, herd.TeardownOpts{Force: true}); err != nil {
            return errMsg{err: err}
        }
        return m.refreshCmd()()
    }
}
```

`Teardown` lists handles by `ref`, stops each by ID, then deletes the worktree. One kill loop, profile-correct by construction. Today there are two, and one is always dead.

### 8.4 TUI refresh

`refreshCmd` (~100 lines, `model.go:462-559`) and `buildItems` (~95 lines, `items.go:73-168`) collapse to `h.List("")`. `items.go` stops deriving identity and becomes a mapping from `Workspace` to render rows.

### 8.5 Expected scale

| File | Now | After (est.) |
|---|---|---|
| `internal/tui/actions.go` | 477 | ~250 |
| `internal/tui/model.go` | 695 | ~570 |
| `cmd/session.go` | 376 | ~200 |
| `cmd/worktree.go` | 250 | ~150 |
| `internal/herd/*` | — | ~1,550 |

Net: roughly −600 lines in the front ends; `herd` absorbs ~1,200 lines of existing logic plus ~350 of orchestration.

## 9. Error handling

**One vocabulary.** All sentinels move to `herd`. Today they span three packages, and `cmd/errors.go` has two translators that both handle `worktree.ErrNotCloned` while printing different text for it (`errors.go:16` vs `:44`). One package yields one translator.

**Fix the `os.Exit` wart.** `cmd/errors.go` calls `os.Exit(1)` inside `RunE`, making the trailing `return nil` unreachable and bypassing `Execute`'s error printing (`root.go:74-77`). Call sites read `return worktreeErr(cmd, …)` but never return. It becomes a translator that returns an error and lets Cobra exit.

**The TUI stops rendering raw errors.** `internal/tui` contains no `errors.Is` today; it prints `msg.err.Error()` where the CLI prints "Run `ch clone project X` first". One vocabulary lets the TUI match the same sentinels.

`herd` never formats user-facing text. Presentation stays in each front end.

## 10. Testing

**Existing tests migrate largely intact.** `session` (91%) and `worktree` (89.3%) already mock at the `Runner` seam per `CLAUDE.md`, and the collapse does not move that seam. Coverage carries across rather than being rewritten — this is what makes the restructure tractable.

**Unit.** `herd` tests fake `tmux.Runner` and the git runners. No service-level mocks: the layer that would be mocked is now the code under test.

**Integration — the coverage contract.** Every operation runs with profiles on *and* off. This matrix is the gate that would have caught the original defect.

| | profile off | profile on |
|---|---|---|
| `EnsureWorkspace` | covered | covered |
| `Launch` | covered | covered |
| `List` | covered | covered |
| `StopSessions` | covered | **gap today** |
| `Teardown` | covered | **gap today** |
| `Resolve` | covered | **gap today** |

The two regression tests on this branch (`internal/tui/delete_teardown_test.go`) carry forward against `Teardown`.

`make check` — 80% coverage floor, integration, lint, build — gates every stage.

## 11. Decisions and rejected alternatives

| Decision | Rejected alternative | Why |
|---|---|---|
| Collapse the primitives into `herd` | Add `herd` as an orchestration layer *above* `session`/`worktree`/`project` | The layered version preserves the boundary that caused the defect. It also required nine consumer-defined interfaces, a `hooks.NewResolver` dispatcher, and forcing `Ref` into `semconv` to dodge adapter types — all accidental complexity defending a layer that should not exist. |
| Fold `project` in too | Keep `project` out (it never imports `tmux`) | Symmetry and one error vocabulary. `project` shows no entanglement, so this is the weaker half of the decision; it is reversible. |
| `Ref` with exported fields + convention | Opaque `Ref` (unexported fields, compiler-enforced) | Opaque fields force every TUI render test to build a `Herd` with fake runners just to mint a `Ref`. That ceremony suppresses tests. The convention is also much stronger than the one that failed (§6.1). |
| `herd.Herd` | `herd.Env` | "Env" reads as environment variables. The stutter rule targets redundant prefixes (`http.HTTPServer`), not a package's central type — cf. `time.Time`, `url.URL`, `template.Template`. Call sites read `h := herd.New(…); h.Teardown(…)`. |
| `Deps` struct | Functional options | Two fields. Revisit at three. |
| ~13 methods on `Herd` | Split into several types | Accepted with reservation: roughly one method per CLI verb. Revisit if it smells in practice. |
| `internal/herd` | `herd/` (importable) | No extension model exists yet (YAGNI). `internal` → public is a rename; public → `internal` breaks downstream builds. Starting internal keeps the door open at zero cost. |
| `SetStatus(name, …)` addresses by name | Address by `Ref` | `plugin handle-claude` receives a bare canonical name from `$CODEHERD_SESSION` and cannot recover a `Ref`: the profile prefix is ambiguous (`work-myapp-feat` could be profile `work` + project `myapp`, or a project literally named `work-myapp`). One narrow escape hatch beats re-exporting name resolution. |

## 12. Implementation plans

The work splits into **three plans, written and executed in order, each in its own session**. Each delivers working software and ends with `make check` green. Written to full granularity in one document, the whole refactor runs past 2,000 lines and 30 tasks — too large to review carefully, and the later stages depend on what the earlier ones learn.

Each plan lands at `docs/superpowers/plans/YYYY-MM-DD-<name>.md`. **Record what you learn in §14 before writing the next plan** — that section is the handoff between sessions.

| Plan | Status | Scope | Done when |
|---|---|---|---|
| **1 — the collapse** | done | Create `internal/herd` and `internal/git`. Move project, then session, then worktree logic in. Delete `internal/project`, `internal/session`, `internal/worktree`. Add `Ref`, `Herd`, `Deps`, `New`, and the six operations. Migrate callers per domain as each moves. | The three packages are gone, `cmd`/`tui` compile against `herd`, the defect is dead structurally, coverage holds at ≥80% |
| **2 — front-end thinning** | not started | Reduce `cmd/` and `internal/tui/` to parse → call → render. Delete `cmd/services.go`'s `*ForProfile` shims, the dead `Model.sesSvc`/`Model.projSvc` fields, and the `profileCache`. Collapse `cmd/errors.go` to one translator and fix the `os.Exit`-inside-`RunE` wart (§9). Teach the TUI to match sentinels instead of printing raw errors. | ~600 fewer lines across the front ends; no front end constructs a service or builds a session name |
| **3 — the coverage contract** | not started | Fill the profile × operation integration matrix (§10). | The three gap cells — `StopSessions`, `Teardown`, `Resolve` under an active profile — are covered |

### 12.1 Build order within Plan 1

**Build order is project → session → worktree**, which reverses the order these packages are listed elsewhere in this document. Dependencies decide it:

- `project` has none — it never imports `tmux`. It moves first and cleanest.
- `session` needs `Ref` only.
- `worktree` needs both: `Teardown` calls the session code, and `EnsureWorkspace`'s `AutoClone` calls `Clone`.

**The shipped defect dies at the end of the worktree stage**, once `Delete` sits beside the session code and can call it directly with the profile in hand. That is roughly two thirds of the way through Plan 1, not at the end of the project.

Migrate callers as part of each domain's stage rather than deferring migration to Plan 2. Otherwise `cmd`/`tui` get touched twice per domain — once to rename an import, once to migrate for real — and the intermediate state needs temporary exported service types that exist only to be deleted.

Stages are pure moves wherever possible: the existing `session` (91%) and `worktree` (89.3%) tests already mock at the `Runner` seam, which does not move, so they carry across rather than being rewritten. **A stage that rewrites tests instead of moving them is a signal the stage is doing too much.**

## 13. Risks

- **Scope.** Roughly 1,200 lines of logic plus their tests move. This restructures the core of a daily-driver tool. Mitigated by staging and by tests that move rather than get rewritten.
- **Package size.** `herd` becomes the largest package at ~1,550 lines across ~6 files. Nothing inside it enforces boundaries; discipline replaces the compiler. Accepted, because the enforced boundary we have today is what caused the defect.
- **`project` folding is the weakest link.** It shows no entanglement. If `herd` grows unwieldy, extracting it again is the first cut to consider.

## 14. Handoff notes

**This section is the handoff between sessions.** Each plan is written and executed in a fresh session that has only this document as context. Record here anything discovered during execution that changes what a later plan should do. Append; do not rewrite history.

**Read this section before writing any plan.** A note here overrides the design above — the design is what we predicted, these notes are what we found.

Worth recording:

- **Assumptions this document got wrong.** Any claim in §2–§11 that execution disproved. Say which section, so the next session distrusts the right paragraph.
- **API changes.** The surface in §6 is a sketch, not a contract. If a signature changed, record the real one — the next plan will code against it.
- **Decisions reversed.** §11 lists what we rejected and why. If execution forced a reversal, note which row and what happened.
- **Traps.** Anything that cost more than ~30 minutes, especially cross-layer surprises: tmux behaviour, git worktree edge cases, Cobra lifecycle, test isolation.
- **Deferred work.** Anything consciously skipped, and which plan should pick it up.

### 14.1 After Plan 1 — the collapse

Plan 1 is **done** (§12). `internal/project`, `internal/session`, and
`internal/worktree` are gone; `cmd` and `internal/tui` compile against
`internal/herd` + `internal/git`; the shipped profile-blind defect is dead
both structurally and end-to-end (see behaviour changes below). This section
is curated across Tasks 1–5, organised by category rather than by task —
Plan 2's author cares what is true now, not which stage found it.

#### What this task can measure (the contract, proved)

Step-1 greps, re-run at close-out (`*.go` only):

- `git grep -n 'internal/worktree\|internal/session\|internal/project'` → **no hits in Go code.** The three packages have no importers. (CLAUDE.md and the frozen historical docs/plans still name them; that is intended history.)
- `git grep -n 'semconv.SessionName'` → **one production caller: `internal/herd/herd.go`** (2 uses, both inside `Ref.CanonicalName`/session-name derivation). The other two hits are tests asserting *on* the name — `internal/semconv/semconv_test.go` (testing `semconv` itself) and `cmd/session_internal_test.go:42` (recomputing the expected canonical name). This is the whole plan in one command: the nine profile-blind `SessionName("", …)` literals are gone, with nowhere to come back to.
- `git grep -rn 'tmux.NewClient(tmux.NewRealRunner())'` → **one site: `cmd/session.go:24`** (the plan guessed `cmd/tui.go` — wrong). One inline tmux client remains; a candidate for Plan 2's front-end thinning.
- `git grep -rn 'hooks.New(' -- cmd internal/tui` → **one site: `cmd/template.go:72`**, legitimately surviving (see "What Plan 2 inherits"). Not a leak.
- `find internal/herd -name '*.go' -not -name '*_test.go' | xargs wc -l` → **1,036 non-test lines**, not the ~1,550 §5/§8.5 predicted. The estimate was high by ~one third. File split: `session.go` 309, `workspace.go` 375, `herd.go` 158, `project.go` 83, `paths.go` 69, `errors.go` 42.
- `make check` → **green, aggregate coverage 84.5%** (`internal/herd` 87.6%, `internal/git` 90.1%, `cmd` 75.3%, `internal/tui` 84.3%).

#### Assumptions §2–§11 got wrong

- **§5 "13 methods" on `WorktreeRunner` — actually 12**, so `git.Runner` is a **13-method** union, not 14. (The plan's own File-Structure table still calls it a "14-method union" — same miscount; the real number is 13.) The §14.1 standing prompt that asked whether "a 14-method union caused pain in test fakes" was asking about the wrong number; the shared `fakeGit` satisfies the 13-method union without pain.
- **§5/§8.5's ~1,550-line `herd` estimate was high** — real is 1,036 non-test lines (above).
- **§6's `New(cfg, profile string, deps)` is wrong** — the real signature is `New(cfg, registry, deps)` (see the deviations table). §8.1's own `herd.New(cfg, registry.Active, …)` sample nil-panics when profiles are off. `cmd.activeProfile()` was deleted as a consequence.
- **§6's "hooks does not appear at all" is half-right** — hooks stay out of the *exported* API (which is what §3.2 actually requires), but an unexported `newHook func(config.HooksConfig) hooks.Hook` field on `Herd`, defaulted in `New` and test-overridable, resolves them per operation.
- **§13 called `project` "the weakest link… the first cut to reverse." Execution disagreed, and the disagreement is preserved on purpose.** Task 3 found folding `project` in *earned its place*: because `Clone` resolves its own hook from `cfg` via `hookFor` (rather than taking a `hooks.Hook` at construction), `cmd/project.go`, `cmd/tui.go`, and all three TUI clone sites (`actions.go`, `form.go`, `agent_picker.go`) collapsed to a single `h.Clone(name)` — "nothing here suggests project should be the first cut to reverse." §13's stance (weakest link, no entanglement, first to extract if `herd` grows unwieldy) still stands as written; a later session should weigh both. `herd` came in *smaller* than §13 feared (1,036 vs ~1,550), which weakens §13's "grows unwieldy" trigger but does not settle the design question.

#### API changes vs the §6 sketch

The plan decided these up front and required Task 6 to copy the table into this section verbatim:

| Spec §6 says | Plan 1 does | Why |
|---|---|---|
| `New(cfg *config.Config, profile string, deps Deps)` | `New(cfg *config.Config, registry *config.ProfileRegistry, deps Deps)` | `WithProfile(name)` needs `ProfilesDir` to call `config.LoadProfile`, and only the registry has it. The spec's own §8.1 sample (`herd.New(cfg, registry.Active, …)`) nil-panics when profiles are off — `config.Load` returns a nil registry, which is exactly why `cmd/services.go:37` guarded it. Passing the registry makes `New` total and deletes `activeProfile()`. |
| `hooks` does not appear anywhere | Unexported field `newHook func(config.HooksConfig) hooks.Hook`, defaulted in `New` | §3.2's constraint is that hooks must not be **bound at construction** — that is what created the dead `Model.sesSvc` fields. A defaulted, test-overridable field satisfies that and keeps the 8 existing hook tests moving intact (§10) instead of being rewritten against real shell commands. It stays out of the exported API. |
| `Handle` has a `Ref` | Same, plus `@codeherd_project` is stamped as a new tmux option | Without it, `Ref.Project` cannot be recovered from a tmux record (the canonical name is ambiguous — §11), so `Sessions()` would return `Handle`s with a half-populated `Ref`. A `Ref` missing only `Project` is a footgun that compiles into `Teardown`. One extra `SetOption` + one format field removes it. |
| — | `Project(name string) (Project, error)` added | `ch show project <name>` needs one project with `Cloned` status. §6 listed only `Projects()`. |
| — | `CloneAll` dropped, not moved | `project.Service.CloneAll` has **zero non-test callers** — `cmd/project.go` runs its own loop over `h.Projects()`. YAGNI. |
| `RemoteBranch` declared in `herd` | Declared in `internal/git`, re-exported from `herd` as a type alias | `git.Runner.ListRemoteBranches` returns it, so declaring it in `herd` would force a conversion loop at the exec boundary. `type RemoteBranch = git.RemoteBranch` gives §6's surface for free. |
| Files: `session.go worktree.go project.go launch.go teardown.go list.go` | `herd.go errors.go paths.go project.go session.go workspace.go` | Launch/Teardown/List are each ~40 lines and belong beside the domain they operate on. Six files either way. |

Further surface facts recorded during execution:

- `ListSessions` widened **9→10 fields** with `Project: fields[9]` appended last, backing the new `SessionRecord.Project` + `semconv.TmuxOptionProject`, so a `Handle` from a list carries a complete `Ref` (`TestSessions_rebuildsCompleteRef`). Field appended last so pre-upgrade 7/8/9-field lines still parse.
- `session.Service` folded in as `Launch`/`Resolve`/`Sessions`/`StopSessions`/`SetStatus`; the `ShowByName`/`StopByName` name-addressed escape hatch is **gone** — the six copies of the list-and-match loop collapse to one `handles()`.
- `EnsureWorkspace` collapsed `New`/`NewFrom`/`NewTracking` into one method + an `addWorktree` switch (`Track` → `git.AddTracking`; `StartPoint` → `freshenStartPoint` + `AddNewBranchFrom`; default → `Add` w/ fallback). `EnsureOpts.Track`/`.StartPoint` are mutually exclusive. With `Track`, **`Workspace.Ref` is authoritative** (the local branch is derived from the remote ref — assert on `ws.Ref.Branch`, not the input Ref).
- `RemoteBranches(project, fetch bool)` replaced `ListRemoteBranches` (no fetch) + `RemoteBranches` (fetches first) — one method, one named argument.
- `git.ParseRef` is exported (Task 5's `freshenStartPoint` lives in `herd` and needs it).
- The TUI `Item` now carries `Ref herd.Ref` (identity), populated from `ws.Ref` — the sanctioned way the front end keeps identity/display split.

#### Decisions reversed / confirmed (§11)

- **§11's exported-field `Ref` held up — no reversal.** The only hand-built `herd.Ref{…}` literals are in `_test.go` fixtures and in `handleFrom`/`workspaceFrom` inside `herd`, which mint the Ref from tmux/git data (the sanctioned internal seams). No front end builds a Ref by hand; they carry `ws.Ref`/`sel.Ref` or call `h.Ref`. The opaque-Ref alternative §11 rejected stays rejected.
- The ~13 `Herd` methods "still felt right after writing them all" (Task 5) — `EnsureWorkspace`/`Provision`/`List`/`Teardown`/`RemoteBranches` sit naturally beside `Launch`/`StopSessions`; `Teardown` calling `StopSessions` with the profile in hand is the whole point (one kill loop, keyed on identity).

#### Traps (cost real time; a later session should expect them)

- **Task 1 — package-scope `git` identifier collision.** `internal/worktree/integration_test.go` declared a helper `func git(t, …)` that collided with the new `internal/git` import used elsewhere in the package (Go rejects that). Renamed to `runGit`. `goimports` also reflowed `CloneRunner` to a multi-line block — kept, since the lint gate enforces gofmt.
- **Task 2 — `golangci-lint`'s `unused` analyses test + production files together.** The shared `fakes_test.go` tripped 23 `unused` issues while no test consumed the fakes yet; it was **deferred to Task 3** (created there once real callers existed). `go vet`/`go build` do *not* flag this — only the linter does. Same gate flagged `paths.go`'s `worktreesRoot` (no caller yet); covered with a small added test rather than a nolint. Also: `TestHookFor_defaultsToConfiguredHooks` in the brief was unfalsifiable (`hooks.New` never returns nil) — rewritten to override `newHook` with a capturing func and assert the exact threaded `HooksConfig`.
- **Task 4 — `Launch` derives `Path`/`CloneDir` from the `Ref`.** Callers no longer pass a path, so every test that fed an arbitrary path had to `os.MkdirAll` the *derived* worktree path (`<projects_dir>/<repoPath>__worktrees/<flatBranch>`) or `Launch`'s `os.Stat` returns `ErrPathNotFound`. Helpers: `mkMyappWorktree`, `tuiHerd`.
- **Task 4 — hooks-shadowing footgun.** `cmd/session.go` and `cmd/worktree.go` both had a local `h := hooks.New(projCfg.Hooks)` shadowing the package-global `h *herd.Herd`; once those funcs call `h.Launch` the shadow breaks the build. Renamed the locals to `projHook`. Related: CLI shim callers read both `h` and `cfg`, so test helpers must build both from the same config (`setHerdTmux`).
- **Task 4 — the tmux 9→10 widening must land before any test sets `sessionRow.Project`/`fakeTmux.Sessions` with a project.**
- **Task 5 — completion runs without `PersistentPreRunE`, so the package-global `h` is nil there.** Added `ensureCompletionHerd(cmd)` at the top of `completeBranches`/`completeRemoteBranches`; without it, dropping the `cfg` param off the completion seams nil-panics real shell completion. (The internal `cmd` tests bypass `PersistentPreRunE` too — a `TestMain` seeds a default nil-registry `h`.)
- **Task 5 — identity comes from the directory name, not live HEAD.** `WorktreeIdentityBranch` returns `filepath.Base(path)` for non-main worktrees and the configured default branch for the clone dir, which is exactly what survives a diverged HEAD.
- Tasks 1–2 surfaced little else worth flagging beyond the above; Tasks 3–5 carried the substance.

#### Behaviour changes (also the changelog entry — 10 total)

1. **Deleting a worktree or session under an active profile now kills the agent process too.** The shipped defect: a profile-prefixed session (and its running agent) used to survive the delete. Verified end-to-end — under `CODEHERD_TMUX_SOCKET` with profile `work` active, `ch create session myapp feat` then `ch delete worktree myapp feat --force` leaves the agent PID dead and the worktree gone.
2. **`ch list worktree` now shows `(running)` for sessions under an active profile.** The marker was previously hardcoded to the no-profile session name, so it never appeared once a profile prefixed the real name.
3. **`ch delete session --force` no longer errors when nothing is running.** Stopping nothing is treated as success; the "not found" message now comes only from the non-`--force` probe.
4. **`ch create worktree --attach` now starts a profile-prefixed session.** It never did — the old path passed no profile, creating an unprefixed session nothing else could address.
5. **`ch create session <uncloned-project>` now reports "not cloned" first** (agent resolution moved after the worktree-existence check), exits via the same `worktreeErr` path as `create worktree` instead of returning, and prints its "creating…" banner only on the successful path. Exit code unchanged (1).
6. **The TUI create-worktree form now provisions (copies files, renders `.herd` templates) even when you do not attach.** Previously `attach=false` created a bare worktree with nothing rendered — a latent defect fixed during the collapse. Create-then-attach now provisions in exactly one place (no double render).
7. **Sessions created before this release (missing the `@codeherd_project` stamp) are recognized, listed, killed, and healed automatically.** The domain matches live sessions on the frozen `@codeherd_canonical_name` (the identity of record every prior version used), not on a name rebuilt from `Ref` parts, so a missing project can no longer hide a session from the TUI or a teardown. On first observation the missing project is recovered from the canonical name and re-stamped, healing the session to first-class. No orphans, no manual migration.
8. **Re-tracking an already-tracked worktree now fetches first and returns a raw "local branch exists" error** instead of the friendly `ErrWorktreeExists`. Edge case (`worktreeErr` does not translate this sentinel).
9. **`delete worktree` / delete-all now stat-gate on the worktree directory before killing sessions.** A session whose worktree was removed out-of-band now survives a delete-all (recoverable via agent-only `delete session`).
10. **The TUI now renders a diverged worktree's live HEAD branch** (`DisplayBranch`) plus an "on `<branch>`" hint, where it used to show the identity branch. Intentional display/identity split; `ch list worktree` CLI output is unchanged.

#### What Plan 2 inherits (read this first)

- **`cmd/errors.go` still has its two translators (`sessionErr`/`worktreeErr`) and its `os.Exit(1)` inside `RunE`.** Plan 2's §9 target: collapse to one translator and remove `os.Exit` from `RunE`. Deliberately untouched here — changing exit codes mid-collapse, while the domain underneath still moved, was out of scope.
- **`cmd/template.go` keeps its own `hooks.New` (line 72) and its direct `herdtemplate` call** — the one front end that legitimately does not route through `herd`, because `ch template` takes an arbitrary `[dir]` and supports `--dry-run`, neither of which fits `Provision`'s premise that paths derive from the `Ref`. Only its profile-blind `semconv.SessionName("", …)` was fixed in place. Leave it, or give `herd` a dir-taking provision variant if Plan 2 wants full routing.
- **One inline `tmux.NewClient(tmux.NewRealRunner())` remains at `cmd/session.go:24`** — the last front-end site building a tmux client by hand. Candidate for Plan 2's thinning.
- **`Model.cfg`:** the TUI `profileCache` and the shared-registry mutation were **deleted, not moved** — `switchProfile` now calls `m.herd.WithProfile(next)` (one small TOML read per keypress; `m.herd` is an immutable value so the old refresh/profile-snapshot race is structurally gone). Whether `Model.cfg` itself can now go away entirely is unverified and left for Plan 2 to measure; if the per-keypress read ever bites, the cache belongs on `Herd`, not the TUI.
- **Front ends are not yet thin** (§8.5) — `cmd` is 75.3% covered and still constructs a tmux client and a `hooks.New`. Plan 2 should re-measure line counts rather than trust §8.5's estimates (which already proved high for `herd`).
- **The profile × operation integration matrix is still unfilled** (§10's three gap cells: `StopSessions`, `Teardown`, `Resolve` under an active profile). They have *unit* coverage here (Tasks 4–5); *integration* coverage against real tmux is Plan 3, and is the gate that would have caught the original defect.


### 14.2 After Plan 2 — front-end thinning

**Scope correction.** §12's "~600 fewer lines of thinning" was already delivered by Plan 1
(`cmd/services.go` + `*ForProfile` shims deleted, `Model.sesSvc`/`projSvc` gone, `profileCache`
gone, front ends already at §8.5 targets). The only §9 work left was the **error vocabulary**,
which is all Plan 2 did — two tasks, commits `d5e97ad` (CLI) and `02f2c65` (TUI). Base `4d0f413`.

**What landed:**
- `cmd/errors.go`'s `worktreeErr`+`sessionErr` collapsed into one `herdErr(project, branch, err) error`
  that **returns** the friendly error. `Execute` (`cmd/root.go`) is now the single print site,
  prefixing every user-facing error `Error: `. All 9 call sites migrated; the worktree `--attach`
  site passes `ws.Ref.Project/Branch` (authoritative identity), not the positional branch.
- `internal/tui/errors.go`'s `humanize(err) string` maps the same sentinels to concise status
  lines; the TUI's two render sites (`statusMsg`, `remotePicker.errText`) route through it.
- Coverage 85.3% throughout; final whole-branch review (`4d0f413..02f2c65`): **Ready to merge, Yes.**

**Answering the standing prompts:**
- **Exit codes unchanged** — removing `os.Exit(1)` from `RunE` moved the exit to `main.go` (already
  exits 1 on non-nil `Execute` return). The *observable* change is a gain: config-load and other
  `RunE` errors now also get the `Error: ` prefix, and each error prints exactly once (the old
  translators printed *and* `os.Exit`ed, bypassing `Execute`). No double-print (the `--all` clone
  path prints its own per-project line then returns a distinct summary error).
- **Line counts** — front ends were already at/near §8.5 before this plan; Plan 2 removed the
  translator duplication but was not a bulk-thinning plan. No re-measurement needed.
- **`WithProfile` vs `profileCache`** — already answered by Plan 1; `switchProfile` uses
  `m.herd.WithProfile(next)`, cache gone. Not touched here.

**Measured non-goals (deliberately kept, see plan §"Measured non-goals"):**
- `Model.cfg` stays — removing it forces ~30 TUI test sites to build a real `Herd` (the ceremony
  §11 rejected). It is a redundant cache resynced at the only two herd-swap sites.
- The inline `tmux.NewClient` attach sites (`execTmuxAttach`, `switchClientCmd`) stay — they are
  interactive-attach exec mechanism (cf. `syscall.Exec`), not the service-injection smell of §3.2.

**Accepted Minors / follow-up candidates (none block merge):**
1. Stale comments still name the deleted `worktreeErr`/`sessionErr` and old `os.Exit` behavior in
   `cmd/worktree_test.go:110`, `cmd/session_internal_test.go:253`, `cmd/session_test.go:88,109-112`.
   Comment-only; a fast-follow sweep would stop pointing future readers at gone symbols.
2. Two defensive fallback branches are effectively **dead**: `herdErr`'s `ErrSessionExists`→`return err`
   and `humanize`'s `"Session already exists."`. `session.go` only ever returns `*SessionExistsError`
   and the bare sentinel is never wrapped-and-returned, so `errors.As` always succeeds. Harmless.
3. **The "one vocabulary" goal is not yet total across `cmd`.** `list worktree` (`cmd/worktree.go:35`)
   wraps as `fmt.Errorf("list: %w", err)`, and the clone paths handle `*AlreadyClonedError` with their
   own `Warning:`/`Error:` lines — neither routes through `herdErr`. Correctly out of this plan's 9
   sites; a candidate for Plan 3 or a cleanup if total coverage is the end state.
4. `herd.ErrAlreadyCloned` / `ErrLocalBranchExists` are unmapped in both front ends (fall through to
   the raw sentinel text). Assessed acceptable: behavior is consistent CLI↔TUI, and the clone flows
   intercept `*AlreadyClonedError` upstream. Polish only.

### 14.3 After Plan 3 — the coverage contract

**Done.** The §10 matrix is filled at the `internal/herd` layer by one new file,
`internal/herd/matrix_integration_test.go` (`//go:build integration`, package `herd`), across
three commits — base `4b03da3`, range `4b03da3..aed43fd`. Test-only; **zero production changes.**
Final whole-branch review: **Ready to merge, Yes.**

**Scope correction.** §12 framed Plan 3 as three gap cells. Verified: the profile-scoped operations
already had *unit* (fake-tmux) coverage from Plan 1 (`TestStopSessions_underProfile_*`,
`TestTeardown_underProfile_*`, `TestList_underProfile_*`) — the "unit coverage" §14.1 named. The real
gap was *integration* (real tmux): **no test anywhere built `herd.New(…, Deps{Tmux: tmux.NewRealRunner()})`.**
Plan 3 is the first, and is the layer §10's matrix names (herd operations), one level below §3.3's
CLI-level `TestProfiles_sessionIsolationAcrossProfiles` (which covers create+list through Cobra).

**What landed** — a two-column table `matrixProfiles` = {profile off (nil registry), profile on
(`&config.ProfileRegistry{Active:"work"}`)} driven through an isolated real tmux + real git harness:
- `TestMatrix_LaunchAndResolve` — Launch + the **Resolve** gap cell.
- `TestMatrix_StopSessions` — the **StopSessions** gap cell (agent + shell, `StopOpts{All}`).
- `TestMatrix_Teardown` + `TestMatrix_TeardownRefusesRunning` — the **Teardown** gap cell, the shipped
  defect's row: force teardown kills the profile-prefixed session AND removes the worktree; non-force
  refuses with `ErrSessionRunning` and touches nothing.

**Answering the standing prompts:**
- **Did it find bugs, or confirm?** Confirmed. All 12 subtests pass on tmux 3.4; the shipped
  orphaned-agent defect (§2/§8.3) does **not** reproduce under a profile — Plan 1's fix is now proven
  end-to-end against real tmux, not just fakes. The review verified the assertions would genuinely
  *fail* if the profile-blind name rebuild were reintroduced, so it is a live gate, not a no-op.
- **Cheap enough to keep green?** Yes. Each subtest spins a private tmux server (socket under
  `t.TempDir()`) + a real git clone + a `sleep 300` agent, all reaped by a `kill-server` cleanup; no
  `t.Parallel`, no cross-test leakage or ordering dependence. It runs only in `make check`'s
  `test-integration` phase and `t.Skip`s where tmux can't daemonize (sandboxed CI). The `//go:build
  integration` tag keeps it **out of the coverage phase**, so the 80% floor is untouched (verified:
  `go test ./internal/herd/` without the tag finds none of these tests).
- **Promote to a CLAUDE.md standing rule?** The final review recommends it — the file cleanly encodes
  the "every operation × profile off/on, keyed on identity" contract. Deferred as a post-merge
  decision; a one-line rule ("new `herd` operations that touch tmux get a `matrixProfiles` row") would
  keep the matrix from rotting. Not done here (out of Plan 3's test-only scope).

**Accepted Minors (none block merge):**
1. Shell name is hand-built as `ref.CanonicalName() + "~sh"` rather than `semconv.ShellSessionName(...)`
   (`matrix_integration_test.go`); a following `t.Fatalf` precondition makes any drift fail loudly, so
   it's a maintainability nit. Optional: call `semconv.ShellSessionName` directly.
2. `tmuxHasSession` swallows the `exec` error, so an unreachable server reads the same as "no session";
   near-zero risk (harness pre-probes, server alive during assertions). Acceptable.
3. `setupMatrixHerd`'s worktree-path return is unused by Task 1's own test (used by the Teardown
   tests) — intended.

**The refactor is complete.** All three plans (collapse, front-end thinning, coverage contract) plus
the session-canonical-compat plan are done and merge-ready on this branch.

