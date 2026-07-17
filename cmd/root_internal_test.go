package cmd

import (
	"os"
	"testing"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/herd"
	"github.com/xico42/codeherd/internal/semconv"
)

// TestMain seeds a default Herd for the internal package-cmd tests that invoke
// a command's Run directly, bypassing PersistentPreRunE (which constructs h in
// production). Tests that need a specific config or tmux runner override h via
// setHerdTmux (cmd/session_internal_test.go).
func TestMain(m *testing.M) {
	h = herd.New(&config.Config{}, nil, herd.Deps{})
	os.Exit(m.Run())
}

func TestResolveProfileArg(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string // "" means unset CODEHERD_PROFILE
		want string
	}{
		{name: "flag only", flag: "work", env: "", want: "work"},
		{name: "env only", flag: "", env: "work", want: "work"},
		{name: "flag beats env", flag: "personal", env: "work", want: "personal"},
		{name: "neither set", flag: "", env: "", want: ""},
		{name: "empty env treated as unset", flag: "", env: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(semconv.EnvProfile, tt.env)
			if got := resolveProfileArg(tt.flag); got != tt.want {
				t.Errorf("resolveProfileArg(%q) with %s=%q = %q, want %q",
					tt.flag, semconv.EnvProfile, tt.env, got, tt.want)
			}
		})
	}
}
