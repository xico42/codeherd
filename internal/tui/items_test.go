package tui

import (
	"testing"

	"github.com/xico42/codeherd/internal/semconv"
)

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
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "api", branch: "develop", path: "/p/api/wt/develop"},
			{project: "myapp", branch: "feature", path: "/p/myapp/wt/feature"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {status: semconv.StatusRunning},
		},
		shellSessions: map[string]string{},
		projects: []projEntry{
			{name: "api", cloned: true},
			{name: "frontend", cloned: true},
			{name: "infra", cloned: false},
			{name: "myapp", cloned: true},
		},
	}

	items := buildItems(data)

	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}

	// Group 1: worktrees with agents
	first := items[0].(Item)
	if first.Project != "myapp" || first.Group != groupAgent {
		t.Errorf("item 0: got %s group %d, want myapp group %d", first.Project, first.Group, groupAgent)
	}

	// Group 2: worktrees without agents
	second := items[1].(Item)
	if second.Project != "api" || second.Group != groupWorktree {
		t.Errorf("item 1: got %s group %d, want api group %d", second.Project, second.Group, groupWorktree)
	}

	// Group 3: projects without worktrees (alphabetical)
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
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "feat", path: "/p/wt/feat"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feat": {status: semconv.StatusWaiting, annotation: "Allow?"},
		},
		shellSessions: map[string]string{},
		projects:      []projEntry{{name: "myapp", cloned: true}},
	}

	items := buildItems(data)
	item := items[0].(Item)

	if item.AgentStatus != semconv.StatusWaiting {
		t.Errorf("AgentStatus = %q, want %q", item.AgentStatus, semconv.StatusWaiting)
	}
	if item.Annotation != "Allow?" {
		t.Errorf("Annotation = %q, want %q", item.Annotation, "Allow?")
	}
}

func TestBuildItems_shellSession(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "api", branch: "dev", path: "/p/wt/dev"},
		},
		agentSessions: map[string]agentInfo{},
		shellSessions: map[string]string{"api-dev": "$1"},
		projects:      []projEntry{{name: "api", cloned: true}},
	}

	items := buildItems(data)
	item := items[0].(Item)

	if !item.HasShell {
		t.Error("expected HasShell = true")
	}
}

func TestBuildItems_cloneStatus(t *testing.T) {
	data := refreshResult{
		worktrees:     []wtEntry{},
		agentSessions: map[string]agentInfo{},
		shellSessions: map[string]string{},
		projects: []projEntry{
			{name: "cloned-proj", cloned: true},
			{name: "uncloned-proj", cloned: false},
		},
	}

	items := buildItems(data)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	first := items[0].(Item)
	if !first.Cloned {
		t.Error("expected cloned-proj to have Cloned = true")
	}

	second := items[1].(Item)
	if second.Cloned {
		t.Error("expected uncloned-proj to have Cloned = false")
	}
}

func TestBuildItems_isMain(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "main", path: "/projects/github.com/user/myapp"},
			{project: "myapp", branch: "feature", path: "/projects/github.com/user/myapp/worktrees/feature"},
		},
		agentSessions: map[string]agentInfo{},
		shellSessions: map[string]string{},
		projects:      []projEntry{{name: "myapp", cloned: true}},
		cloneDirs:     map[string]string{"myapp": "/projects/github.com/user/myapp"},
	}

	items := buildItems(data)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	// After sorting (groupWorktree, alpha by project then branch):
	// "feature" < "main", so item 0 is feature, item 1 is main.
	featureItem := items[0].(Item)
	if featureItem.Branch != "feature" {
		t.Fatalf("item 0: expected branch feature, got %s", featureItem.Branch)
	}
	if featureItem.IsMain {
		t.Error("feature worktree should not be IsMain")
	}

	mainItem := items[1].(Item)
	if mainItem.Branch != "main" {
		t.Fatalf("item 1: expected branch main, got %s", mainItem.Branch)
	}
	if !mainItem.IsMain {
		t.Error("main worktree should be IsMain")
	}
}

func TestBuildItems_isMain_noCfg(t *testing.T) {
	// When cfg is nil, refreshCmd does not allocate cloneDirs (nil map).
	// buildItems must handle nil cloneDirs without panicking and must not
	// set IsMain on any worktree item.
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "main", path: "/projects/github.com/user/myapp"},
		},
		agentSessions: map[string]agentInfo{},
		shellSessions: map[string]string{},
		projects:      []projEntry{{name: "myapp", cloned: true}},
		cloneDirs:     nil, // nil: produced when cfg == nil
	}

	items := buildItems(data)
	item := items[0].(Item)
	if item.IsMain {
		t.Error("IsMain should be false when cloneDirs is nil (no cfg)")
	}
}

