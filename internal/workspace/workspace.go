// Package workspace confines built-in file access to an os.Root directory.
// Shell, Git and external MCP processes still run with the user's permissions;
// this file boundary does not provide process or network isolation.
package workspace

import (
	"fmt"
	"io"
	"io/fs"
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

// Discover only metadata at workspace creation; full guides remain explicit
// read_file calls so the operator can see what the agent actually consulted.
func (w *Workspace) skillCatalog() string {
	paths, _ := fs.Glob(w.FS(), "skills/*/SKILL.md")
	if len(paths) == 0 {
		return "\n\nNo workspace skills found at skills/*/SKILL.md."
	}
	var b strings.Builder
	b.WriteString("\n\nAvailable workspace skills (catalog only, not loaded). Read the relevant SKILL.md with read_file before applying it; briefly name the skill you use:\n")
	for i, path := range paths {
		if i == 32 {
			b.WriteString("More skills exist; use list_files on skills/.\n")
			break
		}
		rel := filepath.FromSlash(path)
		fmt.Fprintf(&b, "- %s", filepath.ToSlash(rel))
		f, err := w.Open(path)
		if err == nil {
			data, _ := io.ReadAll(io.LimitReader(f, 4096))
			f.Close()
			text := strings.ReplaceAll(string(data), "\r\n", "\n")
			if strings.HasPrefix(text, "---\n") {
				lines := strings.Split(strings.TrimPrefix(text, "---\n"), "\n")
				for n, line := range lines {
					if line == "---" {
						break
					}
					if value, ok := strings.CutPrefix(line, "description:"); ok {
						value = strings.TrimSpace(value)
						if strings.HasPrefix(value, ">") || strings.HasPrefix(value, "|") {
							var parts []string
							for _, next := range lines[n+1:] {
								if next != "" && next[0] != ' ' && next[0] != '\t' {
									break
								}
								parts = append(parts, strings.TrimSpace(next))
							}
							value = strings.Join(parts, " ")
						}
						description := []rune(strings.Trim(strings.TrimSpace(value), "\"'"))
						if len(description) > 220 {
							description = append(description[:220], '…')
						}
						fmt.Fprintf(&b, ": %s", string(description))
						break
					}
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
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
