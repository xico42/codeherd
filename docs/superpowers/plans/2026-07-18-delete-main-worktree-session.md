# Session-only delete on the main worktree — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the TUI's `d` key kill the main worktree's agent/shell sessions (leaving the worktree in place), and give `ch delete worktree main` a clean refusal instead of a raw git error.

**Architecture:** The TUI stops hard-blocking `d` on the main worktree; instead it opens the confirm menu in a main-worktree mode that offers only session kills (never worktree removal). A new `deleteAllSessions` action stops every session for a ref via `StopOpts{All: true}` without calling `Teardown`. In the domain, `Teardown` gains an explicit `ErrMainWorktree` guard wired into both front-end error translators.

**Tech Stack:** Go, Cobra (CLI), Bubble Tea v2 (TUI), the `internal/herd` domain package.

## Global Constraints

- Coverage must stay ≥ 80% aggregate — run `make check` before completion (coverage → integration → lint → build).
- Never build a `herd.Ref` by hand in production code — use `h.Ref(project, branch)` or a listed `Workspace.Ref`. (Tests may construct `herd.Ref{...}` literals, as the existing TUI tests do.)
- One error vocabulary: herd owns the sentinels, front ends own the presentation. Any new sentinel is matched in both `cmd/errors.go` and `internal/tui/errors.go`.
- Integration tests are tagged `//go:build integration` and run via `make test-integration`.
- Test repos that `git init` must pass `-b main`; tmux-touching tests must isolate the socket. (Not directly needed below, but hold if you add such a test.)

---

### Task 1: herd — `ErrMainWorktree` sentinel + `Teardown` guard

Add the domain sentinel and refuse to tear down the main worktree before any session-stopping or git work happens.

**Files:**
- Modify: `internal/herd/errors.go` (sentinel var block, ~line 21)
- Modify: `internal/herd/workspace.go` (`Teardown`, insert after `cloneDir` is computed, ~line 332)
- Test: `internal/herd/teardown_main_integration_test.go` (create)

**Interfaces:**
- Produces: `herd.ErrMainWorktree` — `error` sentinel; `Teardown` returns it (wrapped via `%w`) when the ref resolves to the main worktree (i.e. `wtPath == cloneDir`).

- [ ] **Step 1: Write the failing integration test**

Create `internal/herd/teardown_main_integration_test.go`:

```go
//go:build integration

package herd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/git"
)

// Teardown must refuse the main worktree with ErrMainWorktree — the main
// worktree is the clone dir itself, and removing it makes no sense. The guard
// fires for both Force values and never reaches git.Remove, so the clone dir
// survives.
func TestTeardown_mainWorktree_refused(t *testing.T) {
	root := t.TempDir()

	// Build a real upstream repo with a main branch.
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "go.mod"), []byte("module myapp\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "init")

	projectsDir := filepath.Join(root, "projects")
	cloneDir := filepath.Join(projectsDir, "github.com", "user", "myapp")
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: projectsDir},
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git", DefaultBranch: "main"},
		},
	}
	h := New(cfg, nil, Deps{Tmux: nil, Git: localCloneGit{Runner: git.NewRealRunner(), source: remote}})

	// Clone so the main worktree resolves to the clone dir.
	if _, err := h.EnsureWorkspace(h.Ref("myapp", "main"), EnsureOpts{AutoClone: true}); err != nil {
		t.Fatalf("EnsureWorkspace(main): %v", err)
	}

	for _, force := range []bool{false, true} {
		err := h.Teardown(h.Ref("myapp", "main"), TeardownOpts{Force: force})
		if !errors.Is(err, ErrMainWorktree) {
			t.Errorf("Teardown(main, force=%v) err = %v, want ErrMainWorktree", force, err)
		}
	}

	// The clone dir must still be on disk — the guard fired before git.Remove.
	if _, err := os.Stat(filepath.Join(cloneDir, "go.mod")); err != nil {
		t.Errorf("clone dir removed by Teardown(main): %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/herd/ -run TestTeardown_mainWorktree_refused -v`
