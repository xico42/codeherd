# Pre-`@codeherd_project` Session Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-07-16-session-canonical-compat-design.md`. Read §3 (principle), §4 (the three layers), and §5 (testing) before starting.

**Goal:** Make the new binary recognize, kill, and heal tmux sessions that were created before the `@codeherd_project` option existed, by matching on the frozen `@codeherd_canonical_name` instead of a name rebuilt from `Ref` parts.

**Architecture:** The stored `@codeherd_canonical_name` is the session's identity of record. Task 1 makes every match key on it (the correctness guarantee). Task 2 adds best-effort project recovery and a one-time self-heal, folded into the single `handles()` chokepoint so the logic is single-sourced, plus the doc corrections that describe the now-complete behaviour.

**Tech Stack:** Go (module `github.com/xico42/codeherd`), tmux, stdlib `testing` with hand-written fakes at the `Runner` seam.

## Global Constraints

- **`make check` must pass before every commit.** It runs coverage (80% floor), integration tests, lint, and build. A task is not done until it is green.
- **Coverage floor: 80% aggregate.** New code carries tests in the same commit.
- **`herd` must never import `cmd` or `internal/tui`.**
- **Typed enums:** `SessionType` / `Status` stay defined types with named constants; no bare strings for them.
- **`wrapcheck` is enabled.** Every error crossing a package boundary is wrapped with `%w` and a context prefix. `_test.go` files are exempt.
- **`goimports` with `local-prefixes: github.com/xico42/codeherd`.** Import blocks are stdlib / third-party / codeherd.
- **All `herd` tests live in `package herd`** (internal), alongside the shared `fakes_test.go`.
- **This branch continues `chore/refactor-packages`** after the Plan 1 (herd collapse) commits. `internal/herd` already exists and is the only package touched here besides one comment in `internal/tmux` and the Plan 1 spec.

## Background — the exact defect

A session started by any pre-collapse binary carries `@codeherd_canonical_name` (e.g. `work-myapp-feat`), `@codeherd_profile`, `@codeherd_branch`, etc., but **not** `@codeherd_project` (added by the collapse). The new binary matches sessions by `hd.Ref.CanonicalName()`, which rebuilds the name from `Ref.{Profile,Project,Branch}`. With `Project == ""` that rebuild yields `work--feat` (empty project segment), which never equals the real stored `work-myapp-feat`. So the session is dropped from `List` and survives `StopSessions`/`Teardown` — an orphan. Matching on the stored canonical fixes it.

## File Structure

**Modified:**

| File | Responsibility |
|---|---|
| `internal/herd/session.go` | `Handle.Canonical` field; `handleFrom` gains the field (Task 1) then becomes a method with recover+heal (Task 2); `resolveProject` helper (Task 2); `Resolve`/`StopSessions` match on the stored canonical (Task 1). |
| `internal/herd/workspace.go` | `List`'s join keys on the stored canonical (Task 1). |
| `internal/herd/session_test.go` | The compat regression test (Task 1); `resolveProject` unit tests + recover/heal/idempotence tests (Task 2). |
| `internal/tmux/client.go` | `SessionRecord.Project` doc comment corrected (Task 2). |
| `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` | §14.1 behaviour #7 rewrite + drop the resolved Plan-2-inherits hardening note (Task 2). |

---

### Task 1: Match live sessions on the stored canonical name

The correctness guarantee. After this task, a pre-upgrade session (empty `Project`, real `Canonical`) is found by `Resolve`, killed by `StopSessions`, and joined by `List` — even though its project is still blank. No recovery or healing yet.

**Files:**
- Modify: `internal/herd/session.go` (`Handle` struct at 15-23; `handleFrom` at 296-308; `Resolve` at 183; `StopSessions` at 220 and 226)
- Modify: `internal/herd/workspace.go` (`List` join key at 262)
- Test: `internal/herd/session_test.go`

