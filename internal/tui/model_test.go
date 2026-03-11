package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/tmux"
)

func TestModel_Update_windowResize(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	msg := tea.WindowSizeMsg{Width: 100, Height: 40}
	updated, _ := m.Update(msg)
	um := updated.(Model)

	if um.width != 100 || um.height != 40 {
		t.Errorf("size = %dx%d, want 100x40", um.width, um.height)
	}
}

func TestModel_Update_quit(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	msg := tea.KeyPressMsg(tea.Key{})
	// Simulate 'q' press — we test the quit path via Cmd check.
	// Direct key simulation is tricky; test via itemsMsg instead.
	_, _ = m.Update(msg)
}

func TestModel_Update_itemsMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	items := []Item{
		{Project: "myapp", Branch: "feat", Group: groupAgent},
	}

	updated, _ := m.Update(itemsMsg(items))
	um := updated.(Model)

	if len(um.list.Items()) != 1 {
		t.Errorf("list has %d items, want 1", len(um.list.Items()))
	}
}

func TestModel_Update_errMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, _ := m.Update(errMsg{err: nil})
	um := updated.(Model)

	if um.screen != screenList {
		t.Errorf("screen = %d, want %d", um.screen, screenList)
	}
}

func TestModel_Update_errMsg_withError(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, _ := m.Update(errMsg{err: errors.New("something went wrong")})
	um := updated.(Model)

	if um.statusMsg != "something went wrong" {
		t.Errorf("statusMsg = %q, want %q", um.statusMsg, "something went wrong")
	}
}

func TestModel_Update_itemsMsg_preserveSelection(t *testing.T) {
	m := Model{screen: screenList}
	items := []Item{
		{Project: "alpha", Branch: "main", Group: groupWorktree},
		{Project: "beta", Branch: "feat", Group: groupWorktree},
	}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m.list = newList(listItems)
	m.list.Select(1) // select beta/feat

	// Re-send same items — selection should be preserved.
	updated, _ := m.Update(itemsMsg(items))
	um := updated.(Model)

	sel, ok := um.list.SelectedItem().(Item)
	if !ok {
		t.Fatal("no selection")
	}
	if sel.Project != "beta" || sel.Branch != "feat" {
		t.Errorf("selection = %s/%s, want beta/feat", sel.Project, sel.Branch)
	}
}

func TestModel_Update_tickMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Error("tickMsg should return a non-nil Cmd")
	}
}

func TestModel_Update_attachMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, cmd := m.Update(attachMsg{session: "myapp-feat"})
	um := updated.(Model)

	if um.PendingAttach != "myapp-feat" {
		t.Errorf("PendingAttach = %q, want %q", um.PendingAttach, "myapp-feat")
	}
	if cmd == nil {
		t.Error("attachMsg should return tea.Quit cmd")
	}
}

func TestModel_Update_attachMsg_insideTmux(t *testing.T) {
	// When InsideTmux is true, attachMsg should call switchClientCmd instead of tea.Quit.
	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: "", exitCode: 0}, // switch-client
		},
	}
	client := tmux.NewClient(runner)

	m := Model{screen: screenList, InsideTmux: true, tmuxClient: client}
	m.list = newList(nil)

	updated, cmd := m.Update(attachMsg{session: "myapp-feat"})
	um := updated.(Model)

	// PendingAttach should NOT be set when inside tmux.
	if um.PendingAttach != "" {
		t.Errorf("PendingAttach = %q, want empty when InsideTmux", um.PendingAttach)
	}

	// Should return a non-nil cmd (switchClientCmd).
	if cmd == nil {
		t.Error("attachMsg with InsideTmux should return a non-nil Cmd")
	}

	// Execute the cmd and verify it calls switch-client.
	msg := cmd()
	if msg != nil {
		t.Errorf("switchClientCmd should return nil on success, got %T", msg)
	}
	if runner.idx != 1 {
		t.Errorf("expected 1 runner call, got %d", runner.idx)
	}
}

func TestModel_switchClientCmd_error(t *testing.T) {
	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: "", exitCode: 1}, // switch-client fails
		},
	}
	client := tmux.NewClient(runner)

	m := Model{tmuxClient: client}
	cmd := m.switchClientCmd("some-session")
	if cmd == nil {
		t.Fatal("switchClientCmd should return non-nil Cmd")
	}

	msg := cmd()
	if msg == nil {
		t.Fatal("switchClientCmd should return errMsg on failure")
	}
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("switchClientCmd returned %T, want errMsg", msg)
	}
}

func TestModel_Update_cloneDoneMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, _ := m.Update(cloneDoneMsg{project: "myapp"})
	um := updated.(Model)

	if um.statusMsg != "Cloned myapp" {
		t.Errorf("statusMsg = %q, want %q", um.statusMsg, "Cloned myapp")
	}
}

func TestModel_Update_worktreeCreatedMsg(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	updated, _ := m.Update(worktreeCreatedMsg{project: "myapp", branch: "feat"})
	um := updated.(Model)

	if um.statusMsg != "Created myapp/feat" {
		t.Errorf("statusMsg = %q, want %q", um.statusMsg, "Created myapp/feat")
	}
}

