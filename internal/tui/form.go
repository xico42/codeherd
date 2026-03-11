package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	projectpkg "github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/tmux"
	"github.com/xico42/codeherd/internal/worktree"
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

	// Dependencies for creating per-project services
	cfg        *config.Config
	tmuxClient *tmux.Client
}

type formContext struct {
	project    string
	baseBranch string
}

func newFormModel(ctx formContext, cfg *config.Config, tmuxClient *tmux.Client) *formModel {
	m := &formModel{
		project:    ctx.project,
		baseBranch: ctx.baseBranch,
		attach:     true,
		cfg:        cfg,
		tmuxClient: tmuxClient,
	}

	agents := cfg.AgentNames()

	group1 := huh.NewGroup(
		huh.NewNote().
			Title("New Worktree").
			Description(fmt.Sprintf("Project: %s\nBase: %s", ctx.project, ctx.baseBranch)),
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
	attach := f.attach
	agent := f.agent
	cfg := f.cfg
	tmuxClient := f.tmuxClient
	projCfg := cfg.Projects[project]

	return func() tea.Msg {
		h := hooks.New(projCfg.Hooks)
		projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
		_ = projSvc.Clone(project)

		wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
		var result worktree.NewResult
		var err error
		if baseBranch != "" {
			result, err = wtSvc.NewFrom(project, branch, baseBranch)
		} else {
			result, err = wtSvc.New(project, branch)
		}
		if err != nil {
			return errMsg{err: err}
		}

		return worktreeCreatedMsg{
			project: project,
			branch:  branch,
			path:    result.Path,
			attach:  attach,
			agent:   agent,
		}
	}
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

	m.form = newFormModel(ctx, m.cfg, m.tmuxClient)
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
		return m, m.form.submit()
	}

	return m, cmd
}
