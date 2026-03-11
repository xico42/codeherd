package tui

import (
	"fmt"
	"os"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
)

const (
	screenList = iota
	screenForm
	screenConfirmDelete
	screenAgentPicker
)

const maxWidth = 80
const refreshInterval = 3 * time.Second

// Messages
type tickMsg time.Time
type itemsMsg []Item
type errMsg struct{ err error }
type attachMsg struct{ session string }
type cloneDoneMsg struct{ project string }
type worktreeCreatedMsg struct {
	project string
	branch  string
	path    string
	attach  bool
	agent   string
}

// Model is the top-level Bubble Tea model.
type Model struct {
	screen int
	list   list.Model
	keys   keyMap
	help   help.Model

	cfg        *config.Config
	wtSvc      *worktree.Service
	sesSvc     *session.Service
	projSvc    *project.Service
	tmuxClient *tmux.Client

	width  int
	height int

	// Set before quitting to trigger tmux attach.
	PendingAttach string

	// When true, attach uses switch-client instead of quitting.
	InsideTmux bool

	// Delete confirmation state.
	confirm *confirmModel

	// Status message for async operations.
	statusMsg string

	// Form sub-model.
	form *formModel

	// Agent picker sub-model.
	agentPicker *agentPickerModel
}

// NewModel creates the TUI model with all required services.
func NewModel(
	cfg *config.Config,
	wtSvc *worktree.Service,
	sesSvc *session.Service,
	projSvc *project.Service,
	tmuxClient *tmux.Client,
	insideTmux bool,
) Model {
	keys := defaultKeyMap()
	l := newList(nil)
	h := help.New()

	return Model{
		screen:     screenList,
		list:       l,
		keys:       keys,
		help:       h,
		cfg:        cfg,
		wtSvc:      wtSvc,
		sesSvc:     sesSvc,
		projSvc:    projSvc,
		tmuxClient: tmuxClient,
		InsideTmux: insideTmux,
	}
}

func newList(items []list.Item) list.Model {
	if items == nil {
		items = []list.Item{}
	}
	l := list.New(items, newDelegate(), maxWidth, 20)
	l.Title = "codeherd"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	return l
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		w := msg.Width
		if w > maxWidth {
			w = maxWidth
		}
		m.list.SetSize(w, msg.Height-4) // room for title + help
		m.help.SetWidth(w)
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refreshCmd(), tickCmd())

	case itemsMsg:
		items := make([]list.Item, len(msg))
		for i, item := range msg {
			items[i] = item
		}
		// Preserve selection.
		var selProject, selBranch string
		if sel, ok := m.list.SelectedItem().(Item); ok {
			selProject = sel.Project
			selBranch = sel.Branch
		}
		cmd := m.list.SetItems(items)
		if selProject != "" {
			for i, li := range items {
				if it, ok := li.(Item); ok && it.Project == selProject && it.Branch == selBranch {
					m.list.Select(i)
					break
				}
			}
		}
		return m, cmd

	case errMsg:
		if msg.err != nil {
			m.statusMsg = msg.err.Error()
		}
		return m, nil

	case attachMsg:
		if m.InsideTmux {
			return m, m.switchClientCmd(msg.session)
		}
		m.PendingAttach = msg.session
		return m, tea.Quit

	case cloneDoneMsg:
		m.statusMsg = fmt.Sprintf("Cloned %s", msg.project)
		return m, m.refreshCmd()

	case worktreeCreatedMsg:
		m.statusMsg = fmt.Sprintf("Created %s/%s", msg.project, msg.branch)
		m.screen = screenList
		m.form = nil
		if msg.attach && msg.agent != "" {
			return m, m.startSessionAfterCreate(msg)
		}
		return m, m.refreshCmd()
	}

	// Route to sub-screens.
	switch m.screen {
	case screenConfirmDelete:
		return m.updateConfirmDelete(msg)
	case screenForm:
		return m.updateForm(msg)
	case screenAgentPicker:
		return m.updateAgentPicker(msg)
	default:
		return m.updateList(msg)
	}
}

