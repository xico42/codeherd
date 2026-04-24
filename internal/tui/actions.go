package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/filecopy"
	"github.com/xico42/codeherd/internal/herdtemplate"
	"github.com/xico42/codeherd/internal/hooks"
	projectpkg "github.com/xico42/codeherd/internal/project"
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

	cfg := m.cfg
	project := sel.Project
	branch := sel.Branch
	profile := m.activeProfile()

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
		projCfg := cfg.Projects[project]
		tmuxClient := m.tmuxClient

		pending := &agentPickerPending{
			project:    project,
			branch:     branch,
			path:       path,
			projCfg:    projCfg,
			cfg:        cfg,
			tmuxClient: tmuxClient,
			profile:    profile,
		}

		if len(agents) == 1 {
			// Single agent — skip picker.
			agent, _ := cfg.AgentByName(agents[0])
			agentCmd := agent.Command()
			return m, func() tea.Msg {
				h := hooks.New(projCfg.Hooks)
				sesSvc := session.NewService(tmuxClient, h)
				sessionID, err := sesSvc.Start(session.StartRequest{
					Project:  project,
					Branch:   branch,
					Path:     path,
					CloneDir: projectCloneDir(cfg, projCfg),
					Cmd:      agentCmd,
					Env:      agent.Env,
					Profile:  profile,
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

		projCfg := cfg.Projects[project]
		tmuxClient := m.tmuxClient

		if len(agents) == 1 {
			agent, _ := cfg.AgentByName(agents[0])
			agentCmd := agent.Command()
			return m, func() tea.Msg {
				h := hooks.New(projCfg.Hooks)
				projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
				_ = projSvc.Clone(project)

				wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
				result, err := wtSvc.New(project, defaultBranch)
				if err != nil {
					return errMsg{err: err}
				}

				if err := runFileCopyAndTemplate(cfg, projCfg, h, project, defaultBranch, result.Path); err != nil {
					return errMsg{err: err}
				}

				sesSvc := session.NewService(tmuxClient, h)
				sessionID, err := sesSvc.Start(session.StartRequest{
					Project:  project,
					Branch:   defaultBranch,
					Path:     result.Path,
					CloneDir: projectCloneDir(cfg, projCfg),
					Cmd:      agentCmd,
					Env:      agent.Env,
					Profile:  profile,
				})
				if err != nil {
					return errMsg{err: err}
				}
				return attachMsg{session: sessionID}
			}
		}

		// Multiple agents — show picker.
		m.agentPicker = newAgentPicker(cfg, cfg.Defaults.Agent, &agentPickerPending{
			project:    project,
			branch:     defaultBranch,
			projCfg:    projCfg,
			cfg:        cfg,
			tmuxClient: tmuxClient,
			profile:    profile,
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
	cfg := m.cfg
	project := sel.Project
	branch := sel.Branch
	path := sel.Path
	shellSessionID := sel.ShellSessionID
	profile := m.activeProfile()

	return func() tea.Msg {
		// For group 3 (project-only), clone + create worktree first.
		if branch == "" {
			defaultBranch := "main"
			if p, ok := cfg.Projects[project]; ok && p.DefaultBranch != "" {
				defaultBranch = p.DefaultBranch
			}
			branch = defaultBranch

			projCfg := cfg.Projects[project]
			h := hooks.New(projCfg.Hooks)

			projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
			_ = projSvc.Clone(project)

			wtSvc := worktree.NewService(cfg, worktree.NewRealWorktreeRunner(), tmuxClient, h)
			result, err := wtSvc.New(project, branch)
			if err != nil {
				return errMsg{err: err}
			}
			path = result.Path

			if err := runFileCopyAndTemplate(cfg, projCfg, h, project, branch, path); err != nil {
				return errMsg{err: err}
			}
		}

		// If the shell session already exists, attach by stable session ID.
		if shellSessionID != "" {
			return attachMsg{session: shellSessionID}
		}

		shellCmd := os.Getenv("SHELL")
		if shellCmd == "" {
			shellCmd = "/bin/sh"
		}

		// Construct a project-bound hook. The existing group-3 branch above
		// may have already done this (shadowing `h`), but the worktree-item
		// path does not — so always resolve from cfg here.
		projCfg := cfg.Projects[project]
		h := hooks.New(projCfg.Hooks)

		sesSvc := session.NewService(tmuxClient, h)
		sessionID, err := sesSvc.Start(session.StartRequest{
			Project:  project,
			Branch:   branch,
			Path:     path,
			CloneDir: projectCloneDir(cfg, projCfg),
			Type:     semconv.SessionTypeShell,
			Cmd:      shellCmd,
			Profile:  profile,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: sessionID}
	}
}

// ── Clone ───────────────────────────────────────────────────────────────────

func (m Model) cloneAction() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil || sel.Group != groupProject || sel.Cloned {
		return nil
	}

	cfg := m.cfg
	project := sel.Project
	projCfg := cfg.Projects[project]

	return func() tea.Msg {
		h := hooks.New(projCfg.Hooks)
		projSvc := projectpkg.NewService(cfg, projectpkg.NewRealGitRunner(), h)
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
	shellID := target.ShellSessionID

	return m, func() tea.Msg {
		if target.AgentSessionID != "" {
			_ = sesSvc.Stop(project, branch, semconv.SessionTypeAgent)
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

	tmuxClient := m.tmuxClient
	agentID := target.AgentSessionID

	return m, func() tea.Msg {
		if agentID != "" {
			_ = tmuxClient.KillSession(agentID)
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
	tmuxClient := m.tmuxClient
	projCfg := cfg.Projects[msg.project]
	profile := m.activeProfile()

	return func() tea.Msg {
		agent, err := cfg.AgentByName(msg.agent)
		if err != nil {
			return errMsg{err: err}
		}

		h := hooks.New(projCfg.Hooks)

		if err := runFileCopyAndTemplate(cfg, projCfg, h, msg.project, msg.branch, msg.path); err != nil {
			return errMsg{err: err}
		}

		agentCmd := agent.Command()
		sesSvc := session.NewService(tmuxClient, h)
		sessionID, err := sesSvc.Start(session.StartRequest{
			Project:  msg.project,
			Branch:   msg.branch,
			Path:     msg.path,
			CloneDir: projectCloneDir(cfg, projCfg),
			Cmd:      agentCmd,
			Env:      agent.Env,
			Profile:  profile,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return attachMsg{session: sessionID}
	}
}

// projectCloneDir returns the main git clone path for a project, or "" when
// the project's repo URL can't be parsed. Matches the cmd/ helpers so the
// CODEHERD_CLONE_DIR env var is either a valid path or absent.
func projectCloneDir(cfg *config.Config, projCfg config.ProjectConfig) string {
	repoPath, err := config.RepoPath(projCfg.Repo)
	if err != nil {
		return ""
	}
	return filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
}

// runFileCopyAndTemplate runs file copy and herd template processing for a worktree.
func runFileCopyAndTemplate(cfg *config.Config, projCfg config.ProjectConfig, h hooks.Hook, proj, branch, wtPath string) error {
	if len(projCfg.Files) > 0 {
		repoPath, _ := config.RepoPath(projCfg.Repo)
		cloneDir := filepath.Join(cfg.Defaults.ProjectsDir, repoPath)
		copySvc := filecopy.New(h)
		attrs := map[string]string{
			semconv.HookAttrProject:      proj,
			semconv.HookAttrBranch:       branch,
			semconv.HookAttrWorktreePath: wtPath,
		}
		if err := copySvc.Copy(projCfg.Files, cloneDir, wtPath, attrs); err != nil {
			return fmt.Errorf("file copy: %w", err)
		}
	}

	tmplSvc := herdtemplate.New(h)
	tmplAttrs := map[string]string{
		semconv.HookAttrProject:      proj,
		semconv.HookAttrBranch:       branch,
		semconv.HookAttrWorktreePath: wtPath,
	}
	if _, err := tmplSvc.Process(herdtemplate.ProcessContext{
		Project:      proj,
		Branch:       branch,
		WorktreePath: wtPath,
		SessionName:  semconv.SessionName("", proj, branch),
	}, tmplAttrs); err != nil {
		return fmt.Errorf("template processing: %w", err)
	}

	return nil
}
