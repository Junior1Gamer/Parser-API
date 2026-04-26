package mfire

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically: a temp file is created in
// the same directory, written, and then renamed into place. This prevents
// partial/corrupt files if the process is killed mid-write (e.g. job timeout).
// The directory is created if it does not exist.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "tmp.*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		os.Remove(tmpName)
		tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename tmp -> %s: %w", path, err)
	}
	if perm != 0 {
		os.Chmod(path, perm)
	}
	return nil
}
