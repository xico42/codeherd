// Package filecopy copies configured files into new worktrees.
package filecopy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xico42/codeherd/internal/hooks"
	"github.com/xico42/codeherd/internal/semconv"
)

// Service copies files into worktrees.
type Service struct {
	hook hooks.Hook
}

// New creates a file copy service.
func New(hook hooks.Hook) *Service {
	return &Service{hook: hook}
}

// Copy processes the files list, copying each entry from baseDir (or absolute
// path) to targetDir. Triggers pre-copy and post-copy hooks. Destinations are
// confined to targetDir; sources may be anywhere the user points them.
func (s *Service) Copy(files []string, baseDir, targetDir string, attrs map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	if err := s.hook.Trigger(semconv.HookPreCopy, attrs, targetDir); err != nil {
		return fmt.Errorf("pre-copy hook: %w", err)
	}

	if err := copyAll(files, baseDir, targetDir); err != nil {
		return err
	}

	if err := s.hook.Trigger(semconv.HookPostCopy, attrs, targetDir); err != nil {
		return fmt.Errorf("post-copy hook: %w", err)
	}

	return nil
}

// parseEntry splits a file entry into source and destination.
func parseEntry(entry string) (src, dst string) {
	if idx := strings.LastIndex(entry, ":"); idx > 0 {
		return entry[:idx], entry[idx+1:]
	}
	return entry, entry
}

// resolveSrc resolves a source path.
func resolveSrc(src, baseDir string) string {
	if strings.HasPrefix(src, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return src
		}
		return filepath.Join(home, src[2:])
	}
	if filepath.IsAbs(src) {
		return src
	}
	return filepath.Join(baseDir, src)
}

// copyAll copies every entry into targetDir through an os.Root, so no entry
// can write outside it.
func copyAll(files []string, baseDir, targetDir string) (err error) {
	root, err := os.OpenRoot(targetDir)
	if err != nil {
		return fmt.Errorf("opening target %s: %w", targetDir, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()

	for _, entry := range files {
		src, dst := parseEntry(entry)
		if err := copyFile(resolveSrc(src, baseDir), root, dst); err != nil {
			return fmt.Errorf("copying %s: %w", entry, err)
		}
	}
	return nil
}

// copyFile copies src to dst inside root, creating intermediate directories.
// A failed close is reported: for dst it can mean the copy never reached the
// disk.
func copyFile(src string, root *os.Root, dst string) (err error) {
	// src is the path the user configured; there is no directory to confine it to.
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return fmt.Errorf("opening source %s: %w", src, err)
	}
	defer func() { err = errors.Join(err, in.Close()) }()

	if err := root.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	out, err := root.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination %s: %w", dst, err)
	}
	defer func() { err = errors.Join(err, out.Close()) }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}
	return nil
}
