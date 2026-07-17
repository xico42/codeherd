package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
)

// itemsHerd builds a Herd whose configured projects are the keys of cloned,
// materializing the clone dir for the ones mapped to true so hrd.Project
// reports Cloned correctly. buildItems only touches cfg + the filesystem
// through the herd, so git/tmux deps are unnecessary.
func itemsHerd(t *testing.T, cloned map[string]bool) *herd.Herd {
	t.Helper()
	dir := t.TempDir()
	projects := make(map[string]config.ProjectConfig, len(cloned))
	for name, isCloned := range cloned {
		projects[name] = config.ProjectConfig{Repo: "git@github.com:user/" + name + ".git"}
		if isCloned {
			if err := os.MkdirAll(filepath.Join(dir, "github.com", "user", name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg := &config.Config{Defaults: config.DefaultsConfig{ProjectsDir: dir}, Projects: projects}
	return herd.New(cfg, nil, herd.Deps{})
}

func ref(project, branch string) herd.Ref { return herd.Ref{Project: project, Branch: branch} }

func agentHandle(id string, status herd.Status) *herd.Handle {
	return &herd.Handle{ID: id, Type: herd.SessionTypeAgent, Status: status}
}

func TestItem_FilterValue_worktree(t *testing.T) {
	i := Item{Project: "myapp", Branch: "feature", Group: groupAgent}
	want := "myapp / feature"
	if got := i.FilterValue(); got != want {
		t.Errorf("FilterValue() = %q, want %q", got, want)
	}
}

func TestItem_FilterValue_project(t *testing.T) {
	i := Item{Project: "myapp", Group: groupProject}
	want := "myapp"
	if got := i.FilterValue(); got != want {
		t.Errorf("FilterValue() = %q, want %q", got, want)
	}
}

func TestBuildItems_groupOrdering(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"api": true, "frontend": true, "infra": false, "myapp": true})
	spaces := []herd.Workspace{
		{Ref: ref("api", "develop"), DisplayBranch: "develop", Path: "/p/api/wt/develop"},
		{Ref: ref("myapp", "feature"), DisplayBranch: "feature", Path: "/p/myapp/wt/feature",
			Agent: agentHandle("$1", herd.StatusRunning)},
	}

	items := buildItems(hrd, spaces)

	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}

	first := items[0].(Item)
	if first.Project != "myapp" || first.Group != groupAgent {
		t.Errorf("item 0: got %s group %d, want myapp group %d", first.Project, first.Group, groupAgent)
	}
	second := items[1].(Item)
	if second.Project != "api" || second.Group != groupWorktree {
		t.Errorf("item 1: got %s group %d, want api group %d", second.Project, second.Group, groupWorktree)
	}
	third := items[2].(Item)
	if third.Project != "frontend" || third.Group != groupProject {
		t.Errorf("item 2: got %s group %d, want frontend group %d", third.Project, third.Group, groupProject)
	}
	fourth := items[3].(Item)
	if fourth.Project != "infra" || fourth.Group != groupProject {
		t.Errorf("item 3: got %s group %d, want infra group %d", fourth.Project, fourth.Group, groupProject)
	}
}

func TestBuildItems_agentStatus(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"myapp": true})
	spaces := []herd.Workspace{
		{Ref: ref("myapp", "feat"), DisplayBranch: "feat",
			Agent: &herd.Handle{ID: "$1", Type: herd.SessionTypeAgent, Status: herd.StatusWaiting, Annotation: "Allow?"}},
	}

	items := buildItems(hrd, spaces)
	item := items[0].(Item)

	if item.AgentStatus != semconv.StatusWaiting {
		t.Errorf("AgentStatus = %q, want %q", item.AgentStatus, semconv.StatusWaiting)
	}
	if item.Annotation != "Allow?" {
		t.Errorf("Annotation = %q, want %q", item.Annotation, "Allow?")
	}
}

func TestBuildItems_shellSession(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"api": true})
	spaces := []herd.Workspace{
		{Ref: ref("api", "dev"), DisplayBranch: "dev",
			Shell: &herd.Handle{ID: "$1", Type: herd.SessionTypeShell}},
	}

	items := buildItems(hrd, spaces)
	item := items[0].(Item)

	if !item.HasShell {
		t.Error("expected HasShell = true")
	}
	if item.ShellSessionID != "$1" {
		t.Errorf("ShellSessionID = %q, want $1", item.ShellSessionID)
	}
}

func TestBuildItems_cloneStatus(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"cloned-proj": true, "uncloned-proj": false})

	items := buildItems(hrd, nil)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	byName := map[string]Item{}
	for _, li := range items {
		it := li.(Item)
		byName[it.Project] = it
	}
	if !byName["cloned-proj"].Cloned {
		t.Error("expected cloned-proj to have Cloned = true")
	}
	if byName["uncloned-proj"].Cloned {
		t.Error("expected uncloned-proj to have Cloned = false")
	}
}

