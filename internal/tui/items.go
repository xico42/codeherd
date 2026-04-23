package tui

import (
	"sort"

	"charm.land/bubbles/v2/list"

	"github.com/xico42/codeherd/internal/semconv"
)

// Item groups determine sort priority.
const (
	groupAgent    = 1 // worktrees with active agent sessions
	groupWorktree = 2 // worktrees without agent sessions
	groupProject  = 3 // projects without worktrees
)

// Item represents a single entry in the TUI list.
type Item struct {
	Project        string
	Branch         string
	Path           string
	Group          int
	HasAgent       bool
	AgentStatus    string // "running", "waiting", ""
	AgentSessionID string // tmux session_id — stable identifier for attach
	Annotation     string
	HasShell       bool
	ShellSessionID string // tmux session_id — stable identifier for attach
	Cloned         bool
	IsMain         bool // true for the main worktree (clone dir)
}

func (i Item) FilterValue() string {
	if i.Branch != "" {
		return i.Project + " / " + i.Branch
	}
	return i.Project
}

// refreshResult holds raw data collected during a refresh cycle.
type refreshResult struct {
	worktrees     []wtEntry
	agentSessions map[string]agentInfo // keyed by session name (project-branch)
	shellSessions map[string]string    // canonical session name → tmux session_id
	projects      []projEntry
	cloneDirs     map[string]string // project name -> clone dir path
}

type wtEntry struct {
	project string
	branch  string
	path    string
}

type agentInfo struct {
	sessionID  string // tmux session_id — stable identifier for attach
	status     string
	annotation string
}

type projEntry struct {
	name   string
	cloned bool
}

// buildItems transforms refresh data into a sorted slice of list items.
func buildItems(data refreshResult) []list.Item {
	// Track which projects have worktrees.
	projectHasWorktree := make(map[string]bool)

	var items []Item
	for _, wt := range data.worktrees {
		projectHasWorktree[wt.project] = true

		sessionName := semconv.SessionName("", wt.project, wt.branch)

		shellID := data.shellSessions[sessionName]
		item := Item{
			Project:        wt.project,
			Branch:         wt.branch,
			Path:           wt.path,
			HasShell:       shellID != "",
			ShellSessionID: shellID,
			IsMain:         data.cloneDirs[wt.project] == wt.path,
		}

		if agent, ok := data.agentSessions[sessionName]; ok {
			item.Group = groupAgent
			item.HasAgent = true
			item.AgentStatus = agent.status
			item.AgentSessionID = agent.sessionID
			item.Annotation = agent.annotation
		} else {
			item.Group = groupWorktree
		}

		items = append(items, item)
	}

	for _, p := range data.projects {
		if projectHasWorktree[p.name] {
			continue
		}
		items = append(items, Item{
			Project: p.name,
			Group:   groupProject,
			Cloned:  p.cloned,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		// Within agent group, waiting sorts before running
		if items[i].Group == groupAgent {
			iWaiting := items[i].AgentStatus == semconv.StatusWaiting
			jWaiting := items[j].AgentStatus == semconv.StatusWaiting
			if iWaiting != jWaiting {
				return iWaiting
			}
		}
		if items[i].Project != items[j].Project {
			return items[i].Project < items[j].Project
		}
		return items[i].Branch < items[j].Branch
	})

	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}
