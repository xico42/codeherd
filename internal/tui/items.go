package tui

import (
	"sort"

	"charm.land/bubbles/v2/list"

	"github.com/xico42/codeherd/internal/herd"
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
	// Ref is identity, carried straight from herd.List. Every action feeds
	// this back into herd — never a rebuilt (project, display-branch) pair,
	// which is what orphaned an agent against a deleted worktree.
	Ref herd.Ref

	Project        string
	Branch         string // display branch (herd.Workspace.DisplayBranch) — rendering only
	Path           string
	Group          int
	HasAgent       bool
	AgentStatus    string // "running", "waiting", ""
	AgentSessionID string // tmux session_id — stable identifier for attach
	Annotation     string
	HasShell       bool
	ShellSessionID string // tmux session_id — stable identifier for attach
	Cloned         bool
	IsMain         bool   // true for the main worktree (clone dir)
	HeadHint       string // "detached" / "on <branch>" when HEAD diverged from identity, else ""
}

func (i Item) FilterValue() string {
	if i.Branch != "" {
		return i.Project + " / " + i.Branch
	}
	return i.Project
}

// buildItems turns the workspaces herd.List returned into sorted list items,
// then appends a row for every configured project that has no worktree yet.
//
// The identity derivation the TUI used to do here — WorktreeIdentityBranch,
// SessionName, the divergence switch, the display fallbacks — is gone: herd
// computed it once, and each Workspace already carries its identity Ref and
// display branch.
func buildItems(hrd *herd.Herd, spaces []herd.Workspace) []list.Item {
	projectHasWorktree := make(map[string]bool)

	var items []Item
	for _, ws := range spaces {
		projectHasWorktree[ws.Ref.Project] = true

		item := Item{
			Ref:      ws.Ref,
			Project:  ws.Ref.Project,
			Branch:   ws.DisplayBranch,
			Path:     ws.Path,
			IsMain:   ws.IsMain,
			HeadHint: ws.HeadHint,
			HasShell: ws.Shell != nil,
		}
		if ws.Shell != nil {
			item.ShellSessionID = ws.Shell.ID
		}
		if ws.Agent != nil {
			item.Group = groupAgent
			item.HasAgent = true
			item.AgentStatus = string(ws.Agent.Status)
			item.AgentSessionID = ws.Agent.ID
			item.Annotation = ws.Agent.Annotation
		} else {
			item.Group = groupWorktree
		}

		items = append(items, item)
	}

	// Project rows: every configured project without a worktree. herd.List
	// skips uncloned projects, so ask the herd directly for the full set.
	if hrd != nil {
		for _, p := range hrd.Projects() {
			if projectHasWorktree[p.Name] {
				continue
			}
			cloned := false
			if got, err := hrd.Project(p.Name); err == nil {
				cloned = got.Cloned
			}
			items = append(items, Item{
				Project: p.Name,
				Group:   groupProject,
				Cloned:  cloned,
			})
		}
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
