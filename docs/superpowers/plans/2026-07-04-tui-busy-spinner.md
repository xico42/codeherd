# TUI Busy Spinner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a centered animated spinner while the TUI runs slow git operations (create worktree, clone, fetch remote branches) so the dashboard never looks hung.

**Architecture:** Add a single shared `busy string` label plus a `spinner.Model` to the top-level `Model`. A non-empty `busy` makes `View()` short-circuit to a centered spinner, overriding (but never mutating) `m.screen`. The three slow dispatch sites set `busy` and batch in a spinner tick; each result message clears `busy`. A shared `enterBusy` helper carries the set-and-batch logic so every site is identical and unit-testable.

**Tech Stack:** Go, Bubble Tea v2 (`charm.land/bubbletea/v2`), Bubbles v2 spinner (`charm.land/bubbles/v2/spinner`), Lip Gloss v2 (`charm.land/lipgloss/v2`).

## Global Constraints

- Module paths use the `charm.land/...` v2 namespace (already in `go.mod`: `charm.land/bubbles/v2 v2.0.0`). Import the spinner as `"charm.land/bubbles/v2/spinner"`.
- Aggregate test coverage must stay at or above 80% (`make check`). New code needs tests.
- `Model` methods use **value receivers** and return `(tea.Model, tea.Cmd)` or `tea.Cmd`; mutating a field means assigning on the local `m` copy and returning it. Follow this — do not switch to pointer receivers.
- Do not change existing behavior on error: an `errMsg` today leaves `m.screen` unchanged and shows the error via `statusMsg`. Keep that; only add `busy = ""`.
- Run tests with the package command: `go test ./internal/tui/...`.

---

## File Structure

- `internal/tui/model.go` — add `spinner`/`busy` fields to `Model`; init spinner in `NewModel`; add `enterBusy` helper; handle `spinner.TickMsg`; swallow keys while busy; clear `busy` on result messages; render the busy screen in `View`; set `busy` in `startRemotePicker`.
- `internal/tui/actions.go` — set `busy` at the clone dispatch (via `enterBusy`) inside the `updateList` clone case. (The clone case lives in `model.go:289`; `cloneAction` itself stays unchanged.)
- `internal/tui/form.go` — set `busy` when a completed form is submitted (via `enterBusy`).
- `internal/tui/remote_picker.go` — remove the now-unused `loading` field and its `View()` branch (the shared busy screen replaces it).
- `internal/tui/model_test.go`, `internal/tui/form_test.go`, `internal/tui/remote_picker_test.go` — tests for each behavior.

---

## Task 1: Busy state scaffolding — field, init, tick loop, key swallow

Adds the `spinner`/`busy` state, the `enterBusy` helper, spinner tick handling, and the "swallow input while busy except quit" guard. No dispatch site sets `busy` yet, so behavior is unchanged until Task 4 — this task is verified purely through unit tests on the new primitives.

**Files:**
- Modify: `internal/tui/model.go` (imports; `Model` struct near line 55; `NewModel` near line 121; `Update` near line 173)
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Produces:
  - `Model.busy string` — non-empty ⇒ busy screen active.
  - `Model.spinner spinner.Model`.
  - `func (m Model) enterBusy(label string, cmd tea.Cmd) (Model, tea.Cmd)` — sets `m.busy = label` and returns `m` with `tea.Batch(cmd, m.spinner.Tick)`.

- [ ] **Step 1: Add the spinner import**

In `internal/tui/model.go`, add to the `charm.land/bubbles/v2` import group (after the `list` import on line 10):

```go
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
```

- [ ] **Step 2: Add the struct fields**

In `internal/tui/model.go`, in the `Model` struct, add after the `statusMsg` field (line 80):

```go
	// Status message for async operations.
	statusMsg string

	// busy is a non-empty label while a blocking git operation is in
	// flight. When set, View renders a centered spinner + label,
	// overriding m.screen (which is left unchanged). Cleared by the
	// operation's result message.
	busy string

	// spinner animates the busy screen. It only ticks while busy != "".
	spinner spinner.Model
```

