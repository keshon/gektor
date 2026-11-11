// Package workspace confines built-in file access to an os.Root directory.
// Shell, Git and external MCP processes still run with the user's permissions;
// this file boundary does not provide process or network isolation.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

type Workspace struct {
	files                 *os.Root
	root                  string
	instructions          string
	skillMetadata         string
	skillRoots            []string
	skillDescriptionLimit int
}

func New(root string) (*Workspace, error) {
	return NewWithSkillRoots(root, nil)
}

// NewWithSkillRoots adds library roots lowest priority first; project skills win.
func NewWithSkillRoots(root string, skillRoots []string) (*Workspace, error) {
	descriptionLimit, err := skillDescriptionLimitFromEnv()
	if err != nil {
		return nil, err
	}
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
	w := &Workspace{skillDescriptionLimit: descriptionLimit, root: abs, files: files, skillRoots: append([]string(nil), skillRoots...)}
	if err := w.loadInstructions(); err != nil {
		return nil, err
	}
	w.skillMetadata = w.skillCatalog()
	w.instructions += w.skillMetadata
	ready = true
	return w, nil
}

func (w *Workspace) Root() string { return w.root }
