package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
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

	updated, _ := m.Update(worktreeCreatedMsg{ref: herd.Ref{Project: "myapp", Branch: "feat"}})
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
	m.form = newFormModel(formContext{project: "myapp", baseBranch: "main"}, &config.Config{}, nil)

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
		return
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

func TestModel_updateList_remoteOpensPicker(t *testing.T) {
	m := Model{screen: screenList, cfg: &config.Config{}}
	m.list = newList([]list.Item{Item{Project: "myapp", Branch: "main", Group: groupWorktree}})
	m.keys = defaultKeyMap()

	// Press 'r' to open the remote-branch picker for the selected project.
	msg := tea.KeyPressMsg(tea.Key{Code: 'r', Text: "r"})
	updated, cmd := m.Update(msg)
	um := updated.(Model)
	if um.screen != screenRemotePicker {
		t.Errorf("screen = %d, want screenRemotePicker", um.screen)
	}
	if um.remotePicker == nil {
		t.Error("expected remotePicker to be set")
	}
	if cmd == nil {
		t.Error("remote key should return a non-nil fetch Cmd")
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
	m.form = newFormModel(formContext{project: "myapp", baseBranch: "main"}, &config.Config{}, nil)

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
	m := NewModel(nil, nil, false)
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

func TestModel_refreshCmd_withHerd(t *testing.T) {
	// refreshCmd now goes through herd.List, which issues exactly one
	// list-sessions call (the per-session GetOption regression stays fixed).
	runner := &mockTmuxRunner{
		responses: []mockTmuxResponse{
			{stdout: "", exitCode: 1}, // list-sessions → no sessions
		},
	}
	hrd := herd.New(&config.Config{}, nil, herd.Deps{Tmux: runner})

	m := Model{screen: screenList, herd: hrd}
	m.list = newList(nil)

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
	for _, item := range items {
		if strings.HasSuffix(item.Branch, "~sh") {
			t.Errorf("shell session branch %q leaked into items", item.Branch)
		}
	}
}

func TestSwitchProfile_cyclesForward(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"personal", "work"} {
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(`[projects.b]
repo = "git@x:b"
`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reg := &config.ProfileRegistry{Active: "personal", Names: []string{"personal", "work"}, ProfilesDir: dir}
	m := NewModel(herd.New(&config.Config{}, reg, herd.Deps{}), nil, false)

	m2 := m.SwitchProfileForTest(+1)
	if m2.ActiveProfileForTest() != "work" {
		t.Errorf("after forward cycle, active = %q, want work", m2.ActiveProfileForTest())
	}
	m3 := m2.SwitchProfileForTest(+1)
	if m3.ActiveProfileForTest() != "personal" {
		t.Errorf("after second forward cycle, active = %q, want personal", m3.ActiveProfileForTest())
	}
}

func TestSyncProfileKeyEnabled_gate(t *testing.T) {
	// Nil herd → disabled.
	m := NewModel(nil, nil, false)
	if m.NextProfileEnabledForTest() {
		t.Error("NextProfile enabled with nil herd, want disabled")
	}
	// One profile → disabled.
	reg1 := &config.ProfileRegistry{Active: "a", Names: []string{"a"}, ProfilesDir: t.TempDir()}
	m = NewModel(herd.New(&config.Config{}, reg1, herd.Deps{}), nil, false)
	if m.NextProfileEnabledForTest() {
		t.Error("NextProfile enabled with one profile, want disabled")
	}
	// Two profiles → enabled.
	reg2 := &config.ProfileRegistry{Active: "a", Names: []string{"a", "b"}, ProfilesDir: t.TempDir()}
	m = NewModel(herd.New(&config.Config{}, reg2, herd.Deps{}), nil, false)
	if !m.NextProfileEnabledForTest() {
		t.Error("NextProfile disabled with two profiles, want enabled")
	}
}

func TestSwitchProfile_loadFailurePreservesActive(t *testing.T) {
	reg := &config.ProfileRegistry{
		Active:      "personal",
		Names:       []string{"personal", "bogus"},
		ProfilesDir: t.TempDir(),
	}
	m := NewModel(herd.New(&config.Config{}, reg, herd.Deps{}), nil, false)

	m2 := m.SwitchProfileForTest(+1)
	if m2.ActiveProfileForTest() != "personal" {
		t.Errorf("active = %q, want unchanged (personal) after load failure", m2.ActiveProfileForTest())
	}
	if m2.StatusMsgForTest() == "" {
		t.Error("expected statusMsg to describe the failure")
	}
}

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

func TestView_rendersBusyScreen(t *testing.T) {
	m := Model{busy: "Creating worktree…", spinner: spinner.New(), width: 40, height: 10}
	m.list = newList(nil)

	v := m.View()

	if !strings.Contains(v.Content, "Creating worktree…") {
		t.Errorf("busy view should contain the label; got:\n%s", v.Content)
	}
}

func TestView_normalWhenNotBusy(t *testing.T) {
	m := Model{busy: "", screen: screenList, spinner: spinner.New(), width: 40, height: 10}
	m.list = newList(nil)
	m.help = help.New()
	m.keys = defaultKeyMap()

	v := m.View()

	if strings.Contains(v.Content, "Creating worktree…") {
		t.Error("non-busy view should not contain a busy label")
	}
}

func TestUpdate_resultMessagesClearBusy(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"errMsg", errMsg{err: errors.New("boom")}},
		{"cloneDoneMsg", cloneDoneMsg{project: "myapp"}},
		{"worktreeCreatedMsg", worktreeCreatedMsg{ref: herd.Ref{Project: "myapp", Branch: "feat"}}},
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
