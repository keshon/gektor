package edit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/workspace"
)

// previewMaxLines caps a preview: approval prompts share context with
// the work, so the evidence must stay small.
const previewMaxLines = 40

// Preview renders what a mutating call is about to do, for approval
// prompts. patch_file yields the unified diff the call would produce;
// write_file shows new-file size or a capped diff against the existing
// file; move_file shows the rename. Anything else (or unparseable args,
// missing files, UTF-16) returns "" — no preview beats a wrong one, and
// the gate still asks with the tool name and resource.
func Preview(ws *workspace.Workspace, tool string, args json.RawMessage) string {
	switch tool {
	case "patch_file":
		return previewPatch(ws, args)
	case "write_file":
		return previewWrite(ws, args)
	case "move_file":
		return previewMove(args)
	default:
		return ""
	}
}

func previewPatch(ws *workspace.Workspace, args json.RawMessage) string {
	var in struct {
		Path       string `json:"path"`
		OldContent string `json:"old_content"`
		NewContent string `json:"new_content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ""
	}
	if err := guardEdit("patch_file", in.OldContent, in.NewContent); err != nil {
		return ""
	}
	full, err := ws.Resolve(in.Path)
	if err != nil {
		return ""
	}
	data, err := ws.ReadFile(full)
	if err != nil {
		return ""
	}
	if isUTF16(data) {
		return ""
	}
	content, _ := normalizeContent(data)
	oldNorm, _ := normalizeContent([]byte(in.OldContent))
	newNorm, _ := normalizeContent([]byte(in.NewContent))
	if err := guardEdit("patch_file", oldNorm, newNorm); err != nil {
		return ""
	}
	count := strings.Count(content, oldNorm)
	if count == 0 {
		return "(old_content not found — this patch would fail)"
	}
	if count > 1 {
		return "(old_content matches multiple places — this patch would fail)"
	}
	newContent := strings.Replace(content, oldNorm, newNorm, 1)
	return capLines(unifiedDiff(in.Path, content, newContent), previewMaxLines)
}

func previewWrite(ws *workspace.Workspace, args json.RawMessage) string {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ""
	}
	full, err := ws.Resolve(in.Path)
	if err != nil {
		return ""
	}
	old, err := ws.ReadFile(full)
	if err != nil {
		if !os.IsNotExist(err) {
			return ""
		}
		n := len(strings.Split(in.Content, "\n"))
		return fmt.Sprintf("(new file, %d lines)", n)
	}
	if isUTF16(old) {
		return ""
	}
	oldText, _ := normalizeContent(old)
	newText, _ := normalizeContent([]byte(in.Content))
	if oldText == newText {
		return "(identical to the file on disk — this write changes nothing)"
	}
	return capLines(unifiedDiff(in.Path, oldText, newText), previewMaxLines)
}

func previewMove(args json.RawMessage) string {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ""
	}
	if in.From == "" || in.To == "" {
		return ""
	}
	return fmt.Sprintf("rename %s -> %s", in.From, in.To)
}

func capLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n") +
		fmt.Sprintf("\n... (%d more lines)", len(lines)-max)
}

// Evidence is operator-only. It must not be appended to model history.
// Its snapshot covers the source (and rename destination), including absence.
type Evidence struct {
	Text     string
	versions map[string]string
}

func approvalVersion(ws *workspace.Workspace, path string) (string, []byte, error) {
	full, err := ws.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	data, err := ws.ReadFile(full)
	if os.IsNotExist(err) {
		return "absent", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), data, nil
}

func (e Evidence) Current(ws *workspace.Workspace) bool {
	for path, version := range e.versions {
		current, _, err := approvalVersion(ws, path)
		if err != nil || current != version {
			return false
		}
	}
	return true
}

// Proposal builds the complete diff from the same bytes it fingerprints.
// Unsupported or unreadable proposals retain the ordinary bounded gate prompt.
func Proposal(ws *workspace.Workspace, tool string, args json.RawMessage) Evidence {
	e := Evidence{versions: map[string]string{}}
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Old     string `json:"old_content"`
		New     string `json:"new_content"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if json.Unmarshal(args, &in) != nil {
		return Evidence{}
	}
	read := func(path string) (string, string, bool) {
		version, data, err := approvalVersion(ws, path)
		if err != nil || isUTF16(data) {
			return "", "", false
		}
		e.versions[path] = version
		text, _ := normalizeContent(data)
		return text, version, true
	}
	switch tool {
	case "patch_file", "write_file":
		before, version, ok := read(in.Path)
		if !ok {
			return Evidence{}
		}
		after, _ := normalizeContent([]byte(in.Content))
		if tool == "patch_file" {
			old, _ := normalizeContent([]byte(in.Old))
			newText, _ := normalizeContent([]byte(in.New))
			if version == "absent" || guardEdit(tool, old, newText) != nil || strings.Count(before, old) != 1 {
				return Evidence{}
			}
			after = strings.Replace(before, old, newText, 1)
		}
		e.Text = unifiedDiff(in.Path, before, after)
		if before == after {
			if version == "absent" {
				e.Text = fmt.Sprintf("(new empty file: %s)", in.Path)
			} else {
				e.Text = "(identical to the file on disk — this write changes nothing)"
			}
		}
	case "move_file":
		_, version, ok := read(in.From)
		if !ok || version == "absent" {
			return Evidence{}
		}
		if _, _, ok := read(in.To); !ok {
			return Evidence{}
		}
		e.Text = previewMove(args)
	default:
		return Evidence{}
	}
	return e
}
