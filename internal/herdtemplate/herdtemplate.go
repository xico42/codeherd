package herdtemplate

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

const herdSuffix = ".herd"

// DeterministicPort returns a stable port for the given project/branch/name.
// Uses FNV-1a 32-bit hash with null-byte separators. Range: 10000–59999.
func DeterministicPort(project, branch, name string) int {
	key := project + "\x00" + branch + "\x00" + name
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()%50000) + 10000
}

// ProcessContext holds values available to .herd templates.
type ProcessContext struct {
	Project      string
	Branch       string
	WorktreePath string
	SessionName  string
}

// Service processes .herd template files in worktrees.
type Service struct {
	hook hooks.Hook
}

// New creates a template processing service.
func New(hook hooks.Hook) *Service {
	return &Service{hook: hook}
}

// Process walks the worktree directory, finds all .herd files, renders them,
// and writes the output without the .herd suffix. Triggers pre/post-template hooks.
func (s *Service) Process(ctx ProcessContext, attrs map[string]string) error {
	herdFiles, err := findHerdFiles(ctx.WorktreePath)
	if err != nil {
		return fmt.Errorf("scanning for .herd files: %w", err)
	}

	if len(herdFiles) == 0 {
		return nil
	}

	if err := s.hook.Trigger(semconv.HookPreTemplate, attrs, ctx.WorktreePath); err != nil {
		return fmt.Errorf("pre-template hook: %w", err)
	}

	funcMap := template.FuncMap{
		"port": func(name string) int {
			return DeterministicPort(ctx.Project, ctx.Branch, name)
		},
		"env": func(args ...string) string {
			if len(args) == 0 {
				return ""
			}
			if v := os.Getenv(args[0]); v != "" {
				return v
			}
			if len(args) > 1 {
				return args[1]
			}
			return ""
		},
	}

	for _, path := range herdFiles {
		if err := renderFile(path, ctx, funcMap); err != nil {
			return fmt.Errorf("rendering %s: %w", path, err)
		}
	}

	if err := s.hook.Trigger(semconv.HookPostTemplate, attrs, ctx.WorktreePath); err != nil {
		return fmt.Errorf("post-template hook: %w", err)
	}

	return nil
}

func findHerdFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, herdSuffix) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	return files, nil
}

func renderFile(path string, ctx ProcessContext, funcMap template.FuncMap) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	outPath := strings.TrimSuffix(path, herdSuffix)
	if err := os.WriteFile(outPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
