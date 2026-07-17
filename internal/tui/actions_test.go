package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/tmux"
)

// tuiHerd builds a Herd for the action tests, wired to runner, with one
// configured project "myapp" whose worktree for branch exists on disk (Launch
// stats the path it derives from the Ref).
func tuiHerd(t *testing.T, runner tmux.Runner, branch string) (*herd.Herd, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{ProjectsDir: dir},
		Projects: map[string]config.ProjectConfig{"myapp": {Repo: "git@github.com:user/myapp.git"}},
	}
	wt := filepath.Join(dir, "github.com", "user", "myapp__worktrees", semconv.FlattenBranch(branch))
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	return herd.New(cfg, nil, herd.Deps{Tmux: runner}), cfg
}

func TestStartDelete_noSelection(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, _ := m.startDelete()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d (should stay on list)", um.screen, screenList)
	}
}

func TestStartDelete_projectItem(t *testing.T) {
	items := []Item{{Project: "infra", Group: groupProject}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	updated, _ := m.startDelete()
	um := updated.(Model)
	// Can't delete a project-only item — stays on list with a status message.
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d (should stay on list for project items)", um.screen, screenList)
	}
	if um.statusMsg == "" {
		t.Error("statusMsg should be set when trying to delete a project item")
	}
}

func TestStartDelete_mainWorktree(t *testing.T) {
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
		t.Errorf("screen = %d, want %d (should stay on list for main worktree)", um.screen, screenList)
	}
	if um.statusMsg == "" {
		t.Error("statusMsg should be set when trying to delete main worktree")
	}
	if !strings.Contains(um.statusMsg, "main worktree") {
		t.Errorf("statusMsg = %q, should mention main worktree", um.statusMsg)
	}
}

func TestStartDelete_worktreeItem(t *testing.T) {
	items := []Item{{Project: "myapp", Branch: "feat", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	updated, _ := m.startDelete()
	um := updated.(Model)
	if um.screen != screenConfirmDelete {
		t.Errorf("screen = %d, want %d (should go to confirm)", um.screen, screenConfirmDelete)
	}
	if um.confirm == nil {
		t.Fatal("confirm should be set after startDelete on worktree item")
	}
	if um.confirm.target.Project != "myapp" || um.confirm.target.Branch != "feat" {
		t.Errorf("confirm.target = %v/%v, want myapp/feat", um.confirm.target.Project, um.confirm.target.Branch)
	}
}

// ── confirmModel unit tests ──────────────────────────────────────────────────

func TestNewConfirmModel_noSessions(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	c := newConfirmModel(target)
	if len(c.choices) != 2 {
		t.Errorf("choices = %d, want 2 (delete worktree + cancel)", len(c.choices))
	}
	if c.choices[0].action != deleteAll {
		t.Errorf("first choice = %v, want deleteAll", c.choices[0].action)
	}
	if c.choices[1].action != deleteCancel {
		t.Errorf("last choice = %v, want deleteCancel", c.choices[1].action)
	}
}

func TestNewConfirmModel_agentOnly(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, AgentStatus: "running"}
	c := newConfirmModel(target)
	if len(c.choices) != 3 {
		t.Errorf("choices = %d, want 3", len(c.choices))
	}
	if c.choices[0].action != deleteAll {
		t.Errorf("first = %v, want deleteAll", c.choices[0].action)
	}
	if c.choices[1].action != deleteAgent {
		t.Errorf("second = %v, want deleteAgent", c.choices[1].action)
	}
}

func TestNewConfirmModel_bothSessions(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, HasShell: true, AgentStatus: "running"}
	c := newConfirmModel(target)
	if len(c.choices) != 4 {
		t.Errorf("choices = %d, want 4", len(c.choices))
	}
	if c.choices[0].action != deleteAll {
		t.Errorf("choices[0].action = %v, want deleteAll", c.choices[0].action)
	}
	if c.choices[1].action != deleteAgent {
		t.Errorf("choices[1].action = %v, want deleteAgent", c.choices[1].action)
	}
	if c.choices[2].action != deleteShell {
		t.Errorf("choices[2].action = %v, want deleteShell", c.choices[2].action)
	}
	if c.choices[3].action != deleteCancel {
		t.Errorf("choices[3].action = %v, want deleteCancel", c.choices[3].action)
	}
}

