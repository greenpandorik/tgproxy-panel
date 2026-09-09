package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeAtomic writes data to a temp file in the same directory, sets mode, and renames over path.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// chown is best-effort via the exec layer so tests on macOS do not need root.
func (h *Handler) chown(ctx context.Context, path, owner string) error {
	out, err := h.exec.Run(ctx, "chown", owner, path)
	if err != nil {
		return fmt.Errorf("chown %s %s: %s: %w", owner, path, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// safeSitePath rejects absolute paths, traversal and empty segments.
func safeSitePath(p string) (string, error) {
	clean := filepath.Clean("/" + p)
	if strings.Contains(p, "..") || clean == "/" {
		return "", fmt.Errorf("invalid site path %q", p)
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
