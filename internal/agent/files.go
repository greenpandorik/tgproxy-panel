package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// writeAtomic writes data to a temp file in the same directory, sets mode, and renames over path.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	return writeAtomicAs(path, data, mode, -1, -1)
}

// writeAtomicKeepOwner is writeAtomic for a file another service reads, such as telemt's
// config: the replacement keeps the owner of the file it replaces instead of becoming the
// agent's (root's), which with mode 0600 would lock the service out of its own file.
func writeAtomicKeepOwner(path string, data []byte, mode os.FileMode) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeAtomic(path, data, mode)
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return writeAtomic(path, data, mode)
	}
	return writeAtomicAs(path, data, mode, int(stat.Uid), int(stat.Gid))
}

// writeAtomicAs is writeAtomic with the temp file chowned before the rename, so path is never
// briefly owned by someone else. uid and gid of -1 leave the owner alone.
func writeAtomicAs(path string, data []byte, mode os.FileMode, uid, gid int) error {
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
	if uid != -1 || gid != -1 {
		if err := tmp.Chown(uid, gid); err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
			return fmt.Errorf("preserve owner of %s: %w", path, err)
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
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
