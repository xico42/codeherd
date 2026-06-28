package tui

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"

	"github.com/xico42/codeherd/internal/semconv"
)

func TestDelegate_Height(t *testing.T) {
	d := newDelegate()
	if d.Height() != 2 {
		t.Errorf("Height() = %d, want 2", d.Height())
	}
}

func TestDelegate_Spacing(t *testing.T) {
	d := newDelegate()
	if d.Spacing() != 1 {
		t.Errorf("Spacing() = %d, want 1", d.Spacing())
	}
}

func TestDelegate_Render_agentWaiting(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{
		Item{Project: "myapp", Branch: "feature", Group: groupAgent, HasAgent: true, AgentStatus: semconv.StatusWaiting, HasShell: true},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "myapp") || !strings.Contains(out, "feature") {
		t.Errorf("render missing project/branch, got: %q", out)
	}
	if !strings.Contains(out, "WAITING FOR INPUT") {
		t.Errorf("render missing WAITING FOR INPUT tag, got: %q", out)
	}
	if !strings.Contains(out, "shell") {
		t.Errorf("render missing shell tag, got: %q", out)
	}
}

func TestDelegate_Render_projectNotCloned(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{
		Item{Project: "infra", Group: groupProject, Cloned: false},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "infra") {
		t.Errorf("render missing project name, got: %q", out)
	}
	if !strings.Contains(out, "not cloned") {
		t.Errorf("render missing 'not cloned' tag, got: %q", out)
	}
}

type nonItem struct{}

func (nonItem) FilterValue() string { return "" }

func TestDelegate_Render_nonItemListItem(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{}, d, 80, 10)

	var buf bytes.Buffer
	// Pass a non-Item value — should return without writing.
	d.Render(&buf, m, 0, nonItem{})
	if buf.Len() != 0 {
		t.Errorf("render should write nothing for non-Item, wrote %q", buf.String())
	}
}

func TestDelegate_Render_agentRunning(t *testing.T) {
	d := newDelegate()
	items := []list.Item{
		Item{Project: "myapp", Branch: "main", Group: groupAgent, HasAgent: true, AgentStatus: semconv.StatusRunning},
		Item{Project: "other", Branch: "feat", Group: groupWorktree},
	}
	m := list.New(items, d, 80, 10)
	m.Select(1) // select second item so first is NOT selected

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "running") {
		t.Errorf("render missing 'running' tag, got: %q", out)
	}
}

func TestDelegate_Render_projectCloned(t *testing.T) {
	d := newDelegate()
	items := []list.Item{
		Item{Project: "infra", Group: groupProject, Cloned: true},
		Item{Project: "other", Group: groupWorktree},
	}
	m := list.New(items, d, 80, 10)
	m.Select(1) // deselect first item

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "cloned") {
		t.Errorf("render missing 'cloned' tag, got: %q", out)
	}
}

func TestDelegate_Render_agentNoSpecificStatus(t *testing.T) {
	d := newDelegate()
	items := []list.Item{
		Item{Project: "myapp", Branch: "main", Group: groupAgent, HasAgent: true, AgentStatus: ""},
		Item{Project: "other", Branch: "feat", Group: groupWorktree},
	}
	m := list.New(items, d, 80, 10)
	m.Select(1)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "agent") {
		t.Errorf("render missing 'agent' tag, got: %q", out)
	}
}

func TestDelegate_Render_annotationShort(t *testing.T) {
	d := newDelegate()
	ann := "short annotation"
	m := list.New([]list.Item{
		Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, AgentStatus: semconv.StatusWaiting, Annotation: ann},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, ann) {
		t.Errorf("render missing annotation %q, got: %q", ann, out)
	}
}

func TestDelegate_Render_annotationTruncated(t *testing.T) {
	d := newDelegate()
	// 70-char string — should be truncated to first 57 chars + "..."
	ann := strings.Repeat("x", 70)
	want := strings.Repeat("x", 57) + "..."

	m := list.New([]list.Item{
		Item{Project: "myapp", Branch: "feat", Group: groupAgent, HasAgent: true, AgentStatus: semconv.StatusWaiting, Annotation: ann},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, want) {
		t.Errorf("render missing truncated annotation %q, got: %q", want, out)
	}
	if strings.Contains(out, ann) {
		t.Errorf("render should not contain full annotation (should be truncated), got: %q", out)
	}
}

func TestDelegate_Render_noBranch(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{
		Item{Project: "infra", Group: groupProject, Cloned: true},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	// Should not have " / " for project-only items.
	if strings.Contains(out, " / ") {
		t.Errorf("render should not have ' / ' for project-only items, got: %q", out)
	}
}

func TestDelegate_Render_headHint(t *testing.T) {
	d := newDelegate()
	m := list.New([]list.Item{
		Item{Project: "myapp", Branch: "feature", Group: groupAgent, HasAgent: true,
			AgentStatus: semconv.StatusRunning, HeadHint: "detached"},
	}, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, m.Items()[0])
	out := buf.String()

	if !strings.Contains(out, "feature") {
		t.Errorf("render missing branch, got: %q", out)
	}
	if !strings.Contains(out, "detached") {
		t.Errorf("render missing head hint, got: %q", out)
	}
}