func (m Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Don't handle custom keys while filtering.
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Attach):
			return m.attachAction()

		case key.Matches(msg, m.keys.Shell):
			return m, m.shellAction()

		case key.Matches(msg, m.keys.Clone):
			return m, m.cloneAction()

		case key.Matches(msg, m.keys.New):
			return m.showForm()

		case key.Matches(msg, m.keys.Delete):
			return m.startDelete()

		case key.Matches(msg, m.keys.Refresh):
			return m, m.refreshCmd()

		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() tea.View {
	var content string
	switch m.screen {
	case screenForm:
		content = m.form.View()
	case screenConfirmDelete:
		if m.confirm != nil {
			content = m.confirm.View()
		}
	case screenAgentPicker:
		if m.agentPicker != nil {
			content = m.agentPicker.View()
		}
	default:
		content = m.viewList()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m Model) viewList() string {
	// Count agents for title bar.
	agentCount := 0
	for _, item := range m.list.Items() {
		if it, ok := item.(Item); ok && it.HasAgent {
			agentCount++
		}
	}

	tb := titleStyle.Render("codeherd")
	if agentCount > 0 {
		counter := dimStyle.Render(fmt.Sprintf("%d agents", agentCount))
		pad := maxWidth - lipgloss.Width(tb) - lipgloss.Width(counter)
		if pad < 1 {
			pad = 1
		}
		tb = tb + fmt.Sprintf("%*s", pad, counter)
	}

	helpView := m.help.View(m.keys)

	var status string
	if m.statusMsg != "" {
		status = "\n" + dimStyle.Render(m.statusMsg)
	}

	return fmt.Sprintf("%s\n\n%s%s\n\n%s", tb, m.list.View(), status, helpView)
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// refreshCmd fetches all data from services asynchronously.
func (m Model) refreshCmd() tea.Cmd {
	wtSvc := m.wtSvc
	tmuxClient := m.tmuxClient
	cfg := m.cfg

	return func() tea.Msg {
		data := refreshResult{
			agentSessions: make(map[string]agentInfo),
			shellSessions: make(map[string]string),
		}

		// 1. Worktrees
		if wtSvc != nil {
			entries, err := wtSvc.List("")
			if err == nil {
				for _, e := range entries {
					data.worktrees = append(data.worktrees, wtEntry{
						project: e.Project,
						branch:  e.Branch,
						path:    e.Path,
					})
				}
			}
		}

		// 2. Agent sessions (query tmux for status)
		if tmuxClient != nil {
			records, err := tmuxClient.ListSessions()
			if err == nil {
				for _, r := range records {
					switch r.SessionType {
					case semconv.SessionTypeShell:
						data.shellSessions[r.CanonicalName] = r.ID
					case semconv.SessionTypeAgent:
						data.agentSessions[r.CanonicalName] = agentInfo{
							sessionID:  r.ID,
							status:     r.Status,
							annotation: r.Annotation,
						}
					}
				}
			}
		}

		// 3. Project list with clone status
		if cfg != nil {
			for name := range cfg.Projects {
				p := cfg.Projects[name]
				cloned := false
				if rp, err := config.RepoPath(p.Repo); err == nil {
					path := semconv.CloneDir(cfg.Defaults.ProjectsDir, rp)
					if _, err := os.Stat(path); err == nil {
						cloned = true
					}
				}
				data.projects = append(data.projects, projEntry{name: name, cloned: cloned})
			}
		}

		// 4. Clone dirs for main worktree detection
		if cfg != nil {
			data.cloneDirs = make(map[string]string)
			for name, p := range cfg.Projects {
				if rp, err := config.RepoPath(p.Repo); err == nil {
					data.cloneDirs[name] = semconv.CloneDir(cfg.Defaults.ProjectsDir, rp)
				}
			}
		}

		items := buildItems(data)
		result := make([]Item, len(items))
		for i, li := range items {
			result[i] = li.(Item)
		}
		return itemsMsg(result)
	}
}

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

// selectedItem returns the currently selected Item, or nil.
func (m Model) selectedItem() *Item {
	sel, ok := m.list.SelectedItem().(Item)
	if !ok {
		return nil
	}
	return &sel
}
