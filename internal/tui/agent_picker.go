package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
)

// agentPickerPending holds the context needed to start a session after agent
// selection: the identity Ref to operate on and the herd to operate through.
type agentPickerPending struct {
	ref  herd.Ref
	herd *herd.Herd
}

// agentPickerModel shows a compact list of named agents.
type agentPickerModel struct {
	names   []string
	cursor  int
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
	if name == "" {
		return func() tea.Msg { return errMsg{err: fmt.Errorf("no agent selected")} }
	}
	pending := p.pending

	return func() tea.Msg {
		if _, err := pending.herd.EnsureWorkspace(pending.ref, herd.EnsureOpts{AutoClone: true, Provision: true}); err != nil && !errors.Is(err, herd.ErrWorktreeExists) {
			return errMsg{err: err}
		}
		handle, err := pending.herd.Launch(pending.ref, herd.LaunchOpts{Agent: name})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: handle.ID}
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
