package cmd

import (
	"testing"

	"github.com/xico42/codeherd/internal/semconv"
)

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
