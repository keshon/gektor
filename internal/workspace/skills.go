package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// Skill is discoverable metadata, not an automatically loaded instruction.
type Skill struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// Skills discovers nested packages without following directory symlinks or
// reading entire guides. Callers may query again after installing a package.
func (w *Workspace) Skills() ([]Skill, error) {
	entries := []Skill{}
	err := fs.WalkDir(w.FS(), "skills", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == "skills" && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", ".gektor":
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "SKILL.md" || !entry.Type().IsRegular() {
			return nil
		}
		f, err := w.Open(name)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, 4096))
		f.Close()
		if err != nil {
			return err
		}
		entries = append(entries, Skill{Path: path.Clean(name), Description: skillDescription(string(data))})
		return nil
	})
	return entries, err
}

func skillDescription(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return ""
	}
	lines := strings.Split(strings.TrimPrefix(text, "---\n"), "\n")
	for n, line := range lines {
		if line == "---" {
			break
		}
		value, ok := strings.CutPrefix(line, "description:")
		if !ok {
			continue
		}
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
		return string(description)
	}
	return ""
}

func (w *Workspace) skillCatalog() string {
	entries, err := w.Skills()
	if err != nil {
		return fmt.Sprintf("\n\nWorkspace skill discovery incomplete: %v. Query system_info group=skills after resolving the error.", err)
	}
	if len(entries) == 0 {
		return "\n\nNo workspace skills found under skills/. Query system_info group=skills after adding packages."
	}
	header := "\n\nAvailable workspace skills (metadata only). Read the relevant SKILL.md before applying it; briefly name the skill you use. Search all nested packages with system_info group=skills, filter, offset and limit:\n"
	var b strings.Builder
	for _, skill := range entries {
		fmt.Fprintf(&b, "- %s", skill.Path)
		if skill.Description != "" {
			fmt.Fprintf(&b, ": %s", skill.Description)
		}
		b.WriteByte('\n')
	}
	// Keep the cached prefix bounded without hiding an arbitrary first N skills.
	if b.Len() > 8192 {
		return fmt.Sprintf("\n\n%d workspace skills discovered recursively under skills/. Search metadata with system_info group=skills (filter, offset, limit); then read the matching SKILL.md. Full guides are not loaded.", len(entries))
	}
	return header + b.String()
}