- [ ] **Step 3: Initialize the spinner in NewModel**

In `internal/tui/model.go`, in `NewModel`, add before the `m := Model{` literal (line 121):

```go
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
```

Then add `spinner: sp,` to the `Model{...}` literal, right after `help: h,`:

```go
		list:         l,
		keys:         keys,
		help:         h,
		spinner:      sp,
		cfg:          cfg,
```

- [ ] **Step 4: Write the failing test for enterBusy and the tick loop**

In `internal/tui/model_test.go`, add:

```go
func TestEnterBusy_setsLabelAndBatchesTick(t *testing.T) {
	m := Model{}
	called := false
	cmd := func() tea.Msg { called = true; return nil }

	m2, batched := m.enterBusy("Working…", cmd)

	if m2.busy != "Working…" {
		t.Errorf("busy = %q, want %q", m2.busy, "Working…")
	}
	if batched == nil {
		t.Fatal("enterBusy returned nil cmd, want batched cmd")
	}
	_ = called // cmd is not executed here; we only assert it was batched (non-nil).
}

func TestUpdate_spinnerTickIgnoredWhenNotBusy(t *testing.T) {
	m := Model{busy: ""}

	_, cmd := m.Update(spinner.TickMsg{})

	if cmd != nil {
		t.Error("spinner.TickMsg while not busy should not re-issue a tick")
	}
}

func TestUpdate_spinnerTickAdvancesWhenBusy(t *testing.T) {
	m := Model{busy: "Working…", spinner: spinner.New()}

	_, cmd := m.Update(spinner.TickMsg{})

	if cmd == nil {
		t.Error("spinner.TickMsg while busy should re-issue a tick")
	}
}

func TestUpdate_swallowsKeysWhileBusy(t *testing.T) {
	m := Model{busy: "Working…", screen: screenList, keys: defaultKeyMap()}
	m.list = newList(nil)

	// A non-quit key is a no-op while busy.
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Text: "n"}))
	um := updated.(Model)
	if cmd != nil {
		t.Error("non-quit key while busy should return nil cmd")
	}
	if um.screen != screenList {
		t.Errorf("screen changed to %d while busy, want unchanged", um.screen)
	}

	// Quit still quits.
	_, quitCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	if quitCmd == nil {
		t.Error("quit key while busy should return a cmd")
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestEnterBusy|TestUpdate_spinnerTick|TestUpdate_swallowsKeysWhileBusy' -v`
Expected: FAIL to compile (`m.enterBusy` undefined; `spinner.TickMsg` not handled).

- [ ] **Step 6: Add the enterBusy helper**

In `internal/tui/model.go`, add after `syncProfileKeyEnabled` (after line 150):

```go
// enterBusy sets the busy label and batches the given command with a spinner
// tick, so the spinner animates while cmd runs. m.screen is left unchanged;
// View short-circuits to the busy screen while busy != "".
func (m Model) enterBusy(label string, cmd tea.Cmd) (Model, tea.Cmd) {
	m.busy = label
	return m, tea.Batch(cmd, m.spinner.Tick)
}
```

- [ ] **Step 7: Add the key-swallow guard and spinner tick handling in Update**