func TestConfirmModel_navigation(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	c := newConfirmModel(target)
	if c.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", c.cursor)
	}

	// j moves down
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if c.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", c.cursor)
	}

	// k moves back up
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Code: 'k', Text: "k"}))
	if c.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", c.cursor)
	}

	// k at top stays at 0
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Code: 'k', Text: "k"}))
	if c.cursor != 0 {
		t.Errorf("cursor after k at top = %d, want 0", c.cursor)
	}

	// j at bottom stays at last choice index
	c2 := newConfirmModel(target)
	lastIdx := len(c2.choices) - 1
	for i := 0; i < lastIdx+2; i++ {
		c2, _ = c2.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	}
	if c2.cursor != lastIdx {
		t.Errorf("cursor after j past bottom = %d, want %d", c2.cursor, lastIdx)
	}
}

func TestConfirmModel_selection(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	c := newConfirmModel(target)
	if c.selected() != deleteAll {
		t.Errorf("selected() = %v, want deleteAll", c.selected())
	}

	// Move to cancel
	c, _ = c.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if c.selected() != deleteCancel {
		t.Errorf("selected() = %v, want deleteCancel", c.selected())
	}
}

func TestConfirmModel_View(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, AgentStatus: "running"}
	c := newConfirmModel(target)
	out := stripANSI(c.View())
	if !strings.Contains(out, "myapp") {
		t.Errorf("View() should contain project name: %q", out)
	}
	if !strings.Contains(out, "Agent session") {
		t.Errorf("View() should mention active agent: %q", out)
	}
}

// ── updateConfirmDelete integration tests ────────────────────────────────────

func TestUpdateConfirmDelete_esc(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	updated, _ := m.updateConfirmDelete(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after esc", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after esc")
	}
}

func TestUpdateConfirmDelete_enterDeleteAll(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	// cursor starts at 0 (deleteAll) — press enter immediately
	updated, cmd := m.updateConfirmDelete(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after enter with deleteAll", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after successful delete")
	}
	_ = cmd // delete cmd — not called (services are nil)
}

func TestUpdateConfirmDelete_enterCancel(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	// Move to cancel choice (last item).
	m.confirm, _ = m.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))

	updated, _ := m.updateConfirmDelete(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	um := updated.(Model)
	// Cancel selection should go back to list.
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after cancel selection", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after cancel")
	}
}

func TestUpdateConfirmDelete_nonKeyMsg(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	updated, cmd := m.updateConfirmDelete(cloneDoneMsg{project: "x"})
	um := updated.(Model)
	if um.screen != screenConfirmDelete {
		t.Errorf("screen = %d, want %d for non-key msg", um.screen, screenConfirmDelete)
	}
	_ = cmd
}

func TestConfirmDeleteNo(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	updated, _ := m.confirmDeleteNo()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after cancel", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after cancel")
	}
}

// ── Clone / Attach / Shell action tests ─────────────────────────────────────

func TestCloneAction_nilSelection(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	cmd := m.cloneAction()
	if cmd != nil {
		t.Error("cloneAction() with no selection should return nil")
	}
}

