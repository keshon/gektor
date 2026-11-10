package conversation

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveWorkspace never interprets missing or relative archive metadata as cwd.
func (s Snapshot) ResolveWorkspace() (string, error) {
	if s.Workspace == "" {
		return "", fmt.Errorf("workspace is unassigned; select a workspace explicitly")
	}
	if !filepath.IsAbs(s.Workspace) {
		return "", fmt.Errorf("workspace %q is not absolute; select an existing workspace", s.Workspace)
	}
	info, err := os.Stat(s.Workspace)
	if err != nil {
		return "", fmt.Errorf("workspace %q unavailable; restore the directory or select another workspace: %w", s.Workspace, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory; select another workspace", s.Workspace)
	}
	return filepath.Clean(s.Workspace), nil
}