func TestBuildItems_isMain(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"myapp": true})
	spaces := []herd.Workspace{
		{Ref: ref("myapp", "main"), DisplayBranch: "main", Path: "/p/myapp", IsMain: true},
		{Ref: ref("myapp", "feature"), DisplayBranch: "feature", Path: "/p/myapp/wt/feature"},
	}

	items := buildItems(hrd, spaces)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	// After sorting (groupWorktree, alpha by project then branch):
	// "feature" < "main", so item 0 is feature, item 1 is main.
	featureItem := items[0].(Item)
	if featureItem.Branch != "feature" || featureItem.IsMain {
		t.Errorf("item 0 = %+v, want feature, not main", featureItem)
	}
	mainItem := items[1].(Item)
	if mainItem.Branch != "main" || !mainItem.IsMain {
		t.Errorf("item 1 = %+v, want main, IsMain", mainItem)
	}
}

func TestBuildItems_nilHerd_noProjectRows(t *testing.T) {
	// With a nil herd, buildItems maps only the given workspaces and adds no
	// project rows (it cannot enumerate configured projects).
	spaces := []herd.Workspace{
		{Ref: ref("myapp", "feature"), DisplayBranch: "feature", Path: "/p/myapp/wt/feature"},
	}
	items := buildItems(nil, spaces)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].(Item).IsMain {
		t.Error("IsMain should be false for a non-main workspace")
	}
}

func TestBuildItems_waitingSortsBeforeRunning(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"myapp": true})
	spaces := []herd.Workspace{
		{Ref: ref("myapp", "running-branch"), DisplayBranch: "running-branch",
			Agent: agentHandle("$1", herd.StatusRunning)},
		{Ref: ref("myapp", "waiting-branch"), DisplayBranch: "waiting-branch",
			Agent: agentHandle("$2", herd.StatusWaiting)},
	}

	items := buildItems(hrd, spaces)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].(Item).AgentStatus != semconv.StatusWaiting {
		t.Errorf("item 0 AgentStatus = %q, want waiting first", items[0].(Item).AgentStatus)
	}
	if items[1].(Item).AgentStatus != semconv.StatusRunning {
		t.Errorf("item 1 AgentStatus = %q, want running", items[1].(Item).AgentStatus)
	}
}

func TestBuildItems_alphabeticalWithinGroup(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"alpha": true, "zoo": true})
	spaces := []herd.Workspace{
		{Ref: ref("zoo", "main"), DisplayBranch: "main", Path: "/p/zoo/wt/main"},
		{Ref: ref("alpha", "main"), DisplayBranch: "main", Path: "/p/alpha/wt/main"},
		{Ref: ref("alpha", "beta"), DisplayBranch: "beta", Path: "/p/alpha/wt/beta"},
	}

	items := buildItems(hrd, spaces)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	i0, i1, i2 := items[0].(Item), items[1].(Item), items[2].(Item)
	if i0.Project != "alpha" || i0.Branch != "beta" {
		t.Errorf("item 0: got %s/%s, want alpha/beta", i0.Project, i0.Branch)
	}
	if i1.Project != "alpha" || i1.Branch != "main" {
		t.Errorf("item 1: got %s/%s, want alpha/main", i1.Project, i1.Branch)
	}
	if i2.Project != "zoo" {
		t.Errorf("item 2: got %s, want zoo", i2.Project)
	}
}

// buildItems is now a pure mapping: identity, display branch, and head-hint
// come straight from the Workspace herd.List returned. The correlation logic
// that used to live here is exercised in herd's workspace_test.go.
func TestBuildItems_carriesWorkspaceFields(t *testing.T) {
	hrd := itemsHerd(t, map[string]bool{"myapp": true})
	spaces := []herd.Workspace{
		{
			Ref:           ref("myapp", "feat"),
			DisplayBranch: "other",
			HeadHint:      "on other",
			Path:          "/p/myapp/wt/feat",
			Agent:         agentHandle("$9", herd.StatusRunning),
		},
	}

	items := buildItems(hrd, spaces)
	it := items[0].(Item)
	if it.Ref.Branch != "feat" {
		t.Errorf("Ref.Branch = %q, want feat (identity)", it.Ref.Branch)
	}
	if it.Branch != "other" {
		t.Errorf("Branch = %q, want other (display)", it.Branch)
	}
	if it.HeadHint != "on other" {
		t.Errorf("HeadHint = %q, want 'on other'", it.HeadHint)
	}
	if it.AgentSessionID != "$9" {
		t.Errorf("AgentSessionID = %q, want $9", it.AgentSessionID)
	}
}
