# Tmux-Native TUI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `ch` launch the TUI inside a `codeherd` tmux session by default, with context-aware attach (switch-client inside tmux, syscall.Exec outside).

**Architecture:** The root command gains a default `RunE` that detects tmux context and either creates/attaches the `codeherd` session or runs the TUI directly. Inside tmux, attach actions use `switch-client` instead of process replacement, keeping the TUI alive. A `--no-tmux` flag bypasses tmux wrapping.

**Tech Stack:** Go, Cobra, Bubble Tea v2, tmux CLI

---

### Task 1: Add SwitchClient and SelectWindow to tmux Client

**Files:**
- Modify: `internal/tmux/client.go`
- Modify: `internal/tmux/client_test.go`

**Step 1: Write failing tests for SwitchClient and SelectWindow**

Add to `internal/tmux/client_test.go`:

```go
func TestSwitchClient(t *testing.T) {
	r := &mockRunner{
		stdout: "", stderr: "", exitCode: 0,
	}
	c := NewClient(r)
	err := c.SwitchClient("my-session")
	require.NoError(t, err)
	require.Equal(t, []string{"switch-client", "-t", "my-session"}, r.lastArgs)
}

func TestSwitchClientError(t *testing.T) {
	r := &mockRunner{
		stdout: "", stderr: "no current client", exitCode: 1,
	}
	c := NewClient(r)
	err := c.SwitchClient("my-session")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no current client")
}

func TestSelectWindow(t *testing.T) {
	r := &mockRunner{
		stdout: "", stderr: "", exitCode: 0,
	}
	c := NewClient(r)
	err := c.SelectWindow("codeherd:0")
	require.NoError(t, err)
	require.Equal(t, []string{"select-window", "-t", "codeherd:0"}, r.lastArgs)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/... -run "TestSwitchClient|TestSelectWindow" -v`
Expected: FAIL — methods don't exist

**Step 3: Implement SwitchClient and SelectWindow**

Add to `internal/tmux/client.go`:

```go
// SwitchClient switches the current tmux client to the named session.
func (c *Client) SwitchClient(name string) error {
	_, stderr, code, err := c.runner.Run("switch-client", "-t", name)
	if err != nil {
		return fmt.Errorf("tmux switch-client: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux switch-client: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// SelectWindow selects a window in the current session.
func (c *Client) SelectWindow(target string) error {
	_, stderr, code, err := c.runner.Run("select-window", "-t", target)
	if err != nil {
		return fmt.Errorf("tmux select-window: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux select-window: %s", strings.TrimSpace(stderr))
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/... -run "TestSwitchClient|TestSelectWindow" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): add SwitchClient and SelectWindow methods"
```

---

### Task 2: Add NewSessionWithCmd to tmux Client

We need a method that creates a detached session with a command but no env map — used to create the `codeherd` session running `ch --no-tmux`.

**Files:**
- Modify: `internal/tmux/client.go`
- Modify: `internal/tmux/client_test.go`

**Step 1: Write failing test**

```go
func TestNewSessionWithCmd(t *testing.T) {
	r := &mockRunner{exitCode: 0}
	c := NewClient(r)
	err := c.NewSessionWithCmd("codeherd", "/tmp", "ch --no-tmux")
	require.NoError(t, err)
	require.Equal(t, []string{"new-session", "-d", "-s", "codeherd", "-c", "/tmp", "ch --no-tmux"}, r.lastArgs)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/... -run TestNewSessionWithCmd -v`
Expected: FAIL

**Step 3: Implement**

Add to `internal/tmux/client.go`:

```go
// NewSessionWithCmd creates a detached tmux session with an initial command.
func (c *Client) NewSessionWithCmd(name, dir, cmd string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	if cmd != "" {
		args = append(args, cmd)
	}
	_, stderr, code, err := c.runner.Run(args...)
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(stderr))
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tmux/... -run TestNewSessionWithCmd -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): add NewSessionWithCmd method"
```

---

### Task 3: Add CurrentSession helper to tmux Client

We need a way to detect whether we're inside tmux and which session we're in. This checks `$TMUX` env var and runs `tmux display-message -p '#{session_name}'`.

**Files:**
- Modify: `internal/tmux/client.go`
- Modify: `internal/tmux/client_test.go`

**Step 1: Write failing tests**

```go
func TestCurrentSession(t *testing.T) {
	r := &mockRunner{
		stdout: "my-session\n", exitCode: 0,
	}
	c := NewClient(r)
	name, err := c.CurrentSession()
	require.NoError(t, err)
	require.Equal(t, "my-session", name)
	require.Equal(t, []string{"display-message", "-p", "#{session_name}"}, r.lastArgs)
}

func TestCurrentSessionNotInTmux(t *testing.T) {
	r := &mockRunner{
		stderr: "no current client", exitCode: 1,
	}
	c := NewClient(r)
	name, err := c.CurrentSession()
	require.NoError(t, err)
	require.Equal(t, "", name)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/... -run TestCurrentSession -v`