func TestCloneAction_nonProjectItem(t *testing.T) {
	items := []Item{{Project: "myapp", Branch: "feat", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	cmd := m.cloneAction()
	if cmd != nil {
		t.Error("cloneAction() on non-project item should return nil")
	}
}

func TestCloneAction_alreadyCloned(t *testing.T) {
	items := []Item{{Project: "myapp", Group: groupProject, Cloned: true}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	cmd := m.cloneAction()
	if cmd != nil {
		t.Error("cloneAction() on already-cloned item should return nil")
	}
}

func TestAttachAction_nilSelection(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, cmd := m.attachAction()
	um := updated.(Model)
	_ = um
	if cmd != nil {
		t.Error("attachAction() with no selection should return nil cmd")
	}
}

func TestAttachAction_agentGroup(t *testing.T) {
	items := []Item{{Project: "myapp", Branch: "feat", Group: groupAgent, AgentSessionID: "$5"}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList}
	m.list = newList(listItems)

	_, cmd := m.attachAction()
	if cmd == nil {
		t.Fatal("attachAction() on agent item should return non-nil cmd")
	}
	msg := cmd()
	am, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want attachMsg", msg)
	}
	if am.session != "$5" {
		t.Errorf("session = %q, want %q", am.session, "$5")
	}
}

func TestShellAction_nilSelection(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	cmd := m.shellAction()
	if cmd != nil {
		t.Error("shellAction() with no selection should return nil")
	}
}

func TestAttachAction_worktreeGroup_noAgents_returnsError(t *testing.T) {
	cfg := &config.Config{} // no agents defined
	items := []Item{{Project: "myapp", Branch: "feat", Path: "/some/path", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.statusMsg == "" {
		t.Error("attachAction with no agents should set statusMsg")
	}
}

func TestAttachAction_worktreeGroup_singleAgent_skipsPickerAndReturnCmd(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "feat", Path: "/some/path", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	updated, cmd := m.attachAction()
	um := updated.(Model)
	// Single agent — should skip picker and go straight to starting.
	if um.screen == screenAgentPicker {
		t.Error("single agent should skip picker screen")
	}
	if cmd == nil {
		t.Fatal("attachAction with single agent should return non-nil cmd")
	}
}

func TestAttachAction_worktreeGroup_multipleAgents_showsPicker(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "feat", Path: "/some/path", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.screen != screenAgentPicker {
		t.Errorf("screen = %d, want %d (screenAgentPicker)", um.screen, screenAgentPicker)
	}
	if um.agentPicker == nil {
		t.Fatal("agentPicker should be set")
	}
}

func TestAttachAction_worktreeGroup_multipleAgents_defaultPreselected(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{Agent: "aider"},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "feat", Path: "/some/path", Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.agentPicker == nil {
		t.Fatal("agentPicker should be set")
	}
	if um.agentPicker.selected() != "aider" {
		t.Errorf("selected = %q, want aider (default)", um.agentPicker.selected())
	}
}

func TestAttachAction_projectGroup_noAgents_returnsError(t *testing.T) {
	cfg := &config.Config{} // no agents
	items := []Item{{Project: "myapp", Branch: "", Group: groupProject}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.statusMsg == "" {
		t.Error("attachAction on project with no agents should set statusMsg")
	}
}

func TestAttachAction_projectGroup_singleAgent_skipsPickerAndReturnsCmd(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "", Group: groupProject}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg, herd: herd.New(cfg, nil, herd.Deps{})}
	m.list = newList(listItems)

	updated, cmd := m.attachAction()
	um := updated.(Model)
	if um.screen == screenAgentPicker {
		t.Error("single agent on project should skip picker screen")
	}
	if cmd == nil {
		t.Fatal("single agent on project should return non-nil cmd")
	}
}

func TestAttachAction_projectGroup_multipleAgents_showsPicker(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "", Group: groupProject}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg, herd: herd.New(cfg, nil, herd.Deps{})}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.screen != screenAgentPicker {
		t.Errorf("multiple agents on project: screen = %d, want screenAgentPicker", um.screen)
	}
	if um.agentPicker == nil {
		t.Fatal("agentPicker should be set")
	}
}

func TestAttachAction_projectGroup_defaultBranch(t *testing.T) {
	cfg := &config.Config{
		Projects: map[string]config.ProjectConfig{
			"myapp": {DefaultBranch: "develop"},
		},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}
	items := []Item{{Project: "myapp", Branch: "", Group: groupProject}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg, herd: herd.New(cfg, nil, herd.Deps{})}
	m.list = newList(listItems)

	updated, _ := m.attachAction()
	um := updated.(Model)
	if um.agentPicker == nil {
		t.Fatal("agentPicker should be set")
	}
	if um.agentPicker.pending.ref.Branch != "develop" {
		t.Errorf("picker.pending.ref.Branch = %q, want develop", um.agentPicker.pending.ref.Branch)
	}
}

// ── confirmDeleteAgent / confirmDeleteShell ──────────────────────────────────

func TestConfirmDeleteAgent_returnsCmd(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)
	// tmuxClient nil — confirmDeleteAgent builds a cmd; calling it would panic on HasSession.
	// We just verify the model state transitions correctly.
	updated, cmd := m.confirmDeleteAgent()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after confirmDeleteAgent", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after confirmDeleteAgent")
	}
	if cmd == nil {
		t.Error("confirmDeleteAgent should return a non-nil cmd")
	}
}

func TestConfirmDeleteShell_returnsCmd(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree, HasShell: true}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)
	// tmuxClient nil — confirmDeleteShell builds a cmd; calling it would panic on HasSession.
	updated, cmd := m.confirmDeleteShell()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after confirmDeleteShell", um.screen, screenList)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after confirmDeleteShell")
	}
	if cmd == nil {
		t.Error("confirmDeleteShell should return a non-nil cmd")
	}
}