In `internal/tui/model.go`, at the very top of `Update` (before the `switch msg := msg.(type)` on line 174):

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While a blocking operation is in flight, swallow all input except
	// quit. The screen underneath is hidden by the busy view, and the git
	// operation cannot be cancelled mid-flight, so other keys must not
	// mutate state that the pending result message will rely on.
	if kp, ok := msg.(tea.KeyPressMsg); ok && m.busy != "" {
		if key.Matches(kp, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg := msg.(type) {
```

Then add a `spinner.TickMsg` case inside that type switch, right after the `case tickMsg:` block (after line 187):

```go
	case spinner.TickMsg:
		// Only advance (and thus re-schedule) while busy, so the tick
		// loop stops cleanly once the operation completes.
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestEnterBusy|TestUpdate_spinnerTick|TestUpdate_swallowsKeysWhileBusy' -v`
Expected: PASS (4 tests).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): add busy state, spinner tick loop, and input guard"
```

---

## Task 2: Render the centered busy screen

Makes `View()` short-circuit to a centered spinner + label when `busy != ""`.

**Files:**
- Modify: `internal/tui/model.go` (`View` near line 318)
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `Model.busy`, `Model.spinner` (Task 1).

- [ ] **Step 1: Write the failing test**

In `internal/tui/model_test.go`, add:

```go
func TestView_rendersBusyScreen(t *testing.T) {
	m := Model{busy: "Creating worktree…", spinner: spinner.New(), width: 40, height: 10}
	m.list = newList(nil)

	v := m.View()

	if !strings.Contains(v.String(), "Creating worktree…") {
		t.Errorf("busy view should contain the label; got:\n%s", v.String())
	}
}

func TestView_normalWhenNotBusy(t *testing.T) {
	m := Model{busy: "", screen: screenList, spinner: spinner.New(), width: 40, height: 10}
	m.list = newList(nil)
	m.help = help.New()
	m.keys = defaultKeyMap()

	v := m.View()

	if strings.Contains(v.String(), "Creating worktree…") {
		t.Error("non-busy view should not contain a busy label")
	}
}
```

If `strings` or `help` is not already imported in the test file, add `"strings"` and `"charm.land/bubbles/v2/help"` to its imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestView_rendersBusyScreen|TestView_normalWhenNotBusy' -v`
Expected: FAIL — `TestView_rendersBusyScreen` fails because the busy label is not rendered.

- [ ] **Step 3: Add the busy branch to View**

In `internal/tui/model.go`, at the top of `View()` (before `var content string` on line 319):

```go
func (m Model) View() tea.View {
	if m.busy != "" {
		body := lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" "+m.busy,
		)
		v := tea.NewView(body)
		v.AltScreen = true
		return v
	}

	var content string
```

`lipgloss` is already imported (`charm.land/lipgloss/v2`).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestView_rendersBusyScreen|TestView_normalWhenNotBusy' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): render centered busy spinner screen"
```

---

## Task 3: Clear busy on every result message

Clears `busy` when each slow operation completes, so the busy screen disappears and the underlying screen (list, or picker with its loaded list) renders again.

**Files:**
- Modify: `internal/tui/model.go` (`errMsg` ~line 218, `cloneDoneMsg` ~line 231, `worktreeCreatedMsg` ~line 235, `remoteBranchesMsg` ~line 244 handlers)
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `Model.busy` (Task 1).

- [ ] **Step 1: Write the failing test**

In `internal/tui/model_test.go`, add:

```go
func TestUpdate_resultMessagesClearBusy(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"errMsg", errMsg{err: errors.New("boom")}},
		{"cloneDoneMsg", cloneDoneMsg{project: "myapp"}},
		{"worktreeCreatedMsg", worktreeCreatedMsg{project: "myapp", branch: "feat"}},
		{"remoteBranchesMsg", remoteBranchesMsg{project: "myapp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModelForResultTest()
			m.busy = "Working…"

			updated, _ := m.Update(tt.msg)

			if updated.(Model).busy != "" {
				t.Errorf("%s did not clear busy", tt.name)
			}
		})
	}
}

// newModelForResultTest builds a minimal Model that can absorb result
// messages without a nil-pointer (refreshCmd/list access after clearing busy).
func newModelForResultTest() Model {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.cfg = &config.Config{}
	return m
}
```

If `errors` is not already imported in the test file, add `"errors"`.

Note on `worktreeCreatedMsg`: with `attach == false` (the default here) its handler already sets `m.screen = screenList`, `m.form = nil`, and returns `m.refreshCmd()` — `refreshCmd` reads `m.wtSvc`/`m.sesSvc`/`m.projSvc` but only inside the returned closure, which the test does not execute, so nil services are fine.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestUpdate_resultMessagesClearBusy -v`
Expected: FAIL — busy is not cleared for any of the four messages.

