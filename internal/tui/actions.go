package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/worktree"
)

// ── Attach (agent) ──────────────────────────────────────────────────────────

func (m Model) attachAction() (tea.Model, tea.Cmd) {
	sel := m.selectedItem()
	if sel == nil {
		return m, nil
	}

	sesSvc := m.sesSvc
	wtSvc := m.wtSvc
	projSvc := m.projSvc
	cfg := m.cfg
	project := sel.Project
	branch := sel.Branch

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

		path := sel.Path
		pending := &agentPickerPending{
			project: project,
			branch:  branch,
			path:    path,
			sesSvc:  sesSvc,
		}

		if len(agents) == 1 {
			// Single agent — skip picker.
			agent, _ := cfg.AgentByName(agents[0])
			agentCmd := agent.Command()
			return m, func() tea.Msg {
				sessionID, err := sesSvc.Start(session.StartRequest{
					Project: project,
					Branch:  branch,
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

		// Multiple agents — show picker.
		m.agentPicker = newAgentPicker(cfg, cfg.Defaults.Agent, pending)
		m.screen = screenAgentPicker
		return m, nil

	case groupProject:
		agents := cfg.AgentNames()
		if len(agents) == 0 {
			m.statusMsg = "no agents configured — add [agents.<name>] to config"
			return m, nil
		}

		defaultBranch := "main"
		if p, ok := cfg.Projects[project]; ok && p.DefaultBranch != "" {
			defaultBranch = p.DefaultBranch
		}

		if len(agents) == 1 {
			agent, _ := cfg.AgentByName(agents[0])
			agentCmd := agent.Command()
			return m, func() tea.Msg {
				if projSvc != nil {
					_ = projSvc.Clone(project)
				}
				result, err := wtSvc.New(project, defaultBranch)
				if err != nil {
					return errMsg{err: err}
				}
				sessionID, err := sesSvc.Start(session.StartRequest{
					Project: project,
					Branch:  defaultBranch,
					Path:    result.Path,
					Cmd:     agentCmd,
					Env:     agent.Env,
				})
				if err != nil {
					return errMsg{err: err}
				}
				return attachMsg{session: sessionID}
			}
		}

		// Multiple agents — show picker.
		m.agentPicker = newAgentPicker(cfg, cfg.Defaults.Agent, &agentPickerPending{
			project: project,
			branch:  defaultBranch,
			sesSvc:  sesSvc,
			wtSvc:   wtSvc,
			projSvc: projSvc,
		})
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

	tmuxClient := m.tmuxClient
	wtSvc := m.wtSvc
	projSvc := m.projSvc
	cfg := m.cfg
	project := sel.Project
	branch := sel.Branch
	path := sel.Path
	shellSessionID := sel.ShellSessionID

	return func() tea.Msg {
		// For group 3 (project-only), clone + create worktree first.
		if branch == "" {
			defaultBranch := "main"
			if p, ok := cfg.Projects[project]; ok && p.DefaultBranch != "" {
				defaultBranch = p.DefaultBranch
			}
			branch = defaultBranch

			if projSvc != nil {
				_ = projSvc.Clone(project)
			}
			result, err := wtSvc.New(project, branch)
			if err != nil {
				return errMsg{err: err}
			}
			path = result.Path
		}

		// If the shell session already exists, attach by stable session ID.
		if shellSessionID != "" {
			return attachMsg{session: shellSessionID}
		}

		shellName := semconv.ShellSessionName(project, branch)
		sessionName := semconv.SessionName(project, branch)

		if err := tmuxClient.NewSession(shellName, path); err != nil {
			return errMsg{err: err}
		}
		_ = tmuxClient.SetOption(shellName, semconv.TmuxOptionCanonicalName, sessionName)
		_ = tmuxClient.SetOption(shellName, semconv.TmuxOptionSessionType, semconv.SessionTypeShell)

		return attachMsg{session: shellName}
	}
}

// ── Clone ───────────────────────────────────────────────────────────────────

func (m Model) cloneAction() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil || sel.Group != groupProject || sel.Cloned {
		return nil
	}

	projSvc := m.projSvc
	project := sel.Project

	return func() tea.Msg {
		if err := projSvc.Clone(project); err != nil {
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
	target := m.confirm.target
	m.confirm = nil
	m.screen = screenList

	sesSvc := m.sesSvc
	wtSvc := m.wtSvc
	tmuxClient := m.tmuxClient
	project := target.Project
	branch := target.Branch
	canonicalName := semconv.SessionName(project, branch)
	shellID := target.ShellSessionID

	return m, func() tea.Msg {
		if target.AgentSessionID != "" {
			_ = sesSvc.Stop(canonicalName)
		}

		if shellID != "" {
			_ = tmuxClient.KillSession(shellID)
		}

		err := wtSvc.Delete(worktree.DeleteRequest{
			Project: project,
			Branch:  branch,
			Force:   true,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return m.refreshCmd()()
	}
}

func (m Model) confirmDeleteAgent() (tea.Model, tea.Cmd) {
	target := m.confirm.target
	m.confirm = nil
	m.screen = screenList

	sesSvc := m.sesSvc
	canonicalName := semconv.SessionName(target.Project, target.Branch)

	return m, func() tea.Msg {
		if target.AgentSessionID != "" {
			_ = sesSvc.Stop(canonicalName)
		}
		return m.refreshCmd()()
	}
}

func (m Model) confirmDeleteShell() (tea.Model, tea.Cmd) {
	target := m.confirm.target
	m.confirm = nil
	m.screen = screenList

	tmuxClient := m.tmuxClient
	shellID := target.ShellSessionID

	return m, func() tea.Msg {
		if shellID != "" {
			_ = tmuxClient.KillSession(shellID)
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
func (m Model) startSessionAfterCreate(msg worktreeCreatedMsg) tea.Cmd {
	cfg := m.cfg
	sesSvc := m.sesSvc

	return func() tea.Msg {
		agent, err := cfg.AgentByName(msg.agent)
		if err != nil {
			return errMsg{err: err}
		}
		agentCmd := agent.Command()
		sessionID, err := sesSvc.Start(session.StartRequest{
			Project: msg.project,
			Branch:  msg.branch,
			Path:    msg.path,
			Cmd:     agentCmd,
			Env:     agent.Env,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: sessionID}
	}
}
