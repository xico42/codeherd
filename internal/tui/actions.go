package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/herd"
)

// defaultBranch returns a project's configured default branch, or "main".
func (m Model) defaultBranch(project string) string {
	if p, ok := m.cfg.Projects[project]; ok && p.DefaultBranch != "" {
		return p.DefaultBranch
	}
	return "main"
}

// ── Attach (agent) ──────────────────────────────────────────────────────────

func (m Model) attachAction() (tea.Model, tea.Cmd) {
	sel := m.selectedItem()
	if sel == nil {
		return m, nil
	}

	cfg := m.cfg
	hrd := m.herd

	switch sel.Group {
	case groupAgent:
		// Already has agent — attach using stable session ID (unaffected by renames).
		sessionID := sel.AgentSessionID
		return m, func() tea.Msg { return attachMsg{session: sessionID} }

	case groupWorktree:
		agents := cfg.AgentNames()
		if len(agents) == 0 {
			m.statusMsg = "no agents configured — add [agents.<name>] to config"
			return m, nil
		}

		ref := sel.Ref // identity from herd.List — never a rebuilt display branch

		if len(agents) == 1 {
			// Single agent — skip picker. The worktree already exists, so no
			// EnsureWorkspace is needed; go straight to the session.
			agentName := agents[0]
			return m, func() tea.Msg {
				handle, err := hrd.Launch(ref, herd.LaunchOpts{Agent: agentName})
				if err != nil {
					return errMsg{err: err}
				}
				return attachMsg{session: handle.ID}
			}
		}

		// Multiple agents — show picker.
		m.agentPicker = newAgentPicker(cfg, cfg.Defaults.Agent, &agentPickerPending{ref: ref, herd: hrd})
		m.screen = screenAgentPicker
		return m, nil

	case groupProject:
		agents := cfg.AgentNames()
		if len(agents) == 0 {
			m.statusMsg = "no agents configured — add [agents.<name>] to config"
			return m, nil
		}

		// No workspace yet — mint a Ref from the default branch.
		ref := hrd.Ref(sel.Project, m.defaultBranch(sel.Project))

		if len(agents) == 1 {
			agentName := agents[0]
			return m, func() tea.Msg {
				if _, err := hrd.EnsureWorkspace(ref, herd.EnsureOpts{AutoClone: true, Provision: true}); err != nil && !errors.Is(err, herd.ErrWorktreeExists) {
					return errMsg{err: err}
				}
				handle, err := hrd.Launch(ref, herd.LaunchOpts{Agent: agentName})
				if err != nil {
					return errMsg{err: err}
				}
				return attachMsg{session: handle.ID}
			}
		}

		// Multiple agents — show picker.
		m.agentPicker = newAgentPicker(cfg, cfg.Defaults.Agent, &agentPickerPending{ref: ref, herd: hrd})
		m.screen = screenAgentPicker
		return m, nil
	}
	return m, nil
}

// updateAgentPicker handles key events in the agent picker sub-screen.
func (m Model) updateAgentPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok && (kp.String() == "esc" || kp.String() == "q") {
		m.agentPicker = nil
		m.screen = screenList
		return m, nil
	}
	if m.agentPicker == nil {
		m.screen = screenList
		return m, nil
	}
	var cmd tea.Cmd
	m.agentPicker, cmd = m.agentPicker.Update(msg)
	if cmd != nil {
		// Agent selected — transition back to list and run the command.
		m.screen = screenList
		m.agentPicker = nil
	}
	return m, cmd
}

// ── Shell ───────────────────────────────────────────────────────────────────