- [ ] **Step 3: Clear busy in each result handler**

In `internal/tui/model.go`:

In the `errMsg` case (line 218), add `m.busy = ""` at the top of the case:

```go
	case errMsg:
		m.busy = ""
		if msg.err != nil {
			m.statusMsg = msg.err.Error()
		}
		return m, nil
```

In the `cloneDoneMsg` case (line 231):

```go
	case cloneDoneMsg:
		m.busy = ""
		m.statusMsg = fmt.Sprintf("Cloned %s", msg.project)
		return m, m.refreshCmd()
```

In the `worktreeCreatedMsg` case (line 235), add `m.busy = ""` at the top:

```go
	case worktreeCreatedMsg:
		m.busy = ""
		m.statusMsg = fmt.Sprintf("Created %s/%s", msg.project, msg.branch)
		m.screen = screenList
		m.form = nil
```

In the `remoteBranchesMsg` case (line 244), add `m.busy = ""` at the top:

```go
	case remoteBranchesMsg:
		m.busy = ""
		if m.remotePicker != nil && m.remotePicker.project == msg.project {
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestUpdate_resultMessagesClearBusy -v`
Expected: PASS (4 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): clear busy state when slow operations complete"
```

---

## Task 4: Set busy at the three dispatch sites and drop the picker's loading path

Wires `enterBusy` into the create-worktree submit, clone, and remote-picker-open paths, and removes the remote picker's now-redundant `loading` field.

**Files:**
- Modify: `internal/tui/model.go` (`updateList` clone case ~line 289; `startRemotePicker` ~line 578)
- Modify: `internal/tui/form.go` (`updateForm` ~line 212)
- Modify: `internal/tui/remote_picker.go` (struct, `newRemotePicker`, `setBranches`, `View`)
- Test: `internal/tui/model_test.go`, `internal/tui/form_test.go`, `internal/tui/remote_picker_test.go`

**Interfaces:**
- Consumes: `Model.enterBusy` (Task 1), `Model.cloneAction() tea.Cmd` (unchanged), `Model.fetchRemoteBranchesCmd(project string) tea.Cmd` (unchanged), `formModel.submit() tea.Cmd` (unchanged).

- [ ] **Step 1: Write the failing tests**

In `internal/tui/model_test.go`, add:

```go
func TestUpdate_cloneSetsBusy(t *testing.T) {
	m := Model{screen: screenList, keys: defaultKeyMap(), spinner: spinner.New()}
	m.list = newList(toListItems([]Item{
		{Project: "myapp", Group: groupProject, Cloned: false},
	}))
	m.cfg = &config.Config{Projects: map[string]config.ProjectConfig{"myapp": {}}}

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Text: "c"}))
	um := updated.(Model)

	if um.busy != "Cloning myapp…" {
		t.Errorf("busy = %q, want %q", um.busy, "Cloning myapp…")
	}
	if cmd == nil {
		t.Error("clone should return a batched cmd")
	}
}