Expected: FAIL

**Step 3: Implement**

Add to `internal/tmux/client.go`:

```go
// CurrentSession returns the name of the tmux session the current client is
// attached to. Returns empty string (no error) if not inside tmux.
func (c *Client) CurrentSession() (string, error) {
	stdout, _, code, err := c.runner.Run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	if code != 0 {
		return "", nil // not in tmux
	}
	return strings.TrimSpace(stdout), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/... -run TestCurrentSession -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): add CurrentSession method"
```

---

### Task 4: Make TUI attach context-aware (switch-client vs tea.Quit)

The TUI needs to know whether it's running inside tmux. If so, `attachMsg` should trigger `switch-client` instead of `tea.Quit` + `PendingAttach`.

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/actions.go`

**Step 1: Add InsideTmux field to Model**

In `internal/tui/model.go`, add `InsideTmux bool` to the `Model` struct:

```go
type Model struct {
	// ... existing fields ...

	// Set before quitting to trigger tmux attach.
	PendingAttach string

	// When true, attach uses switch-client instead of quitting.
	InsideTmux bool

	// ... rest of fields ...
}
```

**Step 2: Update NewModel to accept InsideTmux parameter**

Change the `NewModel` signature:

```go
func NewModel(
	cfg *config.Config,
	wtSvc *worktree.Service,
	sesSvc *session.Service,
	projSvc *project.Service,
	tmuxClient *tmux.Client,
	insideTmux bool,
) Model {
```

Set `InsideTmux: insideTmux` in the returned struct.

**Step 3: Update attachMsg handler in Update()**

In `internal/tui/model.go`, change the `attachMsg` case in `Update()`:

```go
	case attachMsg:
		if m.InsideTmux {
			return m, m.switchClientCmd(msg.session)
		}
		m.PendingAttach = msg.session
		return m, tea.Quit
```

**Step 4: Add switchClientCmd method**

In `internal/tui/model.go`:

```go
// switchClientCmd switches the tmux client to the given session.
func (m Model) switchClientCmd(session string) tea.Cmd {
	tmuxClient := m.tmuxClient
	return func() tea.Msg {
		if err := tmuxClient.SwitchClient(session); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}
```

**Step 5: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: Compilation errors — callers of `NewModel` need the new parameter. Fix any test callers by adding `false` as the last argument.

**Step 6: Fix test callers of NewModel**

Search for `NewModel(` in test files and add `false` as the last argument to each call.

**Step 7: Run tests again**

Run: `go test ./internal/tui/... -v`
Expected: PASS

**Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/actions.go internal/tui/*_test.go
git commit -m "feat(tui): context-aware attach via switch-client inside tmux"
```

---

### Task 5: Add codeherd session constant to semconv

**Files:**
- Modify: `internal/semconv/semconv.go`

**Step 1: Add constant**

Add to `internal/semconv/semconv.go`:

```go
const CodeherdSessionName = "codeherd"
```

**Step 2: Commit**

```bash
git add internal/semconv/semconv.go
git commit -m "feat(semconv): add CodeherdSessionName constant"
```

---

### Task 6: Wire root command as TUI entrypoint

This is the main behavior change: `ch` (no args) launches the TUI inside a `codeherd` tmux session.

**Files:**
- Modify: `cmd/root.go`
- Delete: `cmd/tui.go`

**Step 1: Add --no-tmux flag to root command**

In `cmd/root.go`, add to the `var` block:

```go
var noTmux bool
```

In `init()`, add:

```go
rootCmd.Flags().BoolVar(&noTmux, "no-tmux", false, "run TUI directly without tmux wrapping")
```

**Step 2: Move TUI logic into root command's RunE**

Set `rootCmd.RunE` in the `var rootCmd` declaration. The logic:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	return runTUI(cmd)
},
```

Then implement `runTUI` as a function in `cmd/root.go`:

```go
func runTUI(cmd *cobra.Command) error {
	tmuxRunner := tmux.NewRealRunner()
	tmuxClient := tmux.NewClient(tmuxRunner)

	if noTmux {
		return runTUIDirect(tmuxClient)
	}
	return runTUIInTmux(tmuxClient)
}
```

**Step 3: Implement runTUIInTmux — the tmux session lifecycle**

```go
func runTUIInTmux(tmuxClient *tmux.Client) error {
	currentSession, err := tmuxClient.CurrentSession()
	if err != nil {
		return fmt.Errorf("detecting tmux session: %w", err)
	}

	sessionName := semconv.CodeherdSessionName

	// Case 2: Already in the codeherd session — select TUI window.
	if currentSession == sessionName {
		return tmuxClient.SelectWindow(sessionName + ":0")
	}

	// Ensure codeherd session exists.
	exists, err := tmuxClient.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		chBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding ch binary: %w", err)
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "/"
		}
		if err := tmuxClient.NewSessionWithCmd(sessionName, homeDir, chBin+" --no-tmux"); err != nil {
			return fmt.Errorf("creating codeherd session: %w", err)
		}
	}

	// Case 3: Inside tmux, different session — switch client.
	if currentSession != "" {
		return tmuxClient.SwitchClient(sessionName)
	}

	// Case 4: Not inside tmux — exec into tmux attach.
	return execTmuxAttach(sessionName)
}
```

**Step 4: Implement runTUIDirect — the --no-tmux path**

This is the current `tuiCmd.RunE` logic, minus the process-replacing attach:

```go
func runTUIDirect(tmuxClient *tmux.Client) error {
	wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient)
	sesSvc := newSessionService()
	projSvc := project.NewService(cfg, project.NewRealGitRunner())

	insideTmux := os.Getenv("TMUX") != ""
	m := tui.NewModel(cfg, wtSvc, sesSvc, projSvc, tmuxClient, insideTmux)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	// Only used when --no-tmux and not inside tmux.
	if fm, ok := finalModel.(tui.Model); ok && fm.PendingAttach != "" {
		return execTmuxAttach(fm.PendingAttach)
	}

	return nil
}
```

**Step 5: Delete `cmd/tui.go`**

Remove the file entirely.

**Step 6: Update imports in cmd/root.go**

Add to imports:

```go
"os"