func TestBuildItems_waitingSortsBeforeRunning(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "running-branch", path: "/p/wt/running-branch"},
			{project: "myapp", branch: "waiting-branch", path: "/p/wt/waiting-branch"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-running-branch": {status: semconv.StatusRunning},
			"myapp-waiting-branch": {status: semconv.StatusWaiting},
		},
		shellSessions: map[string]string{},
		projects:      []projEntry{{name: "myapp", cloned: true}},
	}

	items := buildItems(data)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	first := items[0].(Item)
	if first.AgentStatus != semconv.StatusWaiting {
		t.Errorf("item 0: AgentStatus = %q, want %q (waiting should sort first)", first.AgentStatus, semconv.StatusWaiting)
	}

	second := items[1].(Item)
	if second.AgentStatus != semconv.StatusRunning {
		t.Errorf("item 1: AgentStatus = %q, want %q", second.AgentStatus, semconv.StatusRunning)
	}
}

func TestBuildItems_alphabeticalWithinGroup(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "zoo", branch: "main", path: "/p/zoo__worktrees/main"},
			{project: "alpha", branch: "main", path: "/p/alpha__worktrees/main"},
			{project: "alpha", branch: "beta", path: "/p/alpha__worktrees/beta"},
		},
		agentSessions: map[string]agentInfo{},
		shellSessions: map[string]string{},
		projects: []projEntry{
			{name: "alpha", cloned: true},
			{name: "zoo", cloned: true},
		},
	}

	items := buildItems(data)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}

	// All group 2 (no agents), alphabetical by project then branch
	i0 := items[0].(Item)
	i1 := items[1].(Item)
	i2 := items[2].(Item)

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

func TestBuildItems_detachedWorktreeStillCorrelates(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "", path: "/p/myapp__worktrees/feature", detached: true},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$1", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{"myapp-feature": "feature"},
	}

	items := buildItems(data)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$1" {
		t.Errorf("detached worktree lost its agent session: %+v", it)
	}
	if it.HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature (from @codeherd_branch)", it.Branch)
	}
}

func TestBuildItems_otherBranchCheckedOut(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "hotfix", path: "/p/myapp__worktrees/feature"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$2", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{}, // pre-upgrade session: no raw branch
	}

	items := buildItems(data)
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$2" {
		t.Errorf("worktree with other branch lost its session: %+v", it)
	}
	if it.HeadHint != "on hotfix" {
		t.Errorf("HeadHint = %q, want 'on hotfix'", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature (folder fallback)", it.Branch)
	}
}

func TestBuildItems_cloneDirCorrelatesViaDefaultBranch(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "", path: "/p/myapp", detached: true},
		},
		agentSessions: map[string]agentInfo{
			"myapp-main": {sessionID: "$3", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{},
	}

	items := buildItems(data)
	it := items[0].(Item)
	if !it.HasAgent || it.AgentSessionID != "$3" {
		t.Errorf("clone-dir worktree lost its session: %+v", it)
	}
	if it.HeadHint != "detached" {
		t.Errorf("HeadHint = %q, want detached", it.HeadHint)
	}
	if !it.IsMain {
		t.Errorf("expected IsMain=true for clone dir")
	}
	if it.Branch != "main" {
		t.Errorf("Branch = %q, want main (from config default)", it.Branch)
	}
}

func TestBuildItems_normalWorktreeNoHint(t *testing.T) {
	data := refreshResult{
		worktrees: []wtEntry{
			{project: "myapp", branch: "feature", path: "/p/myapp__worktrees/feature"},
		},
		agentSessions: map[string]agentInfo{
			"myapp-feature": {sessionID: "$4", status: semconv.StatusRunning},
		},
		shellSessions:   map[string]string{},
		cloneDirs:       map[string]string{"myapp": "/p/myapp"},
		defaultBranches: map[string]string{"myapp": "main"},
		sessionBranch:   map[string]string{"myapp-feature": "feature"},
	}

	items := buildItems(data)
	it := items[0].(Item)
	if it.HeadHint != "" {
		t.Errorf("HeadHint = %q, want empty for non-diverged worktree", it.HeadHint)
	}
	if it.Branch != "feature" {
		t.Errorf("Branch = %q, want feature", it.Branch)
	}
}