**Interfaces:**
- Consumes: `tmux.SessionRecord.CanonicalName` (existing); `Ref.CanonicalName()` (existing).
- Produces:
  ```go
  // Handle gains one field:
  type Handle struct {
      ID        string
      Canonical string // @codeherd_canonical_name — frozen identity, the match key
      Ref       Ref
      Type      SessionType
      TmuxName  string
      Status    Status
      Annotation string
      StartedAt time.Time
  }
  ```
  `Resolve`, `StopSessions`, and `List`'s join all match on `hd.Canonical`.

- [ ] **Step 1: Write the failing regression test**

Add to `internal/herd/session_test.go`:

```go
// A session created before @codeherd_project existed has a correct stored
// canonical name but an empty Project. It must still be found and killed —
// this is the exact orphan the collapse reintroduced.
func TestStopSessions_preUpgradeSession_matchedByStoredCanonical(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: ""},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	if _, err := h.Resolve(h.Ref("myapp", "feat"), SessionTypeAgent); err != nil {
		t.Fatalf("Resolve found nothing for a pre-upgrade session: %v", err)
	}
	if _, err := h.StopSessions(h.Ref("myapp", "feat"), StopOpts{}); err != nil {
		t.Fatalf("StopSessions: %v", err)
	}
	if got := f.killed(); len(got) != 1 || got[0] != "$1" {
		t.Errorf("killed = %v, want [$1] — the pre-upgrade session was not killed", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/herd/ -run TestStopSessions_preUpgradeSession_matchedByStoredCanonical -v`
Expected: FAIL — `Resolve found nothing` (the rebuilt name `work--feat` does not match the stored `work-myapp-feat`).

- [ ] **Step 3: Add the `Canonical` field to `Handle`**

In `internal/herd/session.go`, change the `Handle` struct (15-23) to add the field:

```go
type Handle struct {
	ID         string // tmux session_id ("$1") — stable across renames
	Canonical  string // @codeherd_canonical_name — the frozen identity, the match key
	Ref        Ref
	Type       SessionType
	TmuxName   string // current tmux name; may carry the ⚡ status prefix
	Status     Status
	Annotation string
	StartedAt  time.Time
}
```

- [ ] **Step 4: Populate `Canonical` in `handleFrom`**

In `handleFrom` (session.go:296), add the field to the struct literal:

```go
func handleFrom(r tmux.SessionRecord) Handle {
	hd := Handle{
		ID:         r.ID,
		Canonical:  r.CanonicalName,
		Ref:        Ref{Profile: r.Profile, Project: r.Project, Branch: r.Branch},
		Type:       SessionType(r.SessionType),
		TmuxName:   r.Name,
		Status:     Status(r.Status),
		Annotation: r.Annotation,
	}
	if r.StartedAt != "" {
		hd.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}
	return hd
}
```

- [ ] **Step 5: Match on `hd.Canonical` in `Resolve` and `StopSessions`**

In `Resolve` (session.go:183), change the match:

```go
	for _, hd := range all {
		if hd.Canonical == canonical && hd.Type == t {
			return hd, nil
		}
	}
```

In `StopSessions` (session.go:220 and the error at 226), change both:

```go
	for _, hd := range all {
		if hd.Canonical != canonical {
			continue
		}
		if !opts.All && hd.Type != opts.Type {
			continue
		}
		if err := h.tmux.KillSession(hd.ID); err != nil {
			return stopped, fmt.Errorf("killing session %s: %w", hd.Canonical, err)
		}
		stopped = append(stopped, hd)
	}
```

- [ ] **Step 6: Key the `List` join on `hd.Canonical`**

In `internal/herd/workspace.go`, change the join key (262) only. The lookup at 282-283 stays keyed on `ws.Ref.CanonicalName()` (the workspace always has a complete, real-project `Ref`, so its rebuilt name is correct):

