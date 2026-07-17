package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
)

type formModel struct {
	form *huh.Form

	// Bound values
	branch string
	attach bool
	agent  string

	// Context (read-only)
	project    string
	baseBranch string
	tracksRef  string

	// herd is the domain the form creates the worktree through.
	herd *herd.Herd
}

type formContext struct {
	project    string
	baseBranch string
	tracksRef  string
	branch     string // optional pre-fill for the branch input
}

func newFormModel(ctx formContext, cfg *config.Config, hrd *herd.Herd) *formModel {
	m := &formModel{
		project:    ctx.project,
		baseBranch: ctx.baseBranch,
		attach:     true,
		herd:       hrd,
	}

	m.tracksRef = ctx.tracksRef
	if ctx.branch != "" {
		m.branch = ctx.branch
	}

	agents := cfg.AgentNames()

	group1 := huh.NewGroup(
		huh.NewNote().
			Title("New Worktree").
			Description(worktreeFormDescription(ctx)),
		huh.NewInput().
			Title("Branch name").
			Placeholder("feature-name").
			Value(&m.branch).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("branch name required")
				}
				return nil
			}),
		huh.NewConfirm().
			Title("Attach coding session?").
			Value(&m.attach),
	)

	groups := []*huh.Group{group1}

	if len(agents) > 1 {
		var agentOpts []huh.Option[string]
		for _, name := range agents {
			agentOpts = append(agentOpts, huh.NewOption(name, name))
		}
		if len(agents) > 0 {
			m.agent = agents[0]
		}

		group2 := huh.NewGroup(
			huh.NewSelect[string]().
				Title("Agent").
				Options(agentOpts...).
				Value(&m.agent),
		).WithHideFunc(func() bool {
			return !m.attach
		})
		groups = append(groups, group2)
	} else if len(agents) == 1 {
		m.agent = agents[0]
	}

	m.form = huh.NewForm(groups...)
	return m
}

func (f *formModel) Init() tea.Cmd {
	return f.form.Init()
}

func (f *formModel) Update(msg tea.Msg) (*formModel, tea.Cmd) {
	form, cmd := f.form.Update(msg)
	if ff, ok := form.(*huh.Form); ok {
		f.form = ff
	}
	return f, cmd
}

func (f *formModel) View() string {
	return f.form.View()
}

func (f *formModel) completed() bool {
	return f.form.State == huh.StateCompleted
}

func (f *formModel) submit() tea.Cmd {
	branch := strings.TrimSpace(f.branch)
	project := f.project
	baseBranch := f.baseBranch
	tracksRef := f.tracksRef
	attach := f.attach
	agent := f.agent
	hrd := f.herd

	return func() tea.Msg {
		// Provision here (not only on the attach path): a non-attach create
		// used to skip file copy + templates entirely. startSessionAfterCreate
		// no longer provisions, so this runs the templates exactly once.
		ws, err := hrd.EnsureWorkspace(hrd.Ref(project, branch), herd.EnsureOpts{
			AutoClone:  true,
			Provision:  true,
			StartPoint: baseBranch,
			Track:      tracksRef,
		})
		if err != nil {
			return errMsg{err: err}
		}

		return worktreeCreatedMsg{
			ref:    ws.Ref,
			path:   ws.Path,
			attach: attach,
			agent:  agent,
		}
	}
}

func worktreeFormDescription(ctx formContext) string {
	if ctx.tracksRef != "" {
		return fmt.Sprintf("Project: %s\nTracks: %s", ctx.project, ctx.tracksRef)
	}
	return fmt.Sprintf("Project: %s\nBase: %s", ctx.project, ctx.baseBranch)
}

// showForm transitions the model to the form screen.
func (m Model) showForm() (tea.Model, tea.Cmd) {
	sel := m.selectedItem()
	if sel == nil {
		return m, nil
	}

	var ctx formContext
	switch sel.Group {
	case groupProject:
		ctx.project = sel.Project
		if p, ok := m.cfg.Projects[sel.Project]; ok && p.DefaultBranch != "" {
			ctx.baseBranch = p.DefaultBranch
		} else {
			ctx.baseBranch = "main"
		}
	case groupWorktree, groupAgent:
		ctx.project = sel.Project
		ctx.baseBranch = sel.Branch
	}

	m.form = newFormModel(ctx, m.cfg, m.herd)
	m.screen = screenForm
	return m, m.form.Init()
}

// updateForm handles messages while on the form screen.
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "esc" {
			m.screen = screenList
			m.form = nil
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)

	if m.form.completed() {
		// Reassign m (do not shadow it with :=) so the linter stays quiet.
		var submitCmd tea.Cmd
		m, submitCmd = m.enterBusy("Creating worktree…", m.form.submit())
		return m, submitCmd
	}

	return m, cmd
}
