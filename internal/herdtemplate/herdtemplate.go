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
	DryRun       bool
}

// RenderedFile describes one template file that was processed.
type RenderedFile struct {
	Source string // e.g. "docker-compose.yml.herd"
	Target string // e.g. "docker-compose.yml"
	Output string // rendered content
}

// ProcessResult holds the outcome of a Process call.
type ProcessResult struct {
	Files []RenderedFile
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
// When ctx.DryRun is true, files are rendered but not written and hooks are skipped.
func (s *Service) Process(ctx ProcessContext, attrs map[string]string) (ProcessResult, error) {
	herdFiles, err := findHerdFiles(ctx.WorktreePath)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("scanning for .herd files: %w", err)
	}

	if len(herdFiles) == 0 {
		return ProcessResult{}, nil
	}

	if !ctx.DryRun {
		if err := s.hook.Trigger(semconv.HookPreTemplate, attrs, ctx.WorktreePath); err != nil {
			return ProcessResult{}, fmt.Errorf("pre-template hook: %w", err)
		}
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

	var result ProcessResult
	for _, path := range herdFiles {
		rf, err := renderFile(path, ctx, funcMap)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("rendering %s: %w", path, err)
		}
		result.Files = append(result.Files, rf)
	}

	if !ctx.DryRun {
		if err := s.hook.Trigger(semconv.HookPostTemplate, attrs, ctx.WorktreePath); err != nil {
			return ProcessResult{}, fmt.Errorf("post-template hook: %w", err)
		}
	}

	return result, nil
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

func renderFile(path string, ctx ProcessContext, funcMap template.FuncMap) (RenderedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return RenderedFile{}, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return RenderedFile{}, fmt.Errorf("executing template: %w", err)
	}

	outPath := strings.TrimSuffix(path, herdSuffix)

	if !ctx.DryRun {
		if err := os.WriteFile(outPath, buf.Bytes(), 0o600); err != nil {
			return RenderedFile{}, fmt.Errorf("writing output: %w", err)
		}
	}

	return RenderedFile{
		Source: path,
		Target: outPath,
		Output: buf.String(),
	}, nil
}