func TestUpdateConfirmDelete_enterDeleteAgent(t *testing.T) {
	// Build a target with an agent session so deleteAgent appears as a choice.
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	// choices[0] = deleteAll, choices[1] = deleteAgent — move down once.
	m.confirm, _ = m.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if m.confirm.selected() != deleteAgent {
		t.Fatalf("precondition: selected = %v, want deleteAgent", m.confirm.selected())
	}

	updated, cmd := m.updateConfirmDelete(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want screenList after deleteAgent", um.screen)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after deleteAgent")
	}
	if cmd == nil {
		t.Error("deleteAgent should return non-nil cmd")
	}
}

func TestUpdateConfirmDelete_enterDeleteShell(t *testing.T) {
	// Build a target with both sessions so deleteShell appears as a choice.
	target := Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, HasShell: true}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	// choices[0]=deleteAll, [1]=deleteAgent, [2]=deleteShell — move down twice.
	m.confirm, _ = m.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	m.confirm, _ = m.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if m.confirm.selected() != deleteShell {
		t.Fatalf("precondition: selected = %v, want deleteShell", m.confirm.selected())
	}

	updated, cmd := m.updateConfirmDelete(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want screenList after deleteShell", um.screen)
	}
	if um.confirm != nil {
		t.Error("confirm should be nil after deleteShell")
	}
	if cmd == nil {
		t.Error("deleteShell should return non-nil cmd")
	}
}

func TestConfirmView_shellOnly(t *testing.T) {
	// Cover the hasShell-only branch in newConfirmModel View.
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree, HasShell: true}
	c := newConfirmModel(target)
	if len(c.choices) != 3 {
		t.Errorf("choices = %d, want 3 (deleteAll + deleteShell + cancel)", len(c.choices))
	}
	if c.choices[0].action != deleteAll {
		t.Errorf("choices[0] = %v, want deleteAll", c.choices[0].action)
	}
	if c.choices[1].action != deleteShell {
		t.Errorf("choices[1] = %v, want deleteShell", c.choices[1].action)
	}
	out := stripANSI(c.View())
	if !strings.Contains(out, "Shell session") {
		t.Errorf("View() should mention Shell session: %q", out)
	}
}

// ── agentPickerModel tests ────────────────────────────────────────────────────

func TestAgentPicker_selected_empty(t *testing.T) {
	cfg := &config.Config{}
	p := newAgentPicker(cfg, "", &agentPickerPending{})
	if p.selected() != "" {
		t.Errorf("selected() with no agents = %q, want empty", p.selected())
	}
}

func TestAgentPicker_selected_default(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"aider":  {Cmd: "aider"},
			"claude": {Cmd: "claude"},
		},
	}
	p := newAgentPicker(cfg, "aider", &agentPickerPending{})
	if p.selected() != "aider" {
		t.Errorf("selected() = %q, want aider", p.selected())
	}
}

func TestAgentPicker_Update_navigate(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"aider":  {Cmd: "aider"},
			"claude": {Cmd: "claude"},
		},
	}
	p := newAgentPicker(cfg, "aider", &agentPickerPending{})
	if p.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", p.cursor)
	}
	// Move down — cursor should go from 0 to 1.
	p.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if p.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", p.cursor)
	}
	// Move up — cursor should return to 0.
	p.Update(tea.KeyPressMsg(tea.Key{Code: 'k', Text: "k"}))
	if p.cursor != 0 {
		t.Errorf("after k: cursor = %d, want 0", p.cursor)
	}
}

func TestAgentPicker_Update_nonKey(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	p := newAgentPicker(cfg, "claude", &agentPickerPending{})
	p2, cmd := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Error("non-key message should return nil cmd")
	}
	if p2.cursor != p.cursor {
		t.Error("non-key message should not change cursor")
	}
}

func TestAgentPicker_submit_agentNotFound(t *testing.T) {
	cfg := &config.Config{}
	p := newAgentPicker(cfg, "", &agentPickerPending{})
	cmd := p.submit()
	if cmd == nil {
		t.Fatal("submit with no agents should return error cmd")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("submit() returned %T, want errMsg", msg)
	}
}

func TestAgentPicker_View(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}
	p := newAgentPicker(cfg, "claude", &agentPickerPending{})
	view := p.View()
	if !strings.Contains(view, "Select Agent") {
		t.Errorf("View() should contain 'Select Agent': %q", view)
	}
	if !strings.Contains(view, "claude") {
		t.Errorf("View() should list claude agent: %q", view)
	}
	if !strings.Contains(view, "aider") {
		t.Errorf("View() should list aider agent: %q", view)
	}
}

