package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
	"github.com/xico42/codeherd/internal/session"
	"github.com/xico42/codeherd/internal/tmux"
)

// fakeRunner is a minimal tmux.Runner for helper tests. It returns a
// canned list-sessions stdout for the first call and empty-ok for any
// follow-up (e.g. kill-session).
type fakeRunner struct {
	listStdout string
	calls      [][]string
	idx        int
}

func (f *fakeRunner) Run(args ...string) (string, string, int, error) {
	f.calls = append(f.calls, args)
	f.idx++
	if f.idx == 1 {
		// First call is list-sessions.
		if f.listStdout == "" {
			return "", "", 1, nil // exit 1 = no sessions
		}
		return f.listStdout, "", 0, nil
	}
	return "", "", 0, nil
}

func setRegistry(t *testing.T, r *config.ProfileRegistry) {
	t.Helper()
	orig := registry
	registry = r
	t.Cleanup(func() { registry = orig })
}

func newHelperSessionService(r tmux.Runner) *session.Service {
	return session.NewService(tmux.NewClient(r), &hooks.NoOp{})
}

// tabRecord builds a list-sessions line matching the 8-field tab format
// defined in internal/tmux/client.go ListSessions: id, name, canonical,
// type, status, annotation, started_at, profile.
func tabRecord(id, name, canonical, sessionType, status, profile string) string {
	return strings.Join([]string{id, name, canonical, sessionType, status, "", "", profile}, "\t") + "\n"
}

func callMatches(calls [][]string, want ...string) bool {
	for _, c := range calls {
		joined := strings.Join(c, " ")
		ok := true
		for _, w := range want {
			if !strings.Contains(joined, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestActiveProfile_NilRegistry(t *testing.T) {
	setRegistry(t, nil)
	if got := activeProfile(); got != "" {
		t.Errorf("activeProfile() = %q, want empty string", got)
	}
}

func TestActiveProfile_PopulatedRegistry(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	if got := activeProfile(); got != "work" {
		t.Errorf("activeProfile() = %q, want %q", got, "work")
	}
}

func TestActiveProfile_EmptyActive(t *testing.T) {
	// Registry present but Active == "" (profile mode off in-practice).
	setRegistry(t, &config.ProfileRegistry{Active: ""})
	if got := activeProfile(); got != "" {
		t.Errorf("activeProfile() = %q, want empty", got)
	}
}

func TestListSessionsForProfile_NoProfile_ReturnsAll(t *testing.T) {
	setRegistry(t, nil)
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "") +
		tabRecord("$2", "work-myapp-feat", "work-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "work")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	got, err := listSessionsForProfile(svc)
	if err != nil {
		t.Fatalf("listSessionsForProfile() error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(sessions) = %d, want 2 (all records returned when profile is off)", len(got))
	}
}

func TestListSessionsForProfile_ActiveProfile_FiltersMatchingOnly(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "") +
		tabRecord("$2", "work-myapp-feat", "work-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "work") +
		tabRecord("$3", "personal-myapp-feat", "personal-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "personal") +
		tabRecord("$4", "work-other-main", "work-other-main", semconv.SessionTypeShell, semconv.StatusRunning, "work")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	got, err := listSessionsForProfile(svc)
	if err != nil {
		t.Fatalf("listSessionsForProfile() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(sessions) = %d, want 2 (only records with Profile==work)", len(got))
	}
	for _, s := range got {
		if s.Profile != "work" {
			t.Errorf("returned session %q has Profile=%q, want work", s.Name, s.Profile)
		}
	}
}

func TestListSessionsForProfile_NoMatches_ReturnsEmpty(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	stdout := tabRecord("$1", "personal-myapp-feat", "personal-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "personal")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	got, err := listSessionsForProfile(svc)
	if err != nil {
		t.Fatalf("listSessionsForProfile() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(sessions) = %d, want 0", len(got))
	}
}

func TestShowSessionForProfile_NoProfile_HitsPlainShow(t *testing.T) {
	setRegistry(t, nil)
	// Plain Show looks up by canonical "myapp-feat" (profile == "").
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	info, err := showSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent)
	if err != nil {
		t.Fatalf("showSessionForProfile() error = %v", err)
	}
	if info == nil || info.Name != "myapp-feat" {
		t.Errorf("info = %+v, want Name=myapp-feat", info)
	}
	if info.Profile != "" {
		t.Errorf("info.Profile = %q, want empty", info.Profile)
	}
}

func TestShowSessionForProfile_ActiveProfile_HitsShowByName(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	// Also include a non-profile record with the same short name to
	// prove the helper targets the profile-prefixed canonical key.
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "") +
		tabRecord("$2", "work-myapp-feat", "work-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "work")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	info, err := showSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent)
	if err != nil {
		t.Fatalf("showSessionForProfile() error = %v", err)
	}
	if info == nil || info.Name != "work-myapp-feat" {
		t.Errorf("info.Name = %+v, want work-myapp-feat (profile-prefixed canonical)", info)
	}
	if info.Profile != "work" {
		t.Errorf("info.Profile = %q, want work", info.Profile)
	}
}

func TestShowSessionForProfile_ActiveProfile_NotFound(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	// Only a non-profile record exists — ShowByName for "work-myapp-feat" must miss.
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	_, err := showSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent)
	if err == nil {
		t.Fatal("expected ErrSessionNotFound, got nil")
	}
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("err = %v, want errors.Is(err, ErrSessionNotFound) == true (%%w wrap must preserve sentinel)", err)
	}
}

func TestStopSessionForProfile_NoProfile_TargetsPlainName(t *testing.T) {
	setRegistry(t, nil)
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	if err := stopSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent); err != nil {
		t.Fatalf("stopSessionForProfile() error = %v", err)
	}
	if !callMatches(r.calls, "kill-session", "-t", "myapp-feat") {
		t.Errorf("expected kill-session -t myapp-feat; calls = %v", r.calls)
	}
}

func TestStopSessionForProfile_ActiveProfile_TargetsProfilePrefixedName(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	// Two records sharing the short name; only the profile-prefixed one should be killed.
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "") +
		tabRecord("$2", "work-myapp-feat", "work-myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "work")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	if err := stopSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent); err != nil {
		t.Fatalf("stopSessionForProfile() error = %v", err)
	}
	if !callMatches(r.calls, "kill-session", "-t", "work-myapp-feat") {
		t.Errorf("expected kill-session -t work-myapp-feat; calls = %v", r.calls)
	}
	// Make sure we did NOT kill the non-profile session.
	for _, c := range r.calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "kill-session") && strings.Contains(joined, "-t myapp-feat") && !strings.Contains(joined, "work-myapp-feat") {
			t.Errorf("kill-session targeted non-profile session: %v", c)
		}
	}
}

func TestStopSessionForProfile_ActiveProfile_NotFound(t *testing.T) {
	setRegistry(t, &config.ProfileRegistry{Active: "work"})
	stdout := tabRecord("$1", "myapp-feat", "myapp-feat", semconv.SessionTypeAgent, semconv.StatusRunning, "")
	r := &fakeRunner{listStdout: stdout}
	svc := newHelperSessionService(r)

	err := stopSessionForProfile(svc, "myapp", "feat", semconv.SessionTypeAgent)
	if err == nil {
		t.Fatal("expected ErrSessionNotFound, got nil")
	}
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("err = %v, want errors.Is(err, ErrSessionNotFound) == true", err)
	}
}
