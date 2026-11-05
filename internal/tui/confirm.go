package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type deleteAction int

const (
	deleteAll         deleteAction = iota // worktree + all sessions
	deleteAllSessions                     // all sessions, keep the worktree
	deleteAgent                           // agent session only
	deleteShell                           // shell session only
	deleteCancel                          // cancel
)

type choice struct {
	label  string
	action deleteAction
}

type confirmModel struct {
	target  Item
	choices []choice
	cursor  int
}

func newConfirmModel(target Item) *confirmModel {
	var choices []choice

	hasAgent := target.HasAgent
	hasShell := target.HasShell

	// "Delete everything" label varies by what's active.
	switch {
	case target.IsMain:
		// The main worktree is the clone dir itself — never offer to remove it.
		// Only session kills are available here; startDelete guarantees at least
		// one session is active before this menu opens.
		if hasAgent && hasShell {
			choices = append(choices, choice{"Delete all sessions", deleteAllSessions})
		}
		if hasAgent {
			choices = append(choices, choice{"Delete agent session only", deleteAgent})
		}
		if hasShell {
			choices = append(choices, choice{"Delete shell session only", deleteShell})
		}
	case hasAgent && hasShell:
		choices = append(choices, choice{"Delete everything (worktree + all sessions)", deleteAll})
		choices = append(choices, choice{"Delete agent session only", deleteAgent})
		choices = append(choices, choice{"Delete shell session only", deleteShell})
	case hasAgent:
		choices = append(choices, choice{"Delete everything (worktree + agent session)", deleteAll})
		choices = append(choices, choice{"Delete agent session only", deleteAgent})
	case hasShell:
		choices = append(choices, choice{"Delete everything (worktree + shell session)", deleteAll})
		choices = append(choices, choice{"Delete shell session only", deleteShell})
	default:
		choices = append(choices, choice{"Delete worktree", deleteAll})
	}
	choices = append(choices, choice{"Cancel", deleteCancel})

	return &confirmModel{target: target, choices: choices}
}

func (c *confirmModel) selected() deleteAction {
	return c.choices[c.cursor].action
}

func (c *confirmModel) Update(msg tea.Msg) (*confirmModel, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	switch kp.String() {
	case "j", "down":
		if c.cursor < len(c.choices)-1 {
			c.cursor++
		}
	case "k", "up":
		if c.cursor > 0 {
			c.cursor--
		}
	}
	return c, nil
}

// sessionsWarning lists the target's active sessions, or returns "" when it
// has none.
func sessionsWarning(target Item, warnStyle lipgloss.Style) string {
	if !target.HasAgent && !target.HasShell {
		return ""
	}
	var b strings.Builder
	b.WriteString(warnStyle.Render("  ⚠ Active sessions detected:"))
	b.WriteString("\n")
	if target.HasAgent {
		fmt.Fprintf(&b, "    • Agent session (%s)\n", cmp.Or(target.AgentStatus, "active"))
	}
	if target.HasShell {
		b.WriteString("    • Shell session (running)\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (c *confirmModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	selectedStyle := lipgloss.NewStyle().Bold(true)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n────\n", titleStyle.Render(fmt.Sprintf("Delete — %s/%s", c.target.Project, c.target.Branch)))

	b.WriteString(sessionsWarning(c.target, warnStyle))

	// Choices
	for i, ch := range c.choices {
		if i == c.cursor {
			fmt.Fprintf(&b, "  > %s\n", selectedStyle.Render(ch.label))
		} else {
			fmt.Fprintf(&b, "    %s\n", ch.label)
		}
	}

	b.WriteString("────\nEnter: select  |  Esc / q: cancel")
	return b.String()
}