// ── updateAgentPicker tests ───────────────────────────────────────────────────

func TestUpdateAgentPicker_esc(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	m := Model{screen: screenAgentPicker, cfg: cfg}
	m.list = newList(nil)
	m.agentPicker = newAgentPicker(cfg, "claude", &agentPickerPending{})

	updated, cmd := m.updateAgentPicker(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("after esc, screen = %d, want %d (screenList)", um.screen, screenList)
	}
	if um.agentPicker != nil {
		t.Error("agentPicker should be nil after esc")
	}
	if cmd != nil {
		t.Error("esc should return nil cmd")
	}
}

func TestUpdateAgentPicker_nilPicker(t *testing.T) {
	m := Model{screen: screenAgentPicker}
	m.list = newList(nil)
	m.agentPicker = nil

	updated, _ := m.updateAgentPicker(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("nil picker should transition to screenList, got %d", um.screen)
	}
}

func TestUpdateAgentPicker_navigate(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"aider":  {Cmd: "aider"},
			"claude": {Cmd: "claude"},
		},
	}
	m := Model{screen: screenAgentPicker, cfg: cfg}
	m.list = newList(nil)
	m.agentPicker = newAgentPicker(cfg, "aider", &agentPickerPending{})

	updated, cmd := m.updateAgentPicker(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	um := updated.(Model)
	if um.screen != screenAgentPicker {
		t.Errorf("navigation should stay on screenAgentPicker, got %d", um.screen)
	}
	if cmd != nil {
		t.Error("navigation should return nil cmd")
	}
}

func TestUpdateAgentPicker_View(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	m := Model{screen: screenAgentPicker, cfg: cfg}
	m.list = newList(nil)
	m.agentPicker = newAgentPicker(cfg, "claude", &agentPickerPending{})
	v := m.View()
	if !strings.Contains(v.Content, "Select Agent") {
		t.Errorf("View() on screenAgentPicker should show agent picker: %q", v.Content)
	}
}

// ── shellAction with selection tests ──────────────────────────────────────────

// TestShellAction_worktreeItem_sessionExists: ShellSessionID is set, so
// shellAction returns an attachMsg with that ID immediately — no tmux calls.
func TestShellAction_worktreeItem_sessionExists(t *testing.T) {
	client := tmux.NewClient(&mockTmuxRunner{})

	items := []Item{{
		Project:        "myapp",
		Branch:         "feat",
		Path:           t.TempDir(),
		Group:          groupWorktree,
		ShellSessionID: "$42",
	}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, tmuxClient: client}
	m.list = newList(listItems)

	cmd := m.shellAction()
	if cmd == nil {
		t.Fatal("shellAction on worktree item should return non-nil cmd")
	}
	msg := cmd()
	am, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want attachMsg", msg)
	}
	if am.session != "$42" {
		t.Errorf("session = %q, want $42", am.session)
	}
}

// TestShellAction_worktreeItem_sessionCreated: no existing shell session;
// session.Service.Start is called which executes the full tmux sequence.
// The attachMsg.session value is the stable session ID returned by SessionID().
func TestShellAction_worktreeItem_sessionCreated(t *testing.T) {
	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: "", exitCode: 1},      // list-sessions → no sessions (exit 1 = empty)
			{stdout: "$99\n", exitCode: 0}, // new-session-with-env → session_id via -P -F
			{stdout: "", exitCode: 0},      // set-option @codeherd_status
			{stdout: "", exitCode: 0},      // set-option @codeherd_started_at
			{stdout: "", exitCode: 0},      // set-option @codeherd_canonical_name
			{stdout: "", exitCode: 0},      // set-option @codeherd_session_type
		},
	}
	client := tmux.NewClient(runner)
	hrd, cfg := tuiHerd(t, runner, "feat")

	path := t.TempDir()
	items := []Item{{Ref: hrd.Ref("myapp", "feat"), Project: "myapp", Branch: "feat", Path: path, Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, tmuxClient: client, cfg: cfg, herd: hrd}
	m.list = newList(listItems)

	cmd := m.shellAction()
	if cmd == nil {
		t.Fatal("shellAction should return non-nil cmd")
	}
	msg := cmd()
	am, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want attachMsg", msg)
	}
	// attachMsg.session is now the stable session ID from Launch's Handle.
	if am.session != "$99" {
		t.Errorf("session = %q, want $99", am.session)
	}
}