func (m Model) shellAction() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil {
		return nil
	}

	hrd := m.herd
	shellSessionID := sel.ShellSessionID

	isProject := sel.Group == groupProject
	ref := sel.Ref // identity for existing worktree/agent rows
	if isProject {
		// No workspace yet — mint a Ref from the default branch.
		ref = hrd.Ref(sel.Project, m.defaultBranch(sel.Project))
	}

	return func() tea.Msg {
		// If the shell session already exists, attach by stable session ID.
		if shellSessionID != "" {
			return attachMsg{session: shellSessionID}
		}

		if isProject {
			if _, err := hrd.EnsureWorkspace(ref, herd.EnsureOpts{AutoClone: true, Provision: true}); err != nil && !errors.Is(err, herd.ErrWorktreeExists) {
				return errMsg{err: err}
			}
		}

		handle, err := hrd.Launch(ref, herd.LaunchOpts{Type: herd.SessionTypeShell})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: handle.ID}
	}
}

// ── Clone ───────────────────────────────────────────────────────────────────

func (m Model) cloneAction() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil || sel.Group != groupProject || sel.Cloned {
		return nil
	}

	hrd := m.herd
	project := sel.Project

	return func() tea.Msg {
		if err := hrd.Clone(project); err != nil {
			return errMsg{err: err}
		}
		return cloneDoneMsg{project: project}
	}
}

// ── Delete ──────────────────────────────────────────────────────────────────

func (m Model) startDelete() (tea.Model, tea.Cmd) {
	sel := m.selectedItem()
	if sel == nil {
		return m, nil
	}
	if sel.Group == groupProject {
		m.statusMsg = "cannot delete a project entry — select a worktree"
		return m, nil
	}
	if sel.IsMain {
		m.statusMsg = "cannot delete the main worktree"
		return m, nil
	}
	m.confirm = newConfirmModel(*sel)
	m.screen = screenConfirmDelete
	return m, nil
}

func (m Model) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch kp.String() {
	case "esc", "q":
		return m.confirmDeleteNo()
	case "enter":
		switch m.confirm.selected() {
		case deleteCancel:
			return m.confirmDeleteNo()
		case deleteAll:
			return m.confirmDeleteAll()
		case deleteAgent:
			return m.confirmDeleteAgent()
		case deleteShell:
			return m.confirmDeleteShell()
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	}
}

func (m Model) confirmDeleteAll() (tea.Model, tea.Cmd) {
	ref := m.confirm.target.Ref // identity from herd.List — never a display string
	hrd := m.herd
	m.confirm, m.screen = nil, screenList

	return m, func() tea.Msg {
		if err := hrd.Teardown(ref, herd.TeardownOpts{Force: true}); err != nil {
			return errMsg{err: err}
		}
		return m.refreshCmd()()
	}
}

func (m Model) confirmDeleteAgent() (tea.Model, tea.Cmd) {
	ref := m.confirm.target.Ref
	hrd := m.herd
	m.confirm, m.screen = nil, screenList

	return m, func() tea.Msg {
		if _, err := hrd.StopSessions(ref, herd.StopOpts{Type: herd.SessionTypeAgent}); err != nil {
			return errMsg{err: err}
		}
		return m.refreshCmd()()
	}
}

func (m Model) confirmDeleteShell() (tea.Model, tea.Cmd) {
	ref := m.confirm.target.Ref
	hrd := m.herd
	m.confirm, m.screen = nil, screenList

	return m, func() tea.Msg {
		if _, err := hrd.StopSessions(ref, herd.StopOpts{Type: herd.SessionTypeShell}); err != nil {
			return errMsg{err: err}
		}
		return m.refreshCmd()()
	}
}

func (m Model) confirmDeleteNo() (tea.Model, tea.Cmd) {
	m.confirm = nil
	m.screen = screenList
	return m, nil
}

// startSessionAfterCreate starts an agent session for a newly created worktree.
// Provisioning already happened in form.submit's EnsureWorkspace, so this only
// launches the session — running templates here too would render them twice.
func (m Model) startSessionAfterCreate(msg worktreeCreatedMsg) tea.Cmd {
	hrd := m.herd

	return func() tea.Msg {
		handle, err := hrd.Launch(msg.ref, herd.LaunchOpts{Agent: msg.agent})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: handle.ID}
	}
}