```go
	byName := make(map[string][]Handle, len(sessions))
	for _, hd := range sessions {
		key := hd.Canonical
		byName[key] = append(byName[key], hd)
	}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/herd/ -run TestStopSessions_preUpgradeSession_matchedByStoredCanonical -v`
Expected: PASS.

Then run the whole herd package to confirm no existing match test regressed (new sessions have `Canonical == Ref.CanonicalName()`, so the switch is a no-op for them):
Run: `go test ./internal/herd/`
Expected: PASS.

- [ ] **Step 8: Verify and commit**

Run: `make check`
Expected: green, ≥80%.

```bash
git add internal/herd/session.go internal/herd/workspace.go internal/herd/session_test.go
git commit -m "fix: match live sessions on the stored canonical name

Resolve, StopSessions, and List's join keyed on a name rebuilt from Ref
parts, so a session created before the @codeherd_project stamp (empty
project segment) never matched its real stored name and survived a
delete. Match on the frozen @codeherd_canonical_name instead — the
identity every prior version used. No-op for sessions created by this
binary, where the stored and rebuilt names are identical."
```

---

### Task 2: Recover the project and self-heal, and correct the docs

Best-effort recovery for display, plus a one-time re-stamp so the session becomes first-class, plus the doc corrections that now describe the complete behaviour. All recovery/heal lives in the single `handles()` chokepoint via `h.handleFrom`, so every read path inherits it with no duplication.

**Files:**
- Modify: `internal/herd/session.go` (add `resolveProject`; `handleFrom` becomes method `h.handleFrom` with recover+heal; `handles` at 290 calls `h.handleFrom`)
- Modify: `internal/tmux/client.go` (`SessionRecord.Project` comment)
- Modify: `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md` (§14.1)
- Test: `internal/herd/session_test.go`

**Interfaces:**
- Consumes: `Handle.Canonical` (Task 1); `semconv.SessionName` (existing); `h.cfg`, `h.tmux`, `semconv.TmuxOptionProject` (existing).
- Produces:
  ```go
  // Pure, unambiguous project recovery from the frozen name:
  func resolveProject(cfg *config.Config, profile, branch, canonical string) (string, bool)

  // handleFrom is now a method so it can read cfg and stamp the heal:
  func (h *Herd) handleFrom(r tmux.SessionRecord) Handle
  ```

- [ ] **Step 1: Write the failing `resolveProject` unit tests**

Add to `internal/herd/session_test.go`:

```go
func TestResolveProject(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.ProjectConfig{
		"myapp": {}, "other": {},
	}}
	tests := []struct {
		name              string
		profile, branch   string
		canonical         string
		wantProj          string
		wantOK            bool
	}{
		{"under profile", "work", "feat", "work-myapp-feat", "myapp", true},
		{"no profile", "", "feat", "myapp-feat", "myapp", true},
		{"flattened slash branch", "work", "feat/login", "work-myapp-feat-login", "myapp", true},
		{"no configured match", "work", "feat", "work-nope-feat", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveProject(cfg, tt.profile, tt.branch, tt.canonical)
			if got != tt.wantProj || ok != tt.wantOK {
				t.Errorf("resolveProject = (%q, %v), want (%q, %v)", got, ok, tt.wantProj, tt.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/herd/ -run TestResolveProject -v`
Expected: FAIL — `undefined: resolveProject`.

- [ ] **Step 3: Add the `resolveProject` helper**

In `internal/herd/session.go`, add above `handleFrom`:

```go
// resolveProject finds the configured project whose canonical session name
// matches the stored one, given the (stored) profile and branch. Profile and
// branch are known exactly, so the project is the only unknown and the match
// is unambiguous. It validates against real config rather than string-
// splitting the name, so a project no longer in config yields "", false.
func resolveProject(cfg *config.Config, profile, branch, canonical string) (string, bool) {
	for name := range cfg.Projects {
		if semconv.SessionName(profile, name, branch) == canonical {
			return name, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/herd/ -run TestResolveProject -v`
