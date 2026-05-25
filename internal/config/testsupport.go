package config

import "github.com/xico42/codeherd/internal/semconv"

// EnvProfileForTest returns the env var name that carries the active
// profile into a session. Exposed for tests that exercise precedence.
func EnvProfileForTest() string {
	return semconv.EnvProfile
}
