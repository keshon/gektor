package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// PatchLines replaces lines [start_line, end_line] (1-indexed, inclusive)
// with new_content. Complements PatchFile for edits where the target text
// isn't unique enough to match by content (e.g. "fix line 42").
//
// Semantics a model can rely on:
//   - a single trailing "\n" in new_content is a line terminator, not an
//     extra blank line;
//   - empty new_content deletes the range;
//   - start_line == (file lines + 1) appends at the end of the file;
//   - the response states how many lines the edit shifted everything below.
type PatchLines struct{ WS *workspace.Workspace }

func (PatchLines) Name() string         { return "patch_lines" }
func (PatchLines) Mode() agent.ToolMode { return agent.Exclusive }
func (PatchLines) Description() string {
	return "Replace a range of lines (1-indexed, inclusive) in a file with new content. " +
		"Empty new_content deletes the range. " +
		"Line numbers below the edit shift if the line count changes; the response reports the shift, " +
		"so re-read the file or apply several edits bottom-up. " +
		"Use this when editing by line number is clearer than matching exact text with patch_file."
}
func (PatchLines) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"},
			"start_line": {"type": "integer"},
			"end_line": {"type": "integer"},
			"new_content": {"type": "string"}
		},
		"required": ["path", "start_line", "end_line", "new_content"]
	}`)
}

// splitReplacement splits new_content into lines. A single trailing "\n" is
// a terminator, not an extra empty line; empty content means "no lines".
func splitReplacement(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// sameSignificant reports whether two lines are equal ignoring surrounding
// whitespace, and are not blank.
func sameSignificant(a, b string) bool {
	a = strings.TrimSpace(a)
	return a != "" && a == strings.TrimSpace(b)
}

// overlapNote warns when the replacement repeats the line directly above or
// below the range. That is the typical sign of an off-by-one start_line /
// end_line: the old line stays and the new content duplicates it.
// start and end are 1-based and inclusive.
func overlapNote(lines, newSeg []string, start, end int) string {
	if len(newSeg) == 0 {
		return ""
	}
	var notes []string
	if start >= 2 && sameSignificant(newSeg[0], lines[start-2]) {
		notes = append(notes, fmt.Sprintf("the first new line repeats line %d, just above the range", start-1))
	}
	if end < len(lines) && sameSignificant(newSeg[len(newSeg)-1], lines[end]) {
		notes = append(notes, fmt.Sprintf("the last new line repeats line %d, just below the range", end+1))
	}
	if len(notes) == 0 {
		return ""
	}
	return "warning: " + strings.Join(notes, "; ") +
		" — if that is unintended, start_line/end_line is off by one; correct it with another patch_lines call"
}

func (t PatchLines) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path       string `json:"path"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		NewContent string `json:"new_content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "path", "start_line", "end_line", "new_content"); err != nil {
		return "", err
	}
	if err := guardWriteSize("patch_lines", in.NewContent); err != nil {
		return "", err
	}
	if in.StartLine < 1 || in.EndLine < in.StartLine {
		return "", fmt.Errorf("invalid range: start_line=%d end_line=%d", in.StartLine, in.EndLine)
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
		if err := refuseUTF16("patch_lines", in.Path, data); err != nil {
			return "", err
		}

		content, ending := normalizeContent(data)

		// A trailing "\n" terminates the last line; it does not start a new
		// (phantom) one. Strip it for editing and restore it on write.
		hasFinalNL := strings.HasSuffix(content, "\n")
		var lines []string
		if content != "" {
			lines = strings.Split(content, "\n")
			if hasFinalNL {
				lines = lines[:len(lines)-1]
			}
		}
		total := len(lines)

		// start_line == total+1 is allowed: it appends to the end of the file.
		if in.StartLine > total+1 {
			return "", fmt.Errorf("start_line %d is beyond end of file (%d lines)", in.StartLine, total)
		}
		end := min(in.EndLine, total)

		replacement, _ := normalizeContent([]byte(in.NewContent))
		newSeg := splitReplacement(replacement)
		oldSeg := lines[in.StartLine-1 : end]
		if slices.Equal(oldSeg, newSeg) {
			return "", fmt.Errorf("patch_lines: replacement is identical to lines %d-%d — nothing would change", in.StartLine, end)
		}

		result := make([]string, 0, total-len(oldSeg)+len(newSeg))
		result = append(result, lines[:in.StartLine-1]...)
		result = append(result, newSeg...)
		result = append(result, lines[end:]...)

		text := strings.Join(result, "\n")
		if hasFinalNL && len(result) > 0 {
			text += "\n"
		}
		if err := os.WriteFile(full, []byte(denormalize(text, ending)), 0o644); err != nil {
			return "", err
		}

		summary := fmt.Sprintf("replaced lines %d-%d with %d line(s); file now has %d lines",
			in.StartLine, end, len(newSeg), len(result))
		if delta := len(newSeg) - len(oldSeg); delta != 0 {
			summary += fmt.Sprintf("; line numbers below the edit shifted by %+d", delta)
		}

		out := "ok: " + summary + "\n" + unifiedDiff(in.Path, lines, in.StartLine-1, end, newSeg)
		if note := overlapNote(lines, newSeg, in.StartLine, end); note != "" {
			out += "\n" + note
		}
		if note := gofmtNote(full); note != "" {
			out += "\n" + note
		}
		return out, nil
	})
}
