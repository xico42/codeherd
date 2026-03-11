package filecopy

import (
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
// path) to targetDir. Triggers pre-copy and post-copy hooks.
func (s *Service) Copy(files []string, baseDir, targetDir string, attrs map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	if err := s.hook.Trigger(semconv.HookPreCopy, attrs, targetDir); err != nil {
		return fmt.Errorf("pre-copy hook: %w", err)
	}

	for _, entry := range files {
		src, dst := parseEntry(entry)
		srcPath := resolveSrc(src, baseDir)
		dstPath := filepath.Join(targetDir, dst)

		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copying %s: %w", entry, err)
		}
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

// copyFile copies a single file, creating intermediate directories.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}
	return nil
}