Expected: FAIL — either a compile error (`undefined: ErrMainWorktree`) or, once the sentinel exists but the guard does not, `err = <nil or worktree-removal error>, want ErrMainWorktree`.

- [ ] **Step 3: Add the sentinel**

In `internal/herd/errors.go`, add to the `var (...)` block after `ErrPathNotFound`:

```go
	ErrPathNotFound      = errors.New("worktree path not found")
	ErrMainWorktree      = errors.New("cannot delete the main worktree")
```

- [ ] **Step 4: Add the guard in `Teardown`**

In `internal/herd/workspace.go`, `Teardown`, immediately after the `cloneDir` assignment and its error check (before `if !opts.Force {`):

```go
	cloneDir, err := h.cloneDir(ref.Project)
	if err != nil {
		return err
	}

	// The main worktree is the clone dir itself; removing it makes no sense.
	// Refuse before stopping any session or touching git.
	if wtPath == cloneDir {
		return fmt.Errorf("%w: %s/%s", ErrMainWorktree, ref.Project, ref.Branch)
	}

	if !opts.Force {
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test -tags integration ./internal/herd/ -run TestTeardown_mainWorktree_refused -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/herd/errors.go internal/herd/workspace.go internal/herd/teardown_main_integration_test.go
git commit -m "feat: refuse Teardown of the main worktree with ErrMainWorktree"
```

---

### Task 2: front-end translators — map `ErrMainWorktree`

Give the new sentinel a friendly message in both the CLI and TUI translators (one error vocabulary).

**Files:**
- Modify: `cmd/errors.go` (`herdErr`, add a `case` before `default`)
- Modify: `internal/tui/errors.go` (`humanize`, add a `case` before `default`)
- Test: `cmd/errors_internal_test.go` (add a test)

**Interfaces:**
- Consumes: `herd.ErrMainWorktree` (Task 1).

- [ ] **Step 1: Write the failing test**

In `cmd/errors_internal_test.go`, add:

```go
func TestHerdErr_mainWorktree(t *testing.T) {
	err := herdErr("myapp", "main", fmt.Errorf("%w: myapp/main", herd.ErrMainWorktree))
	if err == nil {
		t.Fatal("herdErr returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "main worktree") {
		t.Errorf("message = %q, should mention the main worktree", msg)
	}
	if !strings.Contains(msg, "ch delete session") {
		t.Errorf("message = %q, should point at 'ch delete session'", msg)
	}
}
```

Ensure the test file imports `fmt`, `strings`, `testing`, and `github.com/xico42/codeherd/internal/herd`. Add whichever are missing to the existing import block.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestHerdErr_mainWorktree -v`
Expected: FAIL — `message = "cannot delete the main worktree: myapp/main"` (the raw wrapped error from the `default` branch) does not contain `ch delete session`.

- [ ] **Step 3: Add the CLI translator case**

In `cmd/errors.go`, `herdErr`, add before `default:`:

```go
	case errors.Is(err, herd.ErrMainWorktree):
		return fmt.Errorf("cannot delete the main worktree %s/%s — delete its sessions with 'ch delete session %s <branch>'", project, branch, project)
```

- [ ] **Step 4: Add the TUI translator case**

In `internal/tui/errors.go`, `humanize`, add before `default:`:

```go
	case errors.Is(err, herd.ErrMainWorktree):
		return "Cannot delete the main worktree."
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestHerdErr_mainWorktree -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/errors.go internal/tui/errors.go cmd/errors_internal_test.go
git commit -m "feat: translate ErrMainWorktree in the CLI and TUI"
```

---

### Task 3: confirm menu — `deleteAllSessions` action + main-worktree choices

Add the new action constant and make `newConfirmModel` offer only session kills for the main worktree.

**Files:**
- Modify: `internal/tui/confirm.go` (action consts ~line 13; `newConfirmModel` ~line 31)
- Test: `internal/tui/actions_test.go` (add confirm-model unit tests near the existing `TestNewConfirmModel_*`)

**Interfaces:**
- Produces: `deleteAllSessions` — new `deleteAction` const (stops every session, keeps the worktree). Consumed by Task 4's handler and switch.
- Produces: for a `target.IsMain` `Item`, `newConfirmModel` never includes a `deleteAll` choice; with both sessions it includes `deleteAllSessions`.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/actions_test.go`, add:

```go
func TestNewConfirmModel_mainBothSessions(t *testing.T) {
	target := Item{Project: "myapp", Branch: "main", Group: groupWorktree, IsMain: true, HasAgent: true, HasShell: true, AgentStatus: "running"}
	c := newConfirmModel(target)
	if len(c.choices) != 4 {
		t.Fatalf("choices = %d, want 4 (all sessions + agent + shell + cancel)", len(c.choices))
	}
	want := []deleteAction{deleteAllSessions, deleteAgent, deleteShell, deleteCancel}
	for i, w := range want {
		if c.choices[i].action != w {
			t.Errorf("choices[%d].action = %v, want %v", i, c.choices[i].action, w)
		}
	}
	// The worktree must never be offered for deletion on the main worktree.
	for _, ch := range c.choices {
		if ch.action == deleteAll {
			t.Errorf("main worktree menu offered deleteAll: %+v", c.choices)
		}
	}
}

func TestNewConfirmModel_mainAgentOnly(t *testing.T) {
	target := Item{Project: "myapp", Branch: "main", Group: groupAgent, IsMain: true, HasAgent: true, AgentStatus: "running"}
	c := newConfirmModel(target)
	want := []deleteAction{deleteAgent, deleteCancel}
	if len(c.choices) != len(want) {
		t.Fatalf("choices = %d, want %d", len(c.choices), len(want))
	}
	for i, w := range want {
		if c.choices[i].action != w {
			t.Errorf("choices[%d].action = %v, want %v", i, c.choices[i].action, w)
		}
	}
}

func TestNewConfirmModel_mainShellOnly(t *testing.T) {
	target := Item{Project: "myapp", Branch: "main", Group: groupWorktree, IsMain: true, HasShell: true}
	c := newConfirmModel(target)
	want := []deleteAction{deleteShell, deleteCancel}
	if len(c.choices) != len(want) {
		t.Fatalf("choices = %d, want %d", len(c.choices), len(want))
	}
	for i, w := range want {
		if c.choices[i].action != w {
			t.Errorf("choices[%d].action = %v, want %v", i, c.choices[i].action, w)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestNewConfirmModel_main' -v`
Expected: FAIL — compile error `undefined: deleteAllSessions` (and, once the const exists, wrong choice sets because the non-main branch still offers `deleteAll`).

- [ ] **Step 3: Add the `deleteAllSessions` const**

In `internal/tui/confirm.go`, extend the action block:

```go
const (
	deleteAll         deleteAction = iota // worktree + all sessions
	deleteAllSessions                     // all sessions, keep the worktree
	deleteAgent                           // agent session only
	deleteShell                           // shell session only
	deleteCancel                          // cancel
)
```

- [ ] **Step 4: Branch `newConfirmModel` on `IsMain`**

In `internal/tui/confirm.go`, replace the body of `newConfirmModel` from the `var choices []choice` line through the `choices = append(choices, choice{"Cancel", deleteCancel})` line with:

