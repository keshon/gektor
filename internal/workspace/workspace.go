// Package workspace confines built-in file access to an os.Root directory.
// Shell, Git and external MCP processes still run with the user's permissions;
// this file boundary does not provide process or network isolation.
package workspace

import (
	"fmt"
	"io"

	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	files        *os.Root
	root         string
	instructions string
}

func New(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	files, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			files.Close()
		}
	}()
	w := &Workspace{root: abs, files: files}
	var dirs []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == filepath.Dir(dir) {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		path := filepath.Join(dirs[i], "AGENTS.md")
		var f *os.File
		var err error
		if dirs[i] == abs {
			f, err = w.Open("AGENTS.md")
		} else {
			f, err = os.Open(path)
		}
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > 64*1024 {
			return nil, fmt.Errorf("instructions exceed 64 KiB: %s", path)
		}
		w.instructions += "\n\nProject instructions (" + path + "):\n" + string(data)
	}
	w.instructions += w.skillCatalog()
	ready = true
	return w, nil
}

// ScopedInstructions loads only descendant AGENTS.md files on the target's
// directory chain. Every block names its scope; root rules are already loaded.
func (w *Workspace) ScopedInstructions(rel string) (string, error) {
	full, err := w.Resolve(rel)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(full)
	if info, err := w.Stat(full); err == nil && info.IsDir() {
		dir = full
	}
	var dirs []string
	for dir != w.root && strings.HasPrefix(dir, w.root+string(filepath.Separator)) {
		dirs = append(dirs, dir)
		dir = filepath.Dir(dir)
	}
	var b strings.Builder
	for i := len(dirs) - 1; i >= 0; i-- {
		path := filepath.Join(dirs[i], "AGENTS.md")
		f, err := w.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
		f.Close()
		if err != nil {
			return "", err
		}
		if len(data) > 64*1024 || b.Len()+len(data) > 64*1024 {
			return "", fmt.Errorf("scoped instructions exceed 64 KiB: %s", path)
		}
		scope, _ := filepath.Rel(w.root, dirs[i])
		fmt.Fprintf(&b, "Instructions for %s/ and its descendants only (%s):\n%s\n", filepath.ToSlash(scope), filepath.ToSlash(filepath.Join(scope, "AGENTS.md")), data)
	}
	return b.String(), nil
}

func (w *Workspace) Root() string { return w.root }

// Instructions returns ancestor and root AGENTS.md files, outermost first.
func (w *Workspace) Instructions() string {
	if w == nil {
		return ""
	}
	return w.instructions
}
