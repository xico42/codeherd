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

const (
	herdSuffix = ".herd"
	portMin    = 10000
	portMax    = 59999
	portRange  = portMax - portMin + 1
)

// DeterministicPortWithSeed returns a stable port in [10000, 59999] for the
// given project / branch / name / seed. Different seeds produce independent
// outputs for the same (project, branch, name), enabling multi-hash allocation
// strategies and manual disambiguation of collisions.
func DeterministicPortWithSeed(project, branch, name, seed string) int {
	key := project + "\x00" + branch + "\x00" + name + "\x00" + seed
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()%portRange) + portMin
}

// portAllocator hands out deterministic ports for one Process() call while
// resolving collisions. Each name first tries h1 (empty seed), then h2 (seed
// "alt"), then linear-probes forward with wrap. Returns an error only when
// every slot in [portMin, portMax] is occupied. State is discarded after the
// render completes.
type portAllocator struct {
	project, branch string
	byName          map[string]int
	byPort          map[int]string
}

func newPortAllocator(project, branch string) *portAllocator {
	return &portAllocator{
		project: project,
		branch:  branch,
		byName:  make(map[string]int),
		byPort:  make(map[int]string),
	}
}

func (a *portAllocator) allocate(name string) (int, error) {
	if p, ok := a.byName[name]; ok {
		return p, nil
	}
	h1 := DeterministicPortWithSeed(a.project, a.branch, name, "")
	if _, taken := a.byPort[h1]; !taken {
		a.byName[name] = h1
		a.byPort[h1] = name
		return h1, nil
	}
	h2 := DeterministicPortWithSeed(a.project, a.branch, name, "alt")
	if h2 != h1 {
		if _, taken := a.byPort[h2]; !taken {
			a.byName[name] = h2
			a.byPort[h2] = name
			return h2, nil
		}
	}
	p := h1
	for {
		p++
		if p > portMax {
			p = portMin
		}
		if p == h1 {
			return 0, fmt.Errorf("port allocator exhausted: no free slot in [%d, %d] for %q", portMin, portMax, name)
		}
		if _, taken := a.byPort[p]; !taken {
			a.byName[name] = p
			a.byPort[p] = name
			return p, nil
		}
	}
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

	alloc := newPortAllocator(ctx.Project, ctx.Branch)
	funcMap := template.FuncMap{
		"port": func(name string) (int, error) {
			return alloc.allocate(name)
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