Expected: PASS.

- [ ] **Step 5: Write the failing recover-and-heal tests**

Add to `internal/herd/session_test.go`:

```go
// A pre-upgrade session's project is recovered for display and re-stamped on
// the live session, so it heals to first-class on first observation.
func TestSessions_preUpgradeSession_recoversAndHealsProject(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: ""},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	sessions, err := h.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Ref.Project != "myapp" {
		t.Fatalf("Ref.Project = %q, want %q", sessions[0].Ref.Project, "myapp")
	}
	if !f.called("set-option", "@codeherd_project", "myapp") {
		t.Errorf("project was not re-stamped; calls=%v", f.Calls)
	}
}

// A session that already carries @codeherd_project is never re-stamped.
func TestSessions_stampedSession_isNotHealed(t *testing.T) {
	f := &fakeTmux{Sessions: []sessionRow{
		{ID: "$1", Name: "work-myapp-feat", Canonical: "work-myapp-feat",
			Type: "agent", Status: "running", Profile: "work", Branch: "feat", Project: "myapp"},
	}}
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: t.TempDir()},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	h := New(cfg, &config.ProfileRegistry{Active: "work"}, Deps{Tmux: f, Git: &fakeGit{}})

	if _, err := h.Sessions(); err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if f.called("set-option", "@codeherd_project") {
		t.Errorf("an already-stamped session was healed again; calls=%v", f.Calls)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/herd/ -run 'TestSessions_preUpgradeSession_recoversAndHealsProject|TestSessions_stampedSession_isNotHealed' -v`
Expected: FAIL — `Ref.Project = "", want "myapp"` (recovery not implemented; `handleFrom` still leaves the empty project as-is).

- [ ] **Step 7: Make `handleFrom` a method that recovers and heals**

In `internal/herd/session.go`, replace the `handleFrom` function with a method:

```go
func (h *Herd) handleFrom(r tmux.SessionRecord) Handle {
	hd := Handle{
		ID:         r.ID,
		Canonical:  r.CanonicalName,
		Ref:        Ref{Profile: r.Profile, Project: r.Project, Branch: r.Branch},
		Type:       SessionType(r.SessionType),
		TmuxName:   r.Name,
		Status:     Status(r.Status),
		Annotation: r.Annotation,
	}
	if r.StartedAt != "" {
		hd.StartedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}

	// Backward compatibility: sessions created before @codeherd_project existed
	// carry no project stamp. Recover it from the frozen canonical name and
	// stamp it, so the session heals to first-class on first observation.
	// Idempotent — once stamped, future reads take r.Project directly and skip
	// this path.
	if r.Project == "" && r.CanonicalName != "" {
		if project, ok := resolveProject(h.cfg, r.Profile, r.Branch, r.CanonicalName); ok {
			hd.Ref.Project = project
			_ = h.tmux.SetOption(r.Name, semconv.TmuxOptionProject, project)
		}
	}
	return hd
}
```

- [ ] **Step 8: Call the method from `handles`**

In `handles` (session.go:290), change the call:

```go
	for _, r := range records {
		if r.CanonicalName == "" {
			continue // not a codeherd session
		}
		out = append(out, h.handleFrom(r))
	}
```

- [ ] **Step 9: Run the recover-and-heal tests to verify they pass**

Run: `go test ./internal/herd/ -run 'TestSessions_preUpgradeSession_recoversAndHealsProject|TestSessions_stampedSession_isNotHealed' -v`
Expected: PASS.

Then the whole package:
Run: `go test ./internal/herd/`
Expected: PASS.

- [ ] **Step 10: Correct the `SessionRecord.Project` comment**

In `internal/tmux/client.go`, replace the `Project` field comment (the block ending in the "fails loudly" claim) with:

