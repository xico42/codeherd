package tui

import (
	"fmt"
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

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(Item)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Line 1: project / branch (+ head-state hint when HEAD diverged). The label
	// is formatted by the same herd.FormatBranchLabel the CLI uses, so both
	// surfaces render a diverged or detached worktree identically.
	line1 := item.Project
	if label := herd.FormatBranchLabel(item.Branch, item.HeadHint); label != "" {
		line1 += " / " + label
	}

	cursor := "  "
	if isSelected {
		cursor = cursorStyle.Render("▸ ")
		line1 = selectedStyle.Render(line1)
	} else {
		line1 = "  " + line1
	}

	// Line 2: status tags
	var tags []string
	switch {
	case item.HasAgent && item.AgentStatus == semconv.StatusWaiting:
		tags = append(tags, waitingTag.Render("WAITING FOR INPUT"))
	case item.HasAgent && item.AgentStatus == semconv.StatusRunning:
		tags = append(tags, runningTag.Render("running"))
	case item.HasAgent:
		tags = append(tags, runningTag.Render("agent"))
	}

	if item.HasShell {
		tags = append(tags, shellTag.Render("shell"))
	}

	if item.Group == groupProject {
		if item.Cloned {
			tags = append(tags, clonedTag.Render("cloned"))
		} else {
			tags = append(tags, notClonedTag.Render("not cloned"))
		}
	}

	if item.HasAgent && item.Annotation != "" {
		ann := item.Annotation
		if len(ann) > 60 {
			ann = ann[:57] + "..."
		}
		tags = append(tags, dimStyle.Render(ann))
	}

	line2 := "    " + strings.Join(tags, "  ")
	if len(tags) == 0 {
		line2 = ""
	}

	if isSelected {
		fmt.Fprintf(w, "%s%s\n%s", cursor, line1, line2)
	} else {
		fmt.Fprintf(w, "%s\n%s", line1, line2)
	}
}
