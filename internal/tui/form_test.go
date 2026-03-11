package tui

import (
	"regexp"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

func TestNewFormModel_setsContextFields(t *testing.T) {
	ctx := formContext{project: "myapp", baseBranch: "main"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	f := newFormModel(ctx, cfg, nil)

	if f.project != "myapp" {
		t.Errorf("project = %q, want %q", f.project, "myapp")
	}
	if f.baseBranch != "main" {
		t.Errorf("baseBranch = %q, want %q", f.baseBranch, "main")
	}
	if !f.attach {
		t.Error("attach should default to true")
	}
}

func TestNewFormModel_singleAgent(t *testing.T) {
	ctx := formContext{project: "myapp", baseBranch: "main"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	f := newFormModel(ctx, cfg, nil)

	if f.agent != "claude" {
		t.Errorf("agent = %q, want %q (auto-selected single agent)", f.agent, "claude")
	}
}

func TestNewFormModel_multipleAgents(t *testing.T) {
	ctx := formContext{project: "myapp", baseBranch: "main"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
			"aider":  {Cmd: "aider"},
		},
	}

	f := newFormModel(ctx, cfg, nil)

	// With multiple agents, the first sorted agent should be pre-selected.
	if f.agent != "aider" {
		t.Errorf("agent = %q, want %q (first sorted agent)", f.agent, "aider")
	}
}

func TestNewFormModel_noAgents(t *testing.T) {
	ctx := formContext{project: "myapp", baseBranch: "main"}
	cfg := &config.Config{}

	f := newFormModel(ctx, cfg, nil)

	if f.agent != "" {
		t.Errorf("agent = %q, want empty with no agents configured", f.agent)
	}
}

func TestFormModel_View(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "main"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	f := newFormModel(ctx, cfg, nil)

	out := f.View()
	if out == "" {
		t.Error("View() returned empty string")
	}
}

func TestFormModel_completedInitiallyFalse(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "main"}
	cfg := &config.Config{}
	f := newFormModel(ctx, cfg, nil)

	if f.completed() {
		t.Error("form should not be completed initially")
	}
}

func TestShowForm_fromProject(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(toListItems([]Item{
		{Project: "api", Group: groupProject},
	}))
	m.cfg = &config.Config{
		Projects: map[string]config.ProjectConfig{
			"api": {DefaultBranch: "develop"},
		},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	updated, _ := m.showForm()
	um := updated.(Model)
	if um.screen != screenForm {
		t.Errorf("screen = %d, want %d", um.screen, screenForm)
	}
	if um.form == nil {
		t.Fatal("form should be initialized after showForm()")
	}
	if um.form.project != "api" {
		t.Errorf("form.project = %q, want %q", um.form.project, "api")
	}
	if um.form.baseBranch != "develop" {
		t.Errorf("form.baseBranch = %q, want %q", um.form.baseBranch, "develop")
	}
}

func TestShowForm_fromProjectDefaultBranch(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(toListItems([]Item{
		{Project: "api", Group: groupProject},
	}))
	m.cfg = &config.Config{
		Projects: map[string]config.ProjectConfig{
			"api": {},
		},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	updated, _ := m.showForm()
	um := updated.(Model)
	if um.form == nil {
		t.Fatal("form should be initialized after showForm()")
	}
	if um.form.baseBranch != "main" {
		t.Errorf("form.baseBranch = %q, want %q (default)", um.form.baseBranch, "main")
	}
}

func TestShowForm_fromWorktree(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(toListItems([]Item{
		{Project: "api", Branch: "feature-1", Group: groupWorktree},
	}))
	m.cfg = &config.Config{
		Projects: map[string]config.ProjectConfig{
			"api": {},
		},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	updated, _ := m.showForm()
	um := updated.(Model)
	if um.form == nil {
		t.Fatal("form should be initialized after showForm()")
	}
	if um.form.baseBranch != "feature-1" {
		t.Errorf("form.baseBranch = %q, want %q", um.form.baseBranch, "feature-1")
	}
}

func TestShowForm_noSelection(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(nil)
	m.cfg = &config.Config{}

	updated, cmd := m.showForm()
	um := updated.(Model)
	if um.screen != screenList {
		t.Errorf("screen = %d, want %d when no selection", um.screen, screenList)
	}
	if cmd != nil {
		t.Error("cmd should be nil when no selection")
	}
}

func TestUpdateForm_escKey(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "main"}
	cfg := &config.Config{}
	m := Model{screen: screenForm}
	m.list = newList(nil)
	m.form = newFormModel(ctx, cfg, nil)

	msg := tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	updated, _ := m.updateForm(msg)
	um := updated.(Model)

	if um.screen != screenList {
		t.Errorf("screen = %d, want %d after esc", um.screen, screenList)
	}
	if um.form != nil {
		t.Error("form should be nil after esc")
	}
}

func TestUpdateForm_nonEscKey(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "main"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	m := Model{screen: screenForm}
	m.list = newList(nil)
	m.form = newFormModel(ctx, cfg, nil)
	// Initialize the form so it can handle updates.
	m.form.Init()

	msg := tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})
	updated, _ := m.updateForm(msg)
	um := updated.(Model)

	if um.screen != screenForm {
		t.Errorf("screen = %d, want %d after non-esc", um.screen, screenForm)
	}
}

func TestFormModel_submit_returnsCmd(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: "develop"}
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}
	f := newFormModel(ctx, cfg, nil)
	f.branch = "my-feature"

	cmd := f.submit()
	if cmd == nil {
		t.Fatal("submit() returned nil cmd")
	}
}

func TestFormModel_submit_withoutBaseBranch_returnsCmd(t *testing.T) {
	ctx := formContext{project: "api", baseBranch: ""}
	cfg := &config.Config{}
	f := newFormModel(ctx, cfg, nil)
	f.branch = "my-feature"

	cmd := f.submit()
	if cmd == nil {
		t.Fatal("submit() returned nil cmd")
	}
}

func TestShowForm_fromAgent(t *testing.T) {
	m := Model{screen: screenList}
	m.list = newList(toListItems([]Item{
		{Project: "api", Branch: "feat-x", Group: groupAgent},
	}))
	m.cfg = &config.Config{
		Projects: map[string]config.ProjectConfig{
			"api": {},
		},
		Agents: map[string]config.AgentConfig{
			"claude": {Cmd: "claude"},
		},
	}

	updated, _ := m.showForm()
	um := updated.(Model)
	if um.form == nil {
		t.Fatal("form should be initialized after showForm()")
	}
	if um.form.baseBranch != "feat-x" {
		t.Errorf("form.baseBranch = %q, want %q", um.form.baseBranch, "feat-x")
	}
}

// toListItems converts a slice of Items to list.Items.
func toListItems(items []Item) []list.Item {
	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}
