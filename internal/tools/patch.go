package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
	return "Replace one exact, unique old_content occurrence with new_content in an existing file. Copy old_content from read_file with enough context to match uniquely. new_content replaces only that span; repeating outside context duplicates it. Prefer for small edits."
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
		data, err := t.WS.ReadFile(full)
		if err != nil {
			return "", err
		}
		if err := refuseUTF16("patch_file", in.Path, data); err != nil {
			return "", err
		}

		content, ending := normalizeContent(data)
		oldNorm, _ := normalizeContent([]byte(in.OldContent))
		newNorm, _ := normalizeContent([]byte(in.NewContent))
		if err := guardEdit("patch_file", oldNorm, newNorm); err != nil {
			return "", err
		}
		count := strings.Count(content, oldNorm)
		if count == 0 {
			return "", fmt.Errorf("old_content not found in %s; the file was not changed. Read the current file and copy an exact block, including whitespace", in.Path)
		}
		if count > 1 {
			var lines []string
			for pos := 0; len(lines) < 5; {
				at := strings.Index(content[pos:], oldNorm)
				if at < 0 {
					break
				}
				at += pos
				lines = append(lines, fmt.Sprint(1+strings.Count(content[:at], "\n")))
				pos = at + len(oldNorm)
			}
			return "", fmt.Errorf("old_content appears %d times in %s at starting lines %s (up to 5 shown); file unchanged. Read around the intended match with offset/limit and include enough surrounding text to make old_content unique", count, in.Path, strings.Join(lines, ", "))
		}

		start := strings.Index(content, oldNorm)
		newContent := content[:start] + newNorm + content[start+len(oldNorm):]
		encoded := denormalize(newContent, ending)
		if strings.HasPrefix(string(data), "\xef\xbb\xbf") {
			encoded = "\xef\xbb\xbf" + encoded
		}
		if err := t.WS.WriteFile(full, []byte(encoded), 0o644); err != nil {
			return "", err
		}

		oldBreaks := strings.Count(oldNorm, "\n")
		newBreaks := strings.Count(newNorm, "\n")

		startLine := strings.Count(content[:start], "\n")
		endLine := startLine + oldBreaks + 1
		if strings.HasSuffix(oldNorm, "\n") {
			endLine--
		}

		summary := fmt.Sprintf("edited lines %d-%d", startLine+1, endLine)
		if delta := newBreaks - oldBreaks; delta != 0 {
			summary += fmt.Sprintf("; line numbers below the edit shifted by %+d", delta)
		}

		out := "ok: " + summary + "\n" + unifiedDiff(in.Path, content, newContent)
		if note := gofmtNote(t.WS, full); note != "" {
			out += "\n" + note
		}
		return out, nil
	})
}