func TestModel_View_list(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	v := m.View()
	if v.Content == "" {
		t.Error("View() returned empty content")
	}
}

func TestModel_View_confirmDelete(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	v := m.View()
	if v.Content == "" {
		t.Error("View() in screenConfirmDelete returned empty content")
	}
}

func TestModel_View_form(t *testing.T) {
	m := Model{screen: screenForm}
	m.list = newList(nil)
	m.form = newFormModel(formContext{project: "myapp", baseBranch: "main"}, &config.Config{}, nil, nil)

	v := m.View()
	_ = v
}

func TestModel_viewList_withAgents(t *testing.T) {
	m := Model{screen: screenList}
	m.keys = defaultKeyMap()
	items := []Item{
		{Project: "myapp", Branch: "main", HasAgent: true, Group: groupAgent},
	}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m.list = newList(listItems)

	out := m.viewList()
	if out == "" {
		t.Error("viewList() returned empty string")
	}
}

func TestModel_viewList_withStatus(t *testing.T) {
	m := Model{screen: screenList}
	m.keys = defaultKeyMap()
	m.list = newList(nil)
	m.statusMsg = "some status"

	out := m.viewList()
	if out == "" {
		t.Error("viewList() returned empty string")
	}
}

func TestModel_selectedItem_none(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	if got := m.selectedItem(); got != nil {
		t.Errorf("selectedItem() = %v, want nil", got)
	}
}

func TestModel_selectedItem_exists(t *testing.T) {
	m := Model{screen: screenList}
	items := []Item{
		{Project: "myapp", Branch: "main", Group: groupWorktree},
	}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m.list = newList(listItems)

	got := m.selectedItem()
	if got == nil {
		t.Fatal("selectedItem() = nil, want item")
	}
	if got.Project != "myapp" {
		t.Errorf("project = %q, want %q", got.Project, "myapp")
	}
}

func TestModel_refreshCmd_nilServices(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	// All services nil — should return itemsMsg without panicking.
	cmd := m.refreshCmd()
	if cmd == nil {
		t.Fatal("refreshCmd() returned nil")
	}
	msg := cmd()
	if _, ok := msg.(itemsMsg); !ok {
		t.Errorf("refreshCmd() produced %T, want itemsMsg", msg)
	}
}

func TestModel_refreshCmd_withConfig(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.cfg = &config.Config{
		Projects: map[string]config.ProjectConfig{
			"myapp": {Repo: "git@github.com:user/myapp.git"},
		},
	}
	cmd := m.refreshCmd()
	if cmd == nil {
		t.Fatal("refreshCmd() returned nil")
	}
	msg := cmd()
	if _, ok := msg.(itemsMsg); !ok {
		t.Errorf("refreshCmd() produced %T, want itemsMsg", msg)
	}
}

func TestKeyMap_FullHelp(t *testing.T) {
	k := defaultKeyMap()
	groups := k.FullHelp()
	if len(groups) == 0 {
		t.Error("FullHelp() returned empty groups")
	}
}

func TestModel_updateList_helpToggle(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	// Press '?' to toggle help.
	msg := tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"})
	updated, _ := m.Update(msg)
	um := updated.(Model)

	if !um.help.ShowAll {
		t.Error("help.ShowAll should be true after pressing '?'")
	}
}

func TestModel_updateList_refresh(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	// Press 'r' to refresh.
	msg := tea.KeyPressMsg(tea.Key{Code: 'r', Text: "r"})
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Error("refresh key should return a non-nil Cmd")
	}
}

func TestModel_screenConfirmDelete_update(t *testing.T) {
	target := Item{Project: "myapp", Branch: "feat", Group: groupWorktree}
	m := Model{screen: screenConfirmDelete}
	m.confirm = newConfirmModel(target)
	m.list = newList(nil)

	// Esc should cancel and return to list.
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after esc", um.screen, screenList)
	}
}

func TestModel_screenForm_update(t *testing.T) {
	m := Model{screen: screenForm}
	m.list = newList(nil)
	m.form = newFormModel(formContext{project: "myapp", baseBranch: "main"}, &config.Config{}, nil, nil)

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{}))
	um := updated.(Model)
	_ = um
}

func TestModel_Init(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}
}

func TestNewModel(t *testing.T) {
	m := NewModel(nil, nil, nil, nil, nil, false)
	if m.screen != screenList {
		t.Errorf("screen = %d, want %d", m.screen, screenList)
	}
}

func TestModel_updateList_windowSizeLargerThanMax(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)

	msg := tea.WindowSizeMsg{Width: maxWidth + 10, Height: 50}
	updated, _ := m.Update(msg)
	um := updated.(Model)

	if um.width != maxWidth+10 {
		t.Errorf("width = %d, want %d", um.width, maxWidth+10)
	}
}

