package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Dir is the directory holding the account files.
type Dir string

// IsAccountFile reports whether a file name is one the core manages. Dotfiles
// cover the temporary files WriteAtomic leaves for a moment.
func IsAccountFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}

// List returns the account file paths in sorted order.
func (d Dir) List() ([]string, error) {
	entries, err := os.ReadDir(string(d))
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var paths []string
	for _, e := range entries {
		if e.Type().IsRegular() && IsAccountFile(e.Name()) {
			paths = append(paths, filepath.Join(string(d), e.Name()))
		}
	}
	slices.Sort(paths)
	return paths, nil
}

// WriteAtomic replaces path with data so a reader, or a crash, sees either the
// old file or the new one. The data is on disk before the rename, and the
// rename is on disk before this returns.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
