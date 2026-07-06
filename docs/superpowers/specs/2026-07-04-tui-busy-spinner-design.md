# TUI busy spinner for slow transitions

## Problem

Creating a worktree from the TUI can take several seconds. Since commit
`37f0884`, every creation path freshens its source with a network `fetch`
before branching, and the tracking path fetches a remote branch outright.
Per-project hooks run after creation and can be slow too. During this whole
window the dashboard shows the frozen, completed form with no feedback, so the
UI looks hung.

The same is true of two other slow transitions:

- The remote-branch picker (`r`) fetches all remotes. It shows a static
  `"Fetching remote branches…"` string that never animates, so it also reads as
  stuck.
- The clone action (`c`) does network work and only sets a `statusMsg`.

## Goal

Give a clear in-progress indication during these slow transitions so the UI
never looks hung. A single animated spinner, shown on a dedicated centered busy
screen, covering all three transitions with one shared mechanism.

Non-goals: phased/step-by-step progress (fetch vs. create vs. hooks),
cancellation of in-flight git operations, and progress percentages. A generic
animated spinner is enough to solve the "looks hung" problem; staged progress
can be a later increment if ever justified.

## Architecture: one shared busy state

The slow work already runs off the UI goroutine — `form.go:submit()`,
`cloneAction`, and the remote-fetch command each return a `tea.Cmd` that runs to
completion and emits a result message. The gap is purely presentational: nothing
is shown while the command is in flight.

Add a single reusable busy indicator to the top-level `Model` rather than three
ad-hoc ones:

```go
// in Model
spinner spinner.Model // charm.land/bubbles/v2/spinner
busy    string        // non-empty label ⇒ busy screen is active
```

- `busy == ""` → normal rendering (list / form / pickers), unchanged.
- `busy != ""` → `View()` short-circuits to a centered `"<spinner> <busy>"`,
  overriding whatever `m.screen` is.

The underlying `m.screen` is **never changed** by the busy state. When the result
message clears `busy`, the correct screen renders again automatically — the list
after a create/clone, or the remote picker showing its now-loaded list. This
keeps the "which screen do I return to?" logic trivial: there is nothing to
remember.

`busy` is a plain label string rather than a bool so the same screen can say
`"Creating worktree…"`, `"Cloning myapp…"`, or `"Fetching remote branches…"`.

## Data flow: set on dispatch, clear on result

| Trigger | Sets `busy` to | Cleared by |
|---|---|---|
| Form submit (`submit()`) | `"Creating worktree…"` | `worktreeCreatedMsg` / `errMsg` |
| Clone action (`c`) | `"Cloning <project>…"` | `cloneDoneMsg` / `errMsg` |
| Remote picker open (`r`) | `"Fetching remote branches…"` | `remoteBranchesMsg` |

Setting `busy` and dispatching the slow command happen together in the same
`Update` branch. The command is unchanged; only the surrounding state is set.

### Spinner animation

The spinner animates on its own `spinner.TickMsg` loop, independent of the
existing 3-second `tickMsg` refresh loop (which keeps running harmlessly behind
the busy screen — a background `itemsMsg` refresh just updates the hidden list).

- Whenever `busy` is set, `tea.Batch` in `m.spinner.Tick` alongside the slow
  command.
- The `spinner.TickMsg` handler advances the frame and re-issues the spinner
  tick **only while `busy != ""`**, so the animation loop stops cleanly once the
  result arrives and `busy` is cleared. This avoids a runaway tick loop after the
  operation completes.

### Removing the duplicate loading path

The remote picker's own `loading bool` and its static
`"Fetching remote branches…"` `View()` branch are **removed** and folded into the
shared busy state — one loading mechanism, not two. The picker keeps its
`errText` / empty-list / list branches. On `remoteBranchesMsg`:

- success → `busy` clears, `screenRemotePicker` renders the list (or the "no
  remote branches found" empty state);
- error → `busy` clears and the picker's `errText` shows the error, exactly as
  today.

On any `errMsg` for create/clone, `busy` clears and the existing `statusMsg`
renders the error in the status line, as it does today. On success, the existing
`statusMsg` (`"Created myapp/feat"`, `"Cloned myapp"`) shows once the busy screen
is gone.

### Keys while busy

While `busy != ""`, all input is swallowed except the global quit binding
(`q` / `Ctrl-C`), which quits the app. The git operation itself is not
cancellable mid-flight, so there is no "cancel" affordance — `Esc` does nothing
until the result message arrives. This is enforced by an early check at the top
of `Update`'s key handling: if `busy != ""`, only quit is honored.

## View

When `busy != ""`, `View()` returns a centered single line:

```


        ◐ Creating worktree…


```

Centering reuses the model's known `width`/`height`. The exact vertical/
horizontal centering follows whatever the existing views already do for layout;
if there is no shared centering helper, a small local one is fine (it is only
used here).

## Testing

Following the repo's table-driven `internal/tui/model_test.go` style. No new
integration tests — this is pure TUI state, and `spinner.Model` is deterministic
under an injected `TickMsg`.

- **Transitions set busy**: submitting the form, clone, and `r` each set `busy`
  to the expected label and batch a spinner tick.
- **Results clear busy**: `worktreeCreatedMsg`, `cloneDoneMsg`,
  `remoteBranchesMsg` (success and error), and `errMsg` each clear `busy`.
- **View**: `View()` renders the centered label when `busy != ""` and the normal
  screen when `busy == ""`.
- **Keys while busy**: a non-quit key press while `busy != ""` is a no-op; the
  quit binding still quits.
- **Tick loop stops**: a `spinner.TickMsg` while `busy == ""` does not re-issue a
  tick.

## Files touched

- `internal/tui/model.go` — `spinner`/`busy` fields; `Update` branches to set on
  dispatch and clear on result; `View` short-circuit; key-swallow while busy;
  spinner tick handling.
- `internal/tui/actions.go` — set `busy` in `cloneAction` dispatch; set `busy`
  when opening the remote picker.
- `internal/tui/form.go` — set `busy` when `submit()` is dispatched.
- `internal/tui/remote_picker.go` — remove `loading` and its `View()` branch.
- `internal/tui/model_test.go` (and picker/actions tests as needed) — cover the
  cases above.
