package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// PatchFile replaces one exact occurrence of old_content with new_content
// inside an existing file. It exists so a model can make a small, surgical
// edit to a large file without rewriting the whole thing through
// write_file — which is exactly the kind of single huge generation that
// risks getting cut off (see Config.MaxTokens).
type PatchFile struct{ WS *workspace.Workspace }

func (PatchFile) Name() string         { return "patch_file" }
func (PatchFile) Mode() agent.ToolMode { return agent.Exclusive }
func (PatchFile) Description() string {
	return "Replace one exact occurrence of old_content with new_content in an existing file. " +
		"new_content replaces exactly the text of old_content, so do not repeat surrounding text " +
		"that is not part of old_content — it would be duplicated. " +
		"Use this for small edits instead of rewriting the whole file with write_file."
}
func (PatchFile) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"},
			"old_content": {"type": "string"},
			"new_content": {"type": "string"}
		},
		"required": ["path", "old_content", "new_content"]
	}`)
}

func (t PatchFile) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path       string `json:"path"`
		OldContent string `json:"old_content"`
		NewContent string `json:"new_content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "path", "old_content", "new_content"); err != nil {
		return "", err
	}
	if err := guardEdit("patch_file", in.OldContent, in.NewContent); err != nil {
		return "", err
	}

	full, err := t.WS.Resolve(in.Path)
	if err != nil {
		return "", err
	}
	return withPathLock(in.Path, func() (string, error) {
		data, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		if err := refuseUTF16("patch_file", in.Path, data); err != nil {
			return "", err
		}

		content, ending := normalizeContent(data)
		oldNorm, _ := normalizeContent([]byte(in.OldContent))
		count := strings.Count(content, oldNorm)
		if count == 0 {
			return "", fmt.Errorf("old_content not found in %s", in.Path)
		}
		if count > 1 {
			return "", fmt.Errorf("old_content appears %d times in %s — make it unique before patching", count, in.Path)
		}

		newNorm, _ := normalizeContent([]byte(in.NewContent))
		start := strings.Index(content, oldNorm)
		newContent := content[:start] + newNorm + content[start+len(oldNorm):]
		if err := os.WriteFile(full, []byte(denormalize(newContent, ending)), 0o644); err != nil {
			return "", err
		}

		// Diff over whole lines on both sides. The old side comes from the
		// original file, the new side from the file as actually written, so a
		// mid-line edit shows up as a one-line change (not "line removed,
		// fragment added") and CRLF input does not leak "\r" into the diff.
		oldLines := strings.Split(content, "\n")
		newLines := strings.Split(newContent, "\n")
		oldBreaks := strings.Count(oldNorm, "\n")
		newBreaks := strings.Count(newNorm, "\n")

		startLine := strings.Count(content[:start], "\n")
		endLine := min(startLine+oldBreaks+1, len(oldLines))
		newEnd := min(startLine+newBreaks+1, len(newLines))

		summary := fmt.Sprintf("edited lines %d-%d", startLine+1, endLine)
		if delta := newBreaks - oldBreaks; delta != 0 {
			summary += fmt.Sprintf("; line numbers below the edit shifted by %+d", delta)
		}

		out := "ok: " + summary + "\n" + unifiedDiff(in.Path, oldLines, startLine, endLine, newLines[startLine:newEnd])
		if note := gofmtNote(full); note != "" {
			out += "\n" + note
		}
		return out, nil
	})
}
