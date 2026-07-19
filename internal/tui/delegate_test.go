package tui

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"

	"github.com/xico42/codeherd/internal/herd"
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

// The main worktree, when its HEAD is on a non-default branch, must render as
// "<project> / main (on <live>)" — the identity branch labels the row and the
// checkout is a hint. The regression it guards: rendering the live branch as
// the label produced "geomonitor / docs/rbac-epic (on docs/rbac-epic)", with
// the live branch doubled and "main" gone. Starts from a herd.Workspace so it
// exercises the real buildItems -> delegate path, not a hand-built Item.
func TestDelegate_Render_mainWorktreeDivergedHead(t *testing.T) {
	spaces := []herd.Workspace{{
		Ref:           herd.Ref{Project: "geomonitor", Branch: "main"},
		DisplayBranch: "main",
		HeadHint:      "on docs/rbac-epic",
		IsMain:        true,
		Path:          "/p/geomonitor",
	}}
	items := buildItems(nil, spaces)
	d := newDelegate()
	m := list.New(items, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, items[0])
	out := buf.String()

	if !strings.Contains(out, "geomonitor / main (on docs/rbac-epic)") {
		t.Errorf("render should label the main row by identity, got: %q", out)
	}
	if strings.Contains(out, "docs/rbac-epic (on docs/rbac-epic)") {
		t.Errorf("render doubled the live branch instead of showing main, got: %q", out)
	}
}

// A non-main worktree with no session shows git's live branch, clean — the
// folder name never surfaces and there is no divergence hint. This is the
// geomonitor chore-cron-rework row. Starts from a herd.Workspace so it
// exercises the real buildItems -> delegate path.
func TestDelegate_Render_nonMainNoSessionShowsLiveBranch(t *testing.T) {
	spaces := []herd.Workspace{{
		Ref:           herd.Ref{Project: "geomonitor", Branch: "chore-cron-rework"},
		DisplayBranch: "chore/restore-cron-rework",
		HeadHint:      "",
		Path:          "/p/geomonitor/wt/chore-cron-rework",
	}}
	items := buildItems(nil, spaces)
	d := newDelegate()
	m := list.New(items, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, items[0])
	out := buf.String()

	if !strings.Contains(out, "geomonitor / chore/restore-cron-rework") {
		t.Errorf("render should show the live branch, got: %q", out)
	}
	if strings.Contains(out, "(on ") || strings.Contains(out, "chore-cron-rework (") {
		t.Errorf("render should not add a hint or show the folder name, got: %q", out)
	}
}

// With a session proving the divergence, the row shows the recorded branch and
// the live checkout as a hint.
func TestDelegate_Render_sessionDivergenceShowsRecordedBranch(t *testing.T) {
	spaces := []herd.Workspace{{
		Ref:           herd.Ref{Project: "geomonitor", Branch: "feat"},
		DisplayBranch: "feat",
		HeadHint:      "on other",
		Path:          "/p/geomonitor/wt/feat",
	}}
	items := buildItems(nil, spaces)
	d := newDelegate()
	m := list.New(items, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, m, 0, items[0])
	out := buf.String()

	if !strings.Contains(out, "geomonitor / feat (on other)") {
		t.Errorf("render should show recorded branch + hint, got: %q", out)
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
