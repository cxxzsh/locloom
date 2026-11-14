package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AtomicWriteFile writes data to a temporary file in filepath.Dir(path), syncs, closes, and renames over path.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	dirPerm := os.FileMode(0o755)
	if perm&0o077 == 0 {
		dirPerm = 0o700
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create parent dir %q: %w", dir, err)
	}
	if perm&0o077 == 0 {
		// Tighten directory permissions if it already existed with broader access, ignoring errors if shared (e.g., TempDir).
		if base := filepath.Base(dir); base == AppDirName {
			_ = os.Chmod(dir, 0o700)
		}
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %q: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file %q: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		if runtime.GOOS == "windows" {
			backup := tmpName + ".bak"
			_ = os.Remove(backup)
			if bkErr := os.Rename(path, backup); bkErr == nil {
				if renErr := os.Rename(tmpName, path); renErr == nil {
					_ = os.Remove(backup)
					cleanup = false
					return nil
				} else {
					_ = os.Rename(backup, path)
					return fmt.Errorf("rename %q -> %q: %w", tmpName, path, renErr)
				}
			}
		}
		return fmt.Errorf("rename %q -> %q: %w", tmpName, path, err)
	}

	cleanup = false
	return nil
}
