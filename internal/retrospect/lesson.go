package retrospect

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

// Lesson identifies the exact guide approved by the user. Undo never removes
// a subsequently edited guide or an already existing lesson.
type Lesson struct {
	Path    string
	SHA256  string
	Created bool
}

// Approve publishes one user-reviewed procedure through ordinary skill discovery.
// Evidence is pinned beside the guide before the guide becomes discoverable.
func Approve(ctx context.Context, root, stateFile string, result Result, when, procedure string) (Lesson, error) {
	if err := ctx.Err(); err != nil {
		return Lesson{}, err
	}
	when, procedure = strings.TrimSpace(when), strings.TrimSpace(procedure)
	if when == "" || procedure == "" || len(when) > 300 || len(procedure) > 1000 {
		return Lesson{}, fmt.Errorf("use 1–300 bytes for When and 1–1000 bytes for Procedure")
	}
	if len(result.Memory) != 1 {
		return Lesson{}, fmt.Errorf("choose one proposed lesson")
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return Lesson{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != result.SourceSHA256 {
		return Lesson{}, fmt.Errorf("session changed; run /learn again before saving")
	}
	snapshot, err := session.Decode(data)
	if err != nil {
		return Lesson{}, err
	}
	memory := result.Memory[0]
	evidence := make(map[int]string)
	confirmed := false
	for _, id := range memory.Evidence {
		if id < result.Start || id > result.End || id < 1 || id > len(snapshot.History) {
			return Lesson{}, fmt.Errorf("invalid evidence message %d", id)
		}
		message := snapshot.History[id-1]
		if message.Role == llm.RoleTool && message.ToolFailed != nil && !*message.ToolFailed && len([]rune(strings.TrimSpace(memory.Quote))) >= 8 && strings.Contains(message.Content, memory.Quote) {
			confirmed = true
			evidence[id] = memory.Quote
		}
	}
	if !confirmed {
		return Lesson{}, fmt.Errorf("lesson lacks successful quoted evidence")
	}
	// Identity excludes evidence IDs so the same approved text from another run
	// does not accumulate duplicate skills. Existing files are never overwritten.
	identity, _ := json.Marshal([]string{when, procedure})
	id := fmt.Sprintf("learned-%x", sha256.Sum256(identity))[:24]
	dir := filepath.Join("skills", id)
	path := filepath.Join(dir, "SKILL.md")
	description, _ := json.Marshal(strings.Join(strings.Fields(when), " "))
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# Repository lesson\n\n## When\n\n%s\n\n## Procedure\n\n%s\n\nApproved by the user from historical evidence, not independently verified. Check current files before applying; if the condition no longer holds, ignore this lesson. This guide grants no tool permissions.\n", id, description, when, procedure)
	guide := []byte(body + fmt.Sprintf("\nSource: session %s, messages %v, archive SHA-256 %s. Original evidence and proposal: evidence.json.\n", filepath.Base(filepath.Dir(stateFile)), memory.Evidence, result.SourceSHA256))
	files, err := os.OpenRoot(root)
	if err != nil {
		return Lesson{}, err
	}
	defer files.Close()
	if info, err := files.Lstat("skills"); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return Lesson{}, fmt.Errorf("skills directory is a link; choose a workspace with a local skills directory")
	}
	if err := files.MkdirAll("skills", 0755); err != nil {
		return Lesson{}, err
	}
	if err := files.Mkdir(dir, 0755); err != nil {
		if !os.IsExist(err) {
			return Lesson{}, err
		}
		old, readErr := files.ReadFile(path)
		retired := filepath.Join(dir, "SKILL.retired.md")
		reactivate := os.IsNotExist(readErr)
		if reactivate {
			old, readErr = files.ReadFile(retired)
		}
		if readErr != nil {
			return Lesson{}, fmt.Errorf("lesson directory already exists; inspect %s before saving: %w", dir, readErr)
		}
		// Match the approved text, preserving the first observation's evidence.
		if !strings.HasPrefix(string(old), body+"\nSource: session ") {
			return Lesson{}, fmt.Errorf("lesson was edited; existing %s was preserved", path)
		}
		if reactivate {
			if err := ctx.Err(); err != nil {
				return Lesson{}, err
			}
			if err := files.Rename(retired, path); err != nil {
				return Lesson{}, err
			}
		}
		return Lesson{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(old)), Created: reactivate}, nil
	}
	published := false
	defer func() {
		if !published {
			files.Remove(filepath.Join(dir, "SKILL.pending"))
			files.Remove(filepath.Join(dir, "evidence.json"))
			files.Remove(dir)
		}
	}()
	pinned, err := json.MarshalIndent(struct {
		SourceSHA256           string         `json:"source_sha256"`
		Proposal               Memory         `json:"proposal"`
		SuccessfulToolExcerpts map[int]string `json:"successful_tool_excerpts"`
	}{result.SourceSHA256, memory, evidence}, "", "  ")
	if err != nil {
		return Lesson{}, err
	}
	if err := files.WriteFile(filepath.Join(dir, "evidence.json"), pinned, 0600); err != nil {
		return Lesson{}, err
	}
	if err := files.WriteFile(filepath.Join(dir, "SKILL.pending"), guide, 0600); err != nil {
		return Lesson{}, err
	}
	if err := ctx.Err(); err != nil {
		return Lesson{}, err
	}
	if err := files.Rename(filepath.Join(dir, "SKILL.pending"), path); err != nil {
		return Lesson{}, err
	}
	published = true
	return Lesson{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(guide)), Created: true}, nil
}

// Undo removes a newly approved lesson from discovery, retaining its evidence.
func Undo(root string, lesson Lesson) error {
	if !lesson.Created {
		return fmt.Errorf("no newly saved lesson to undo")
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer files.Close()
	data, err := files.ReadFile(lesson.Path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != lesson.SHA256 {
		return fmt.Errorf("lesson was edited; preserved %s", lesson.Path)
	}
	return files.Rename(lesson.Path, filepath.Join(filepath.Dir(lesson.Path), "SKILL.retired.md"))
}