```go
	var choices []choice

	hasAgent := target.HasAgent
	hasShell := target.HasShell

	switch {
	case target.IsMain:
		// The main worktree is the clone dir itself — never offer to remove it.
		// Only session kills are available here; startDelete guarantees at least
		// one session is active before this menu opens.
		if hasAgent && hasShell {
			choices = append(choices, choice{"Delete all sessions", deleteAllSessions})
		}
		if hasAgent {
			choices = append(choices, choice{"Delete agent session only", deleteAgent})
		}
		if hasShell {
			choices = append(choices, choice{"Delete shell session only", deleteShell})
		}
	case hasAgent && hasShell:
		choices = append(choices, choice{"Delete everything (worktree + all sessions)", deleteAll})
		choices = append(choices, choice{"Delete agent session only", deleteAgent})
		choices = append(choices, choice{"Delete shell session only", deleteShell})
	case hasAgent:
		choices = append(choices, choice{"Delete everything (worktree + agent session)", deleteAll})
		choices = append(choices, choice{"Delete agent session only", deleteAgent})
	case hasShell:
		choices = append(choices, choice{"Delete everything (worktree + shell session)", deleteAll})
		choices = append(choices, choice{"Delete shell session only", deleteShell})
	default:
		choices = append(choices, choice{"Delete worktree", deleteAll})
	}
	choices = append(choices, choice{"Cancel", deleteCancel})
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestNewConfirmModel' -v`
Expected: PASS (both the new `_main*` tests and the pre-existing non-main ones).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/confirm.go internal/tui/actions_test.go
git commit -m "feat: main-worktree confirm menu offers session kills only"
```

---

### Task 4: TUI actions — narrow `startDelete` + `confirmDeleteAllSessions` handler

Stop hard-blocking `d` on the main worktree; open the menu when a session exists, and wire the new action to a handler that stops all sessions without tearing down the worktree.

**Files:**
- Modify: `internal/tui/actions.go` (`startDelete` ~line 175; `updateConfirmDelete` switch ~line 203; add `confirmDeleteAllSessions` near the other `confirmDelete*` funcs)
- Test: `internal/tui/actions_test.go` (update `TestStartDelete_mainWorktree`, add new tests)

**Interfaces:**
- Consumes: `deleteAllSessions` (Task 3); `herd.StopOpts{All: true}`, `herd.StopSessions` (existing domain API); `m.refreshCmd()` (existing).
- Produces: `confirmDeleteAllSessions()` — `(tea.Model, tea.Cmd)`, mirrors `confirmDeleteAgent`/`confirmDeleteShell`.

- [ ] **Step 1: Update the existing main-worktree test and add new ones**

In `internal/tui/actions_test.go`, replace `TestStartDelete_mainWorktree` with the two cases below (main + no session stays blocked with new wording; main + session opens the menu), and add the handler test:

```go
func TestStartDelete_mainWorktree_noSessions(t *testing.T) {
	items := []Item{{Project: "myapp", Branch: "main", Group: groupWorktree, IsMain: true}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	updated, _ := m.startDelete()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d (no sessions — stay on list)", um.screen, screenList)
	}
	if !strings.Contains(um.statusMsg, "no sessions") {
		t.Errorf("statusMsg = %q, should say there are no sessions to delete", um.statusMsg)
	}
}

func TestStartDelete_mainWorktree_withSession(t *testing.T) {
	items := []Item{{Project: "myapp", Branch: "main", Group: groupAgent, IsMain: true, HasAgent: true, AgentStatus: "running"}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	updated, _ := m.startDelete()
	um := updated.(Model)
	if um.screen != screenConfirmDelete {
		t.Errorf("screen = %d, want %d (session present — open confirm menu)", um.screen, screenConfirmDelete)
	}
	if um.confirm == nil {
		t.Fatal("confirm should be set for a main worktree with an active session")
	}
	for _, ch := range um.confirm.choices {
		if ch.action == deleteAll {
			t.Errorf("main worktree confirm offered deleteAll: %+v", um.confirm.choices)
		}
	}
}

func TestConfirmDeleteAllSessions_stopsBothKeepsWorktree(t *testing.T) {
	runner := &recordingRunner{
		sessions: strings.Join([]string{
			sessionRowLine("$1", "myapp-main", "myapp-main", "agent", "", "main", "myapp"),
			sessionRowLine("$2", "myapp-main~sh", "myapp-main", "shell", "", "main", "myapp"),
		}, "\n"),
	}
	hrd := teardownHerd(t, runner, nil, "main")

	m := Model{
		herd:       hrd,
		tmuxClient: tmux.NewClient(runner),
		confirm: newConfirmModel(Item{
			Ref:            herd.Ref{Project: "myapp", Branch: "main"},
			Project:        "myapp",
			Branch:         "main",
			IsMain:         true,
			AgentSessionID: "$1",
			ShellSessionID: "$2",
			HasAgent:       true,
			HasShell:       true,
		}),
	}

	updated, cmd := m.confirmDeleteAllSessions()
	if cmd == nil {
		t.Fatal("confirmDeleteAllSessions returned no command")
	}
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after action", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be cleared after the action")
	}
	cmd() // execute the stop closure

	for _, want := range []string{"$1", "$2"} {
		found := false
		for _, got := range runner.killed {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("session %s was never killed; killed=%v", want, runner.killed)
		}
	}
}
```

Note: `TestConfirmDeleteAllSessions_stopsBothKeepsWorktree` reuses `recordingRunner`, `sessionRowLine`, and `teardownHerd` from `internal/tui/delete_teardown_test.go` (same package) — do not redefine them.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStartDelete_mainWorktree|TestConfirmDeleteAllSessions' -v`
Expected: FAIL — compile error `um.confirmDeleteAllSessions undefined` and, once the handler exists, `TestStartDelete_mainWorktree_withSession` fails because `startDelete` still returns early on `IsMain`.

