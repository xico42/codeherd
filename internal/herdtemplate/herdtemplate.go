// Package herdtemplate renders *.herd template files into their unsuffixed
// counterparts, with deterministic port allocation and environment lookups.
package herdtemplate

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
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
	_, _ = h.Write([]byte(key)) // hash.Hash.Write never returns an error
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
//
// Every read and write goes through an os.Root on the worktree: templates come
// from the cloned repo, and a symlink in it must not make rendering read or
// write outside the worktree.
func (s *Service) Process(ctx ProcessContext, attrs map[string]string) (result ProcessResult, err error) {
	root, err := os.OpenRoot(ctx.WorktreePath)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("scanning for .herd files: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()

	herdFiles, err := findHerdFiles(root.FS())
	if err != nil {
		return ProcessResult{}, fmt.Errorf("scanning for .herd files: %w", err)
	}
	if len(herdFiles) == 0 {
		return ProcessResult{}, nil
	}

	if err := s.trigger(semconv.HookPreTemplate, ctx, attrs); err != nil {
		return ProcessResult{}, err
	}
	result, err = renderAll(root, herdFiles, ctx)
	if err != nil {
		return ProcessResult{}, err
	}
	if err := s.trigger(semconv.HookPostTemplate, ctx, attrs); err != nil {
		return ProcessResult{}, err
	}
	return result, nil
}

// trigger runs a template hook. A dry run skips hooks: it must have no side
// effects.
func (s *Service) trigger(name string, ctx ProcessContext, attrs map[string]string) error {
	if ctx.DryRun {
		return nil
	}
	if err := s.hook.Trigger(name, attrs, ctx.WorktreePath); err != nil {
		return fmt.Errorf("%s hook: %w", name, err)
	}
	return nil
}

// templateFuncs returns the functions available to .herd templates.
func templateFuncs(alloc *portAllocator) template.FuncMap {
	return template.FuncMap{
		"port": alloc.allocate,
		"env":  envOr,
	}
}

// envOr implements the template func `env "VAR" "default"`: the variable's
// value when set and non-empty, else the default, else "".
func envOr(args ...string) string {
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
}

// renderAll renders the named templates in order. They share one port
// allocator, so ports stay unique across files.
func renderAll(root *os.Root, names []string, ctx ProcessContext) (ProcessResult, error) {
	funcs := templateFuncs(newPortAllocator(ctx.Project, ctx.Branch))
	var result ProcessResult
	for _, name := range names {
		rf, err := renderFile(root, name, ctx, funcs)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("rendering %s: %w", filepath.Join(ctx.WorktreePath, name), err)
		}
		result.Files = append(result.Files, rf)
	}
	return result, nil
}

// findHerdFiles returns the .herd files under fsys as OS paths relative to it.
func findHerdFiles(fsys fs.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, herdSuffix) {
			files = append(files, filepath.FromSlash(path))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	return files, nil
}

// renderFile renders the template at name inside root and, unless this is a
// dry run, writes the result next to it without the .herd suffix.
func renderFile(root *os.Root, name string, ctx ProcessContext, funcs template.FuncMap) (RenderedFile, error) {
	data, err := root.ReadFile(name)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New(filepath.Base(name)).Funcs(funcs).Parse(string(data))
	if err != nil {
		return RenderedFile{}, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return RenderedFile{}, fmt.Errorf("executing template: %w", err)
	}

	outName := strings.TrimSuffix(name, herdSuffix)

	if !ctx.DryRun {
		if err := root.WriteFile(outName, buf.Bytes(), 0o600); err != nil {
			return RenderedFile{}, fmt.Errorf("writing output: %w", err)
		}
	}

	return RenderedFile{
		Source: filepath.Join(ctx.WorktreePath, name),
		Target: filepath.Join(ctx.WorktreePath, outName),
		Output: buf.String(),
	}, nil
}