tea "charm.land/bubbletea/v2"

"github.com/xico42/codeherd/internal/project"
"github.com/xico42/codeherd/internal/semconv"
"github.com/xico42/codeherd/internal/tmux"
"github.com/xico42/codeherd/internal/tui"
"github.com/xico42/codeherd/internal/worktree"
```

**Step 7: Build and verify**

Run: `go build -o ch .`
Expected: Compiles successfully

**Step 8: Commit**

```bash
git rm cmd/tui.go
git add cmd/root.go
git commit -m "feat: make root command launch TUI in codeherd tmux session"
```

---

### Task 7: Update CLI callers of execTmuxAttach for tmux awareness

`execTmuxAttach` is called from `cmd/session.go` and `cmd/worktree.go` for `--attach` flags. These should also use `switch-client` when inside tmux.

**Files:**
- Modify: `cmd/session.go`

**Step 1: Make execTmuxAttach context-aware**

Replace the existing `execTmuxAttach` in `cmd/session.go`:

```go
// execTmuxAttach attaches to a tmux session. If already inside tmux, uses
// switch-client. Otherwise, replaces the process with tmux attach-session.
func execTmuxAttach(name string) error {
	if os.Getenv("TMUX") != "" {
		tc := tmux.NewClient(tmux.NewRealRunner())
		return tc.SwitchClient(name)
	}
	tmuxBin, err := lookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}
	return syscall.Exec(tmuxBin, []string{"tmux", "attach-session", "-t", name}, os.Environ())
}
```

**Step 2: Add tmux import if not present**

Ensure `"github.com/xico42/codeherd/internal/tmux"` is imported in `cmd/session.go`.

**Step 3: Run session tests**

Run: `go test ./cmd/... -run TestSession -v`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/session.go
git commit -m "feat: make execTmuxAttach use switch-client inside tmux"
```

---

### Task 8: Update tests for root command changes

**Files:**
- Modify: `cmd/root_test.go`

**Step 1: Read existing root tests**

Read `cmd/root_test.go` to understand current test patterns.

**Step 2: Update or add tests**

- Verify `ch help` still shows help output
- Verify `--no-tmux` flag is recognized
- Any test that relied on `ch tui` subcommand needs to be removed or updated

**Step 3: Run full test suite**

Run: `go test ./... -v`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/root_test.go
git commit -m "test: update root command tests for TUI default behavior"
```

---

### Task 9: Run make check

**Step 1: Run full verification**

Run: `make check`
Expected: Coverage >= 80%, integration tests pass, lint passes, build succeeds.

**Step 2: Fix any issues found**

If coverage drops below 80%, add tests for the new code paths in `cmd/root.go`.

**Step 3: Final commit if needed**

```bash
git add -A
git commit -m "test: ensure coverage meets 80% threshold"
```
