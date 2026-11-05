package tui

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
)

// ANSI 256 colors (mosh-safe, no true color).
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	waitingTag    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	runningTag    = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	shellTag      = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	clonedTag     = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("34"))
	notClonedTag  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("240"))
)

type itemDelegate struct{}

func newDelegate() itemDelegate { return itemDelegate{} }

func (d itemDelegate) Height() int                             { return 2 }
func (d itemDelegate) Spacing() int                            { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render writes one list row. The list.ItemDelegate interface gives it no way
// to report a write error, and bubbles renders rows into a strings.Builder,
// whose writes cannot fail — so the result is discarded on purpose.
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(Item)
	if !ok {
		return
	}
	_, _ = io.WriteString(w, renderRow(item, index == m.Index()))
}

// renderRow formats an item as two lines: its title, and its status tags.
func renderRow(item Item, selected bool) string {
	title := rowTitle(item)
	tags := rowTags(item)
	if selected {
		return cursorStyle.Render("▸ ") + selectedStyle.Render(title) + "\n" + tags
	}
	return "  " + title + "\n" + tags
}

// rowTitle is "project / branch", plus the head-state hint when HEAD diverged.
// The label is formatted by the same herd.FormatBranchLabel the CLI uses, so
// both surfaces render a diverged or detached worktree identically.
func rowTitle(item Item) string {
	if label := herd.FormatBranchLabel(item.Branch, item.HeadHint); label != "" {
		return item.Project + " / " + label
	}
	return item.Project
}

// rowTags renders the item's status tags, indented under the title, or ""
// when it has none.
func rowTags(item Item) string {
	var tags []string
	if tag := agentTag(item); tag != "" {
		tags = append(tags, tag)
	}
	if item.HasShell {
		tags = append(tags, shellTag.Render("shell"))
	}
	if item.Group == groupProject {
		tags = append(tags, clonedStateTag(item.Cloned))
	}
	if item.HasAgent && item.Annotation != "" {
		tags = append(tags, dimStyle.Render(truncateAnnotation(item.Annotation)))
	}
	if len(tags) == 0 {
		return ""
	}
	return "    " + strings.Join(tags, "  ")
}

// agentTag shows the agent's state, or "" when the item has no agent.
func agentTag(item Item) string {
	switch {
	case !item.HasAgent:
		return ""
	case item.AgentStatus == semconv.StatusWaiting:
		return waitingTag.Render("WAITING FOR INPUT")
	case item.AgentStatus == semconv.StatusRunning:
		return runningTag.Render("running")
	default:
		return runningTag.Render("agent")
	}
}

func clonedStateTag(cloned bool) string {
	if cloned {
		return clonedTag.Render("cloned")
	}
	return notClonedTag.Render("not cloned")
}

// truncateAnnotation keeps an annotation within 60 bytes.
func truncateAnnotation(ann string) string {
	if len(ann) > 60 {
		return ann[:57] + "..."
	}
	return ann
}