```go
	// Project is @codeherd_project — the project the session belongs to, ""
	// when unset. Sessions started before this option existed have no value
	// here; internal/herd recovers the project from the frozen canonical name
	// and re-stamps it on first observation (see herd.resolveProject), so such
	// a record heals to first-class rather than being orphaned.
	Project string
```

- [ ] **Step 11: Correct the Plan 1 handoff — spec §14.1**

In `docs/superpowers/specs/2026-07-15-herd-domain-package-design.md`, rewrite behaviour change #7 (currently describing pre-upgrade sessions as dropped / surviving teardown) to:

```markdown
7. **Sessions created before this release (missing the `@codeherd_project` stamp) are recognized, listed, killed, and healed automatically.** The domain matches live sessions on the frozen `@codeherd_canonical_name` (the identity of record every prior version used), not on a name rebuilt from `Ref` parts, so a missing project can no longer hide a session from the TUI or a teardown. On first observation the missing project is recovered from the canonical name and re-stamped, healing the session to first-class. No orphans, no manual migration.
```

Then delete the "What Plan 2 inherits" bullet that begins "**Pre-upgrade sessions could be recognized instead of silently dropped (behaviour change #7).**" — it is implemented here, no longer inherited.

- [ ] **Step 12: Verify and commit**

Run: `make check`
Expected: green, ≥80%.

```bash
git add internal/herd/session.go internal/herd/session_test.go internal/tmux/client.go docs/superpowers/specs/2026-07-15-herd-domain-package-design.md
git commit -m "feat: recover and self-heal the project on pre-upgrade sessions

A session created before the @codeherd_project stamp carries no project,
so its Ref rendered blank and could not be re-stamped. Recover the
project by matching the frozen canonical name against configured
projects, then re-stamp it on first observation so the session heals to
first-class. Recovery and healing live in the one handles() chokepoint
every read path funnels through, so the logic is single-sourced.

Corrects the SessionRecord.Project comment and Plan 1 handoff #7, which
described a 'fails loudly' guard the code never actually reached."
```

---

## Self-Review

**Spec coverage:**
- §3 principle (match on stored canonical) → Task 1 Steps 3-6.
- §4 Layer 1 (guarantee) → Task 1. Layer 2 (`resolveProject`) → Task 2 Steps 1-4. Layer 3 (self-heal with compat comment) → Task 2 Step 7. Reusability (single `handles()` chokepoint, `handleFrom` method) → Task 2 Steps 7-8.
- §5 testing: compat regression → Task 1 Step 1; `resolveProject` units → Task 2 Step 1; recover+heal+idempotence → Task 2 Step 5. tmux isolation is not needed — every test uses `fakeTmux`, no real tmux server.
- §6 docs (SessionRecord comment; §14.1 #7; drop the inherited hardening note) → Task 2 Steps 10-11.
- §7 boundary (project removed from config → lists blank, still killable): covered by construction — Task 1's match works without recovery, and `resolveProject` returning `false` (Task 2 Step 1 `no configured match` case) leaves `Ref.Project` blank with no heal write.

**Placeholder scan:** none — every step carries the actual code or command.

**Type consistency:** `Handle.Canonical` (Task 1 Step 3) is read in Task 1 Steps 5-6 and populated in Task 2 Step 7's method. `resolveProject(cfg, profile, branch, canonical)` signature (Task 2 Step 3) matches its call in Task 2 Step 7 and its tests in Step 1. `h.handleFrom` (Task 2 Step 7) matches its call in Step 8. `semconv.TmuxOptionProject` is the existing `"@codeherd_project"` constant asserted in the Step 5 tests via `f.called("set-option", "@codeherd_project", "myapp")`.

**Note for the executor:** Task 1 leaves `handleFrom` a free function; Task 2 converts it to a method. Do not merge the two — Task 1 is the independently reviewable correctness guarantee (matching), Task 2 is the recovery/heal layer on top.
