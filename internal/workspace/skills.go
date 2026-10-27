package workspace

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const skillGuideLimit = 256 * 1024
const skillMetadataLimit = 16 * 1024

// Skill contains routing metadata; the guide is read only through LoadSkill.
type Skill struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	Description    string `json:"description,omitempty"`
	BaseDir        string `json:"base_dir"`
	Source         string `json:"source"`
	root, relative string
	project        bool
}

func DefaultSkillRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".gektor", "skills")}
}

// Skills discovers roots recursively without following directory symlinks.
// Later roots override earlier ones; malformed guides fail visibly.
func (w *Workspace) Skills() ([]Skill, error) {
	type rootSpec struct {
		root, prefix, source string
		project              bool
	}
	roots := []rootSpec{}
	for _, root := range w.skillRoots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		roots = append(roots, rootSpec{root: abs, prefix: ".", source: abs})
	}
	for _, prefix := range []string{"skills", ".agents/skills", ".gektor/skills"} {
		roots = append(roots, rootSpec{root: w.root, prefix: prefix, source: "project:" + prefix, project: true})
	}
	byID := map[string]Skill{}
	for _, spec := range roots {
		var rooted *os.Root
		if spec.project {
			rooted = w.files
		} else {
			var err error
			rooted, err = os.OpenRoot(spec.root)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		found := map[string]Skill{}
		err := fs.WalkDir(rooted.FS(), spec.prefix, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				if name == spec.prefix && errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", ".gektor":
					if name != spec.prefix {
						return fs.SkipDir
					}
				}
				return nil
			}
			if entry.Name() != "SKILL.md" || !entry.Type().IsRegular() {
				return nil
			}
			file, err := rooted.Open(name)
			if err != nil {
				return err
			}
			meta, err := readSkillMetadata(file)
			file.Close()
			if err != nil {
				return fmt.Errorf("skill %s: %w", filepath.Join(spec.root, filepath.FromSlash(name)), err)
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(name, spec.prefix), "/")
			if spec.prefix == "." {
				rel = name
			}
			id := meta.Name
			if id == "" {
				id = strings.TrimSuffix(rel, "/SKILL.md")
				if id == "SKILL.md" {
					id = filepath.Base(spec.root)
				}
			}
			if _, exists := found[id]; exists {
				return fmt.Errorf("duplicate skill ID %q in %s", id, spec.source)
			}
			display := filepath.Join(spec.root, filepath.FromSlash(name))
			if spec.project {
				display = path.Clean(name)
			}
			found[id] = Skill{ID: id, Name: id, Path: display, Description: meta.Description, BaseDir: filepath.Dir(filepath.Join(spec.root, filepath.FromSlash(name))), Source: spec.source, root: spec.root, relative: name, project: spec.project}
			return nil
		})
		if !spec.project {
			rooted.Close()
		}
		if err != nil {
			return nil, err
		}
		for id, skill := range found {
			byID[id] = skill
		}
	}
	entries := make([]Skill, 0, len(byID))
	for _, skill := range byID {
		entries = append(entries, skill)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

type skillMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func readSkillMetadata(reader io.Reader) (skillMetadata, error) {
	var meta skillMetadata
	scanner := bufio.NewScanner(io.LimitReader(reader, skillMetadataLimit+1))
	scanner.Buffer(make([]byte, 1024), skillMetadataLimit+1)
	if !scanner.Scan() {
		return meta, scanner.Err()
	}
	if strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff")) != "---" {
		return meta, nil
	}
	var front strings.Builder
	closed := false
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			closed = true
			break
		}
		front.WriteString(scanner.Text())
		front.WriteByte('\n')
		if front.Len() > skillMetadataLimit {
			return meta, fmt.Errorf("frontmatter exceeds 16 KiB")
		}
	}
	if err := scanner.Err(); err != nil {
		return meta, err
	}
	if !closed {
		return meta, fmt.Errorf("unterminated or oversized skill frontmatter")
	}
	if err := yaml.Unmarshal([]byte(front.String()), &meta); err != nil {
		return meta, fmt.Errorf("frontmatter: %w", err)
	}
	if meta.Name != "" && (len(meta.Name) > 64 || !skillName.MatchString(meta.Name)) {
		return meta, fmt.Errorf("invalid skill name %q", meta.Name)
	}
	meta.Description = strings.TrimSpace(meta.Description)
	if len([]rune(meta.Description)) > 1024 {
		return meta, fmt.Errorf("skill description exceeds 1024 characters")
	}
	return meta, nil
}

// LoadSkill confines access to a discovered library root, including symlinks.
func (w *Workspace) LoadSkill(id string) (Skill, string, string, error) {
	entries, err := w.Skills()
	if err != nil {
		return Skill{}, "", "", err
	}
	for _, skill := range entries {
		if skill.ID != id {
			continue
		}
		data, err := w.readSkillResource(skill, "SKILL.md")
		if err != nil {
			return skill, "", "", err
		}
		return skill, string(data), fmt.Sprintf("%x", sha256.Sum256(data)), nil
	}
	return Skill{}, "", "", fmt.Errorf("unknown skill %q; discover it with system_info group=skills", id)
}

func (w *Workspace) LoadSkillResource(id, resource string) (Skill, string, error) {
	entries, err := w.Skills()
	if err != nil {
		return Skill{}, "", err
	}
	for _, skill := range entries {
		if skill.ID == id {
			data, err := w.readSkillResource(skill, resource)
			return skill, string(data), err
		}
	}
	return Skill{}, "", fmt.Errorf("unknown skill %q", id)
}

func (w *Workspace) readSkillResource(skill Skill, resource string) ([]byte, error) {
	if !filepath.IsLocal(resource) {
		return nil, fmt.Errorf("skill resource must remain within its package")
	}
	root := w.files
	if !skill.project {
		var err error
		root, err = os.OpenRoot(skill.root)
		if err != nil {
			return nil, err
		}
		defer root.Close()
	}
	pack, err := root.OpenRoot(filepath.Dir(filepath.FromSlash(skill.relative)))
	if err != nil {
		return nil, err
	}
	defer pack.Close()
	file, err := pack.Open(resource)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, skillGuideLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > skillGuideLimit {
		return nil, fmt.Errorf("skill resource exceeds 256 KiB")
	}
	return data, nil
}

func (w *Workspace) skillCatalog() string {
	entries, err := w.Skills()
	if err != nil {
		return fmt.Sprintf("\n\nWorkspace skill discovery incomplete: %v. Query system_info group=skills after resolving the error.", err)
	}
	if len(entries) == 0 {
		return "\n\nNo workspace skills found. Query system_info group=skills after adding packages."
	}
	header := "\n\nSkills (ID: summary). Search full metadata with system_info group=skills; load instructions with load_skill(id):\n"
	var b strings.Builder
	for _, skill := range entries {
		fmt.Fprintf(&b, "- %s", skill.ID)
		if skill.Description != "" {
			description := []rune(strings.Join(strings.Fields(skill.Description), " "))
			if len(description) > 120 {
				description = append(description[:117], '.', '.', '.')
			}
			fmt.Fprintf(&b, ": %s", string(description))
		}
		b.WriteByte('\n')
	}
	if len(header)+b.Len() > 2048 {
		return fmt.Sprintf("\n\n%d workspace skills discovered. Search metadata with system_info group=skills (filter, offset, limit); then load_skill by ID. Full guides are not loaded.", len(entries))
	}
	return header + b.String()
}