func TestStartRemotePicker_setsBusy(t *testing.T) {
	m := Model{screen: screenList, spinner: spinner.New()}
	m.list = newList(toListItems([]Item{
		{Project: "myapp", Group: groupProject, Cloned: true},
	}))
	m.cfg = &config.Config{}

	updated, cmd := m.startRemotePicker()
	um := updated.(Model)

	if um.busy != "Fetching remote branches…" {
		t.Errorf("busy = %q, want %q", um.busy, "Fetching remote branches…")
	}
	if um.screen != screenRemotePicker {
		t.Errorf("screen = %d, want screenRemotePicker", um.screen)
	}
	if cmd == nil {
		t.Error("startRemotePicker should return a batched cmd")
	}
}
```

In `internal/tui/form_test.go`, add a negative test (a still-editing form must not set busy):

```go
func TestUpdateForm_nonCompletedDoesNotSetBusy(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "develop"}
	cfg := &config.Config{
		Projects: map[string]config.ProjectConfig{"api": {}},
		Agents:   map[string]config.AgentConfig{"claude": {Cmd: "claude"}},
	}
	m := Model{screen: screenForm, spinner: spinner.New(), cfg: cfg}
	m.form = newFormModel(ctx, cfg, nil)
	m.form.Init()

	updated, _ := m.updateForm(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))

	if updated.(Model).busy != "" {
		t.Errorf("busy = %q, want empty for a non-completed form", updated.(Model).busy)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestUpdate_cloneSetsBusy|TestStartRemotePicker_setsBusy|TestUpdateForm_nonCompletedDoesNotSetBusy' -v`
Expected: FAIL — `TestUpdate_cloneSetsBusy` and `TestStartRemotePicker_setsBusy` fail because `busy` stays empty. (`TestUpdateForm_nonCompletedDoesNotSetBusy` may already pass; it guards against regressions in Step 4.)

- [ ] **Step 3: Set busy in the clone case**

In `internal/tui/model.go`, in `updateList`, replace the clone case (lines 289-290):

```go
		case key.Matches(msg, m.keys.Clone):
			return m, m.cloneAction()
```

with:

```go
		case key.Matches(msg, m.keys.Clone):
			cmd := m.cloneAction()
			if cmd == nil {
				return m, nil
			}
			// cloneAction returned non-nil ⇒ selection is a valid
			// uncloned project, so selectedItem() is non-nil.
			sel := m.selectedItem()
			m, cmd = m.enterBusy(fmt.Sprintf("Cloning %s…", sel.Project), cmd)
			return m, cmd
```

- [ ] **Step 4: Set busy in the form submit path**

In `internal/tui/form.go`, in `updateForm`, replace the completed branch (lines 212-214):

```go
	if m.form.completed() {
		return m, m.form.submit()
	}
```

with:

```go
	if m.form.completed() {
		// Reassign m (do not shadow it with :=) so the linter stays quiet.
		var submitCmd tea.Cmd
		m, submitCmd = m.enterBusy("Creating worktree…", m.form.submit())
		return m, submitCmd
	}
```

- [ ] **Step 5: Set busy in startRemotePicker**

In `internal/tui/model.go`, in `startRemotePicker`, replace the body after the guard (lines 583-585):

```go
	m.remotePicker = newRemotePicker(sel.Project, m.cfg, m.tmuxClient)
	m.screen = screenRemotePicker
	return m, m.fetchRemoteBranchesCmd(sel.Project)
```

with:

```go
	m.remotePicker = newRemotePicker(sel.Project, m.cfg, m.tmuxClient)
	m.screen = screenRemotePicker
	m, cmd := m.enterBusy("Fetching remote branches…", m.fetchRemoteBranchesCmd(sel.Project))
	return m, cmd
```

- [ ] **Step 6: Remove the picker's loading path**

In `internal/tui/remote_picker.go`:

Remove the `loading bool` field from `remotePickerModel`:

```go
type remotePickerModel struct {
	list       list.Model
	project    string
	errText    string
	cfg        *config.Config
	tmuxClient *tmux.Client
}
```

In `newRemotePicker`, drop `loading: true,` from the returned literal:

```go
	return &remotePickerModel{
		list:       l,
		project:    project,
		cfg:        cfg,
		tmuxClient: tmuxClient,
	}
```

In `setBranches`, drop the `p.loading = false` line:

```go
func (p *remotePickerModel) setBranches(branches []worktree.RemoteBranch) {
	items := make([]list.Item, len(branches))
	for i, b := range branches {
		items[i] = remoteBranchItem{rb: b}
	}
	p.list.SetItems(items)
}
```

In `View`, remove the `case p.loading:` branch:

```go
func (p *remotePickerModel) View() string {
	switch {
	case p.errText != "":
		return "Error: " + p.errText + "\n\nEsc: cancel"
	case len(p.list.Items()) == 0:
		return "No remote branches found (try again after pushing/fetching).\n\nEsc: cancel"
	default:
		return p.list.View()
	}
}
```

- [ ] **Step 7: Fix the two tests referencing `loading`**

Two tests in `internal/tui/remote_picker_test.go` assert on the removed `loading` field. Edit them:

In `TestRemotePicker_setAndSelect`, remove both `loading` assertions, keeping the branch-setting and selection checks:

```go
func TestRemotePicker_setAndSelect(t *testing.T) {
	p := newRemotePicker("myapp", &config.Config{}, nil)
	p.setBranches([]worktree.RemoteBranch{
		{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"},
		{Remote: "origin", Branch: "fix-y", Ref: "origin/fix-y"},
	})
	rb, ok := p.selected()
	if !ok || rb.Ref != "origin/feat-x" {
		t.Errorf("selected = %+v ok=%v, want origin/feat-x", rb, ok)
	}
}
```

In `TestRemoteBranchesMsg_populatesPicker`, remove the `loading` assertion (the busy-clearing behavior is covered by `TestUpdate_resultMessagesClearBusy`), keeping the item-count check:

```go
func TestRemoteBranchesMsg_populatesPicker(t *testing.T) {
	m := Model{screen: screenRemotePicker, remotePicker: newRemotePicker("myapp", &config.Config{}, nil)}
	updated, _ := m.Update(remoteBranchesMsg{
		project:  "myapp",
		branches: []worktree.RemoteBranch{{Remote: "origin", Branch: "feat-x", Ref: "origin/feat-x"}},
	})
	mm := updated.(Model)
	if got := len(mm.remotePicker.list.Items()); got != 1 {
		t.Errorf("items = %d, want 1", got)
	}
}
```

No other test references `loading`. If the build still fails, run `go test ./internal/tui/ 2>&1 | head -30` and fix the reported line.

- [ ] **Step 8: Run the full package tests to verify they pass**

Run: `go test ./internal/tui/... -v 2>&1 | tail -30`
Expected: PASS, including `TestUpdate_cloneSetsBusy`, `TestStartRemotePicker_setsBusy`, `TestUpdateForm_nonCompletedDoesNotSetBusy`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/form.go internal/tui/remote_picker.go internal/tui/model_test.go internal/tui/form_test.go internal/tui/remote_picker_test.go
git commit -m "feat(tui): show busy spinner during create, clone, and remote fetch"
```

---

## Task 5: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full check suite**

Run: `make check`
Expected: coverage ≥ 80%, integration tests, lint, and build all pass. If coverage dropped below 80%, add a focused test for any uncovered new branch (e.g. the `View` busy branch or the `enterBusy` helper) and re-run.

- [ ] **Step 2: Manual smoke (optional but recommended)**

Run: `make install` then `ch` in a project with at least one uncloned project and one clonable remote. Press `c` (clone), `n`→submit (create), and `r` (remote fetch); confirm each shows a centered animated spinner that clears on completion, and that keys do nothing (except `q`) while the spinner is up.

- [ ] **Step 3: Commit any coverage follow-up**

```bash
git add -A
git commit -m "test(tui): cover busy spinner branches"
```

(Skip if Step 1 passed with no new tests needed.)

---

## Self-Review Notes

- **Spec coverage:** Shared busy state (Task 1); centered busy screen View (Task 2); all three transitions set busy (Task 4); all four result messages clear busy (Task 3); remote picker `loading` folded into shared state (Task 4); keys swallowed except quit (Task 1); spinner tick loop stops when not busy (Task 1); table-driven tests, no integration tests (Tasks 1–4).
- **Type consistency:** `enterBusy(label string, cmd tea.Cmd) (Model, tea.Cmd)` and `busy string` / `spinner spinner.Model` are used identically across all tasks. `cloneAction() tea.Cmd` and `fetchRemoteBranchesCmd(project string) tea.Cmd` keep their existing signatures.
- **Behavior preserved on error:** the `errMsg` handler only adds `busy = ""`; it does not change `m.screen`, matching today's behavior (a known cosmetic quirk where a failed create leaves the completed form on screen is intentionally left as-is — out of scope).
