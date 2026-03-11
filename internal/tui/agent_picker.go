package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/project"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/worktree"
)

// agentPickerPending holds the context needed to start a session after agent selection.
type agentPickerPending struct {
	project string
	branch  string
	path    string
	sesSvc  *session.Service
	wtSvc   *worktree.Service
	projSvc *project.Service
}

// agentPickerModel shows a compact list of named agents.
type agentPickerModel struct {
	names   []string
	cursor  int
	cfg     *config.Config
	pending *agentPickerPending
}

func newAgentPicker(cfg *config.Config, defaultAgent string, pending *agentPickerPending) *agentPickerModel {
	names := cfg.AgentNames()
	cursor := 0
	for i, n := range names {
		if n == defaultAgent {
			cursor = i
			break
		}
	}
	return &agentPickerModel{
		names:   names,
		cursor:  cursor,
		cfg:     cfg,
		pending: pending,
	}
}

func (p *agentPickerModel) selected() string {
	if len(p.names) == 0 {
		return ""
	}
	return p.names[p.cursor]
}

func (p *agentPickerModel) Update(msg tea.Msg) (*agentPickerModel, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	switch kp.String() {
	case "j", "down":
		if p.cursor < len(p.names)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "enter":
		return p, p.submit()
	}
	return p, nil
}

func (p *agentPickerModel) submit() tea.Cmd {
	name := p.selected()
	agent, err := p.cfg.AgentByName(name)
	if err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}

	pending := p.pending
	agentCmd := agent.Command()

	return func() tea.Msg {
		path := pending.path

		// If no path, need to clone + create worktree.
		if path == "" {
			if pending.projSvc != nil {
				_ = pending.projSvc.Clone(pending.project)
			}
			if pending.wtSvc != nil {
				result, err := pending.wtSvc.New(pending.project, pending.branch)
				if err != nil {
					return errMsg{err: err}
				}
				path = result.Path
			}
		}

		sessionID, err := pending.sesSvc.Start(session.StartRequest{
			Project: pending.project,
			Branch:  pending.branch,
			Path:    path,
			Cmd:     agentCmd,
			Env:     agent.Env,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: sessionID}
	}
}

func (p *agentPickerModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Select Agent"))
	sb.WriteString("\n────\n")
	for i, name := range p.names {
		cursor := "  "
		if i == p.cursor {
			cursor = "> "
		}
		style := lipgloss.NewStyle()
		if i == p.cursor {
			style = style.Bold(true)
		} else {
			style = style.Faint(true)
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(name)))
	}
	sb.WriteString("────\nEnter: select  |  Esc: cancel  |  j/k: navigate")
	return sb.String()
}
