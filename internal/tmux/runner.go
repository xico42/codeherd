package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// SocketEnvVar is the environment variable name read by RealRunner. When set,
// its value is passed as `-S <path>` to every tmux invocation. Used by
// integration tests to give each test a private tmux server (socket under a
// per-test t.TempDir), so tests cannot leak sessions into the developer's
// outer tmux server and cannot collide across tests.
const SocketEnvVar = "CODEHERD_TMUX_SOCKET"

// Runner executes raw tmux commands. Implement this interface in tests to avoid
// spawning a real tmux process.
type Runner interface {
	Run(args ...string) (stdout, stderr string, exitCode int, err error)
}

// RealRunner executes tmux via os/exec.
type RealRunner struct{}

// NewRealRunner returns a Runner backed by the system tmux binary.
func NewRealRunner() *RealRunner { return &RealRunner{} }

// Run executes tmux with the given arguments. Returns stdout, stderr, exit code,
// and a non-nil err only when the process could not be started at all.
//
// If the SocketEnvVar environment variable is set, its value is inserted as
// `-S <path>` before the tmux subcommand so that the call targets an isolated
// tmux server. The flag is only injected for top-level tmux subcommands (i.e.
// when args[0] is not itself a global flag like -S/-L); production code always
// calls subcommands directly.
func (r *RealRunner) Run(args ...string) (stdout, stderr string, exitCode int, err error) {
	if socket := os.Getenv(SocketEnvVar); socket != "" {
		args = append([]string{"-S", socket}, args...)
	}
	// tmux binary is fixed; args come from typed Client methods, with an
	// optional `-S <socket>` injected from a process-local env var that is
	// only set by tests.
	cmd := exec.Command("tmux", args...) // #nosec G702
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return stdout, stderr, exitErr.ExitCode(), nil
		}
		return stdout, stderr, -1, fmt.Errorf("running tmux %v: %w", args, runErr)
	}
	return stdout, stderr, 0, nil
}