// TestShellAction_newSessionExecError: ListSessions succeeds (no sessions),
// then NewSessionWithEnv fails at the exec level (runner returns error).
func TestShellAction_newSessionExecError(t *testing.T) {
	// errOnCallRunner: call 0 = list-sessions (succeeds, empty), call 1 = new-session (exec error).
	errRunner := &errOnCallRunner{errOnCall: 1}
	client := tmux.NewClient(errRunner)
	hrd, cfg := tuiHerd(t, errRunner, "feat")

	path := t.TempDir()
	items := []Item{{Ref: hrd.Ref("myapp", "feat"), Project: "myapp", Branch: "feat", Path: path, Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, tmuxClient: client, cfg: cfg, herd: hrd}
	m.list = newList(listItems)

	cmd := m.shellAction()
	if cmd == nil {
		t.Fatal("shellAction should return non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("cmd() returned %T, want errMsg on NewSessionWithEnv exec error", msg)
	}
}

// TestShellAction_newSessionError: ListSessions succeeds (no sessions),
// then NewSessionWithEnv returns a non-zero exit code.
func TestShellAction_newSessionError(t *testing.T) {
	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: "", exitCode: 1}, // list-sessions → no sessions (exit 1 = empty)
			{stdout: "", exitCode: 1}, // new-session-with-env → fails (non-zero exit)
		},
	}
	client := tmux.NewClient(runner)
	hrd, cfg := tuiHerd(t, runner, "feat")

	path := t.TempDir()
	items := []Item{{Ref: hrd.Ref("myapp", "feat"), Project: "myapp", Branch: "feat", Path: path, Group: groupWorktree}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, tmuxClient: client, cfg: cfg, herd: hrd}
	m.list = newList(listItems)

	cmd := m.shellAction()
	if cmd == nil {
		t.Fatal("shellAction should return non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("cmd() returned %T, want errMsg on NewSessionWithEnv exit error", msg)
	}
}

// errOnCallRunner returns an error on a specific call index.
type errOnCallRunner struct {
	callIdx   int
	errOnCall int
}

func (r *errOnCallRunner) Run(args ...string) (string, string, int, error) {
	idx := r.callIdx
	r.callIdx++
	if idx == r.errOnCall {
		return "", "", 0, fmt.Errorf("forced error")
	}
	return "", "", 0, nil
}

// ── startSessionAfterCreate tests ─────────────────────────────────────────────

func TestStartSessionAfterCreate_agentNotFound(t *testing.T) {
	// no agents configured; Launch's agent resolution must fail with errMsg.
	hrd, cfg := tuiHerd(t, &mockTmuxRunner{}, "feat")
	m := Model{cfg: cfg, herd: hrd}

	cmd := m.startSessionAfterCreate(worktreeCreatedMsg{
		ref:   hrd.Ref("myapp", "feat"),
		path:  t.TempDir(),
		agent: "nonexistent",
	})
	if cmd == nil {
		t.Fatal("startSessionAfterCreate should return non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("cmd() returned %T, want errMsg for missing agent", msg)
	}
}

// ── cloneAction with per-project hooks tests ─────────────────────────────────

func TestCloneAction_projectItem_returnsCmd(t *testing.T) {
	cfg := &config.Config{
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	items := []Item{{Project: "myapp", Group: groupProject, Cloned: false}}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m := Model{screen: screenList, cfg: cfg}
	m.list = newList(listItems)

	cmd := m.cloneAction()
	if cmd == nil {
		t.Fatal("cloneAction() on uncloned project should return non-nil cmd")
	}
	// We don't execute the cmd as it would try real git operations,
	// but we verify it's properly constructed.
}

// ── worktreeCreatedMsg with attach tests ──────────────────────────────────────

func TestModel_Update_worktreeCreatedMsg_withAttachAndAgent(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	m := Model{screen: screenForm, cfg: cfg, herd: herd.New(cfg, nil, herd.Deps{})}
	m.list = newList(nil)
	m.form = newFormModel(formContext{project: "myapp", baseBranch: "main"}, cfg, nil)

	updated, cmd := m.Update(worktreeCreatedMsg{
		ref:    herd.Ref{Project: "myapp", Branch: "feat"},
		path:   "/tmp/wt",
		attach: true,
		agent:  "claude",
	})
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d", um.screen, screenList)
	}
	if cmd == nil {
		t.Error("worktreeCreatedMsg with attach+agent should return non-nil cmd")
	}
}
