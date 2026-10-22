package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Relative accepts both workspace-relative paths and absolute paths inside it.
// Actual containment, including symlinks and junctions, is enforced by os.Root
// at the filesystem operation, never by a check followed by an ordinary open.
func (w *Workspace) Relative(name string) (string, error) {
	if filepath.IsAbs(name) {
		var err error
		name, err = filepath.Rel(w.root, name)
		if err != nil {
			return "", fmt.Errorf("path is outside workspace %q: %w", w.root, err)
		}
	}
	if name == "" {
		name = "."
	}
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("path %q escapes workspace root %q; use a path inside this workspace", name, w.root)
	}
	return filepath.Clean(name), nil
}

// Resolve supplies a display/subprocess path. It does not grant file access:
// callers reading or writing workspace content must use the rooted methods.
func (w *Workspace) Resolve(name string) (string, error) {
	rel, err := w.Relative(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(w.root, rel), nil
}

func (w *Workspace) Close() error { return w.files.Close() }
func (w *Workspace) FS() fs.FS    { return w.files.FS() }

func (w *Workspace) Open(name string) (*os.File, error) {
	rel, err := w.Relative(name)
	if err != nil {
		return nil, err
	}
	return w.files.Open(rel)
}
func (w *Workspace) Stat(name string) (os.FileInfo, error) {
	rel, err := w.Relative(name)
	if err != nil {
		return nil, err
	}
	return w.files.Stat(rel)
}
func (w *Workspace) ReadFile(name string) ([]byte, error) {
	rel, err := w.Relative(name)
	if err != nil {
		return nil, err
	}
	return w.files.ReadFile(rel)
}
func (w *Workspace) ReadDir(name string) ([]os.DirEntry, error) {
	rel, err := w.Relative(name)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(w.files.FS(), filepath.ToSlash(rel))
}
func (w *Workspace) WriteFile(name string, data []byte, mode os.FileMode) error {
	rel, err := w.Relative(name)
	if err != nil {
		return err
	}
	return w.files.WriteFile(rel, data, mode)
}
func (w *Workspace) MkdirAll(name string, mode os.FileMode) error {
	rel, err := w.Relative(name)
	if err != nil {
		return err
	}
	return w.files.MkdirAll(rel, mode)
}
func (w *Workspace) Rename(from, to string) error {
	src, err := w.Relative(from)
	if err != nil {
		return err
	}
	dst, err := w.Relative(to)
	if err != nil {
		return err
	}
	return w.files.Rename(src, dst)
}
func (w *Workspace) Remove(name string) error {
	rel, err := w.Relative(name)
	if err != nil {
		return err
	}
	return w.files.Remove(rel)
}