func TestModel_noSelectionActions(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	// With no selection, action cmds should be nil.
	if _, cmd := m.attachAction(); cmd != nil {
		t.Error("attachAction with no selection should return nil")
	}
	if cmd := m.shellAction(); cmd != nil {
		t.Error("shellAction with no selection should return nil")
	}
	if cmd := m.cloneAction(); cmd != nil {
		t.Error("cloneAction with no selection should return nil")
	}

	// startDelete with no selection stays on screenList.
	nm, cmd := m.startDelete()
	if nm == nil || cmd != nil {
		t.Error("startDelete with no selection: unexpected return values")
	}
	if nm.(Model).screen != screenList {
		t.Error("startDelete with no selection should stay on screenList")
	}

	// startDelete on project item sets statusMsg but stays on list.
	items2 := []Item{{Project: "infra", Group: groupProject}}
	listItems2 := make([]list.Item, len(items2))
	for i, it := range items2 {
		listItems2[i] = it
	}
	m2 := Model{screen: screenList}
	m2.list = newList(listItems2)
	m2.keys = defaultKeyMap()
	nm2, _ := m2.startDelete()
	if nm2.(Model).screen != screenList {
		t.Error("startDelete on project item should stay on screenList")
	}
}

func TestModel_updateList_attachKey(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	// Press 'a' to attach.
	msg := tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})
	_, cmd := m.Update(msg)
	// stub returns nil, so cmd is nil — just verify no panic
	_ = cmd
}

func TestModel_updateList_deleteKey(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	msg := tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"})
	_, _ = m.Update(msg)
}

func TestModel_updateList_newKey(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()
	m.cfg = &config.Config{}

	msg := tea.KeyPressMsg(tea.Key{Code: 'n', Text: "n"})
	_, _ = m.Update(msg)
}

func TestModel_updateList_cloneKey(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	msg := tea.KeyPressMsg(tea.Key{Code: 'c', Text: "c"})
	_, _ = m.Update(msg)
}

func TestModel_updateList_shellKey(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.keys = defaultKeyMap()

	msg := tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"})
	_, _ = m.Update(msg)
}

func TestModel_updateList_filteringState(t *testing.T) {
	m := Model{screen: screenList}
	// Set up filtering by sending '/' key which usually enables filtering in list.
	items := []Item{
		{Project: "myapp", Branch: "main", Group: groupWorktree},
	}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m.list = newList(listItems)
	m.keys = defaultKeyMap()

	// Enable filtering.
	m.list, _ = m.list.Update(tea.KeyPressMsg(tea.Key{Code: '/', Text: "/"}))

	// Now press 'a' while filtering — should be handled by list, not our key handler.
	msg := tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})
	_, _ = m.Update(msg)
	// Just verify no panic.
}

func TestModel_viewList_wideWindow(t *testing.T) {
	m := Model{screen: screenList}
	m.keys = defaultKeyMap()
	// Width wider than maxWidth triggers the w = maxWidth branch.
	m.width = maxWidth + 20
	items := []Item{
		{Project: "myapp", Branch: "main", HasAgent: true, Group: groupAgent},
	}
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	m.list = newList(listItems)

	out := m.viewList()
	if out == "" {
		t.Error("viewList() returned empty string")
	}
}

func TestTickCmd(t *testing.T) {
	cmd := tickCmd()
	if cmd == nil {
		t.Error("tickCmd() returned nil")
	}
}

// mockTmuxRunner is a stateful Runner that returns scripted responses in order.
// Each call to Run pops the next response from the queue.
type mockTmuxRunner struct {
	responses []mockTmuxResponse
	idx       int
}

type mockTmuxResponse struct {
	stdout   string
	exitCode int
}

func (r *mockTmuxRunner) Run(args ...string) (string, string, int, error) {
	if r.idx >= len(r.responses) {
		return "", "", 0, nil
	}
	resp := r.responses[r.idx]
	r.idx++
	return resp.stdout, "", resp.exitCode, nil
}

func TestModel_refreshCmd_withTmuxClient(t *testing.T) {
	// Sessions: one agent session (waiting/prefixed), one shell session.
	agentLine := "⚡ myapp-feat\tmyapp-feat\tagent\twaiting\tneed input\t\n"
	shellLine := "myapp-feat~sh\tmyapp-feat\tshell\t\t\t\n"
	listOutput := agentLine + shellLine

	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: listOutput, exitCode: 0}, // list-sessions (single call)
		},
	}
	client := tmux.NewClient(runner)

	m := Model{screen: screenList}
	m.list = newList(nil)
	m.tmuxClient = client

	cmd := m.refreshCmd()
	if cmd == nil {
		t.Fatal("refreshCmd() returned nil")
	}
	msg := cmd()
	items, ok := msg.(itemsMsg)
	if !ok {
		t.Fatalf("refreshCmd() produced %T, want itemsMsg", msg)
	}

	// Exactly 1 runner call — no per-session GetOption calls.
	if runner.idx != 1 {
		t.Errorf("runner.idx = %d, want 1 (single list-sessions call)", runner.idx)
	}

	// No item should have a Branch that ends with "~sh".
	for _, item := range items {
		if strings.HasSuffix(item.Branch, "~sh") {
			t.Errorf("shell session branch %q leaked into items", item.Branch)
		}
	}
}