- [ ] **Step 3: Narrow the `startDelete` guard**

In `internal/tui/actions.go`, `startDelete`, replace:

```go
	if sel.IsMain {
		m.statusMsg = "cannot delete the main worktree"
		return m, nil
	}
```

with:

```go
	if sel.IsMain && !sel.HasAgent && !sel.HasShell {
		m.statusMsg = "main worktree has no sessions to delete"
		return m, nil
	}
```

- [ ] **Step 4: Add the `confirmDeleteAllSessions` handler**

In `internal/tui/actions.go`, add after `confirmDeleteShell`:

```go
func (m Model) confirmDeleteAllSessions() (tea.Model, tea.Cmd) {
	ref := m.confirm.target.Ref
	hrd := m.herd
	m.confirm, m.screen = nil, screenList

	return m, func() tea.Msg {
		if _, err := hrd.StopSessions(ref, herd.StopOpts{All: true}); err != nil {
			return errMsg{err: err}
		}
		return m.refreshCmd()()
	}
}
```

- [ ] **Step 5: Wire the switch in `updateConfirmDelete`**

In `internal/tui/actions.go`, `updateConfirmDelete`, add a case in the `enter` switch alongside the others:

```go
		case deleteAll:
			return m.confirmDeleteAll()
		case deleteAllSessions:
			return m.confirmDeleteAllSessions()
		case deleteAgent:
			return m.confirmDeleteAgent()
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestStartDelete|TestConfirmDelete' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/actions.go internal/tui/actions_test.go
git commit -m "feat: allow session-only delete on the main worktree in the TUI"
```

---

### Task 5: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full package tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run `make check`**

Run: `make check`
Expected: coverage ≥ 80%, integration tests pass, lint clean, build succeeds. If coverage dipped below 80%, add a focused unit test for any uncovered new branch (e.g. the TUI `humanize` case for `ErrMainWorktree`) and re-run.

- [ ] **Step 3: Manual smoke (optional, if a real project is configured)**

Run: `./ch delete worktree <project> main --force`
Expected: prints `cannot delete the main worktree <project>/main — delete its sessions with 'ch delete session <project> <branch>'` and exits non-zero; the clone dir is untouched.

- [ ] **Step 4: Commit any coverage top-ups**

```bash
git add -A
git commit -m "test: cover ErrMainWorktree translation paths"
```

(Skip if Step 2 needed no additions.)

---

## Notes for the implementer

- `ch delete session <project> main [--shell]` is intentionally left unchanged — it already stops the main worktree's sessions today because it calls `StopSessions`, never `Teardown`. No task touches it.
- `deleteAllSessions` is offered **only** on the main + agent + shell menu. Non-main worktrees keep their `Delete everything` option, which already clears sessions and removes the worktree.
- The `deleteAll` action can no longer be selected for a main worktree because Task 3 never adds it to that menu — the invariant is enforced at menu construction, so `confirmDeleteAll` needs no `IsMain` check.
