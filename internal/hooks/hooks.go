package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/xico42/codeherd/internal/config"
	"github.com/xico42/codeherd/internal/semconv"
)

// Hook triggers lifecycle hooks by name.
type Hook interface {
	Trigger(name string, attrs map[string]string, workDir string) error
}

// Service maps hook names to configured commands and executes them.
type Service struct {
	hooks map[string]string
}

// New creates a Hook from the given config.
func New(cfg config.HooksConfig) *Service {
	return &Service{
		hooks: map[string]string{
			semconv.HookPreClone:     cfg.PreClone,
			semconv.HookPostClone:    cfg.PostClone,
			semconv.HookPreWorktree:  cfg.PreWorktree,
			semconv.HookPostWorktree: cfg.PostWorktree,
			semconv.HookPreCopy:      cfg.PreCopy,
			semconv.HookPostCopy:     cfg.PostCopy,
			semconv.HookPreTemplate:  cfg.PreTemplate,
			semconv.HookPostTemplate: cfg.PostTemplate,
			semconv.HookPreSession:   cfg.PreSession,
			semconv.HookPostSession:  cfg.PostSession,
		},
	}
}

// Trigger runs the hook command for the given name. Returns nil if the hook
// is not configured (empty command). Returns an error on non-zero exit code.
func (s *Service) Trigger(name string, attrs map[string]string, workDir string) error {
	command := s.hooks[name]
	if command == "" {
		return nil
	}

	cmd := exec.Command("sh", "-c", command)

	// Inherit current environment and add hook attributes.
	cmd.Env = os.Environ()
	for k, v := range attrs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %q failed (command: %s): %w\n%s",
			name, command, err, stderr.String())
	}
	return nil
}

// NoOp is a Hook that does nothing. Used in tests and when hooks are not configured.
type NoOp struct{}

// Trigger always returns nil.
func (n *NoOp) Trigger(name string, attrs map[string]string, workDir string) error {
	return nil
}
