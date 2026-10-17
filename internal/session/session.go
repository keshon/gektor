// Package session owns the one file a run persists: the conversation
// snapshot at <dir>/state.json, rewritten atomically on every step.
//
// There is deliberately no second copy. An append-only log of full
// snapshots sat beside it for a while, read only as a fallback when the
// snapshot failed to decode -- but the snapshot is written tmp+rename, so
// a crash cannot leave it half-written, and anything that could take both
// files down takes the backup with them. It cost a file per session and
// grew without bound: 6 MB for a session that resumes from 46 KB.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/llm"
)

// WriteSnapshot stores history at path, creating its directory. Writes
// are atomic (tmp+rename) so an interrupted save leaves the previous
// snapshot whole rather than a truncated one.
func WriteSnapshot(path string, history []llm.Message) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ReadHistory loads a conversation snapshot. path may be the snapshot
// itself or the session directory containing it.
func ReadHistory(path string) ([]llm.Message, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "state.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var history []llm.Message
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return history, nil
}

// TitleFor derives the default display title: the first user message,
// first line, trimmed, capped at 60 runes. Loop nudges ([harness]
// provenance) are history, not conversation, and never title.
// Shapes locally instead of sharing workspace.TitleLine:
// package-layers forbids session from importing workspace, and the
// twin is eight lines, not a seam.
func TitleFor(history []llm.Message) string {
	for _, msg := range history {
		if msg.Role != llm.RoleUser || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if strings.HasPrefix(msg.Content, "[harness] ") {
			continue
		}
		line := msg.Content
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if r := []rune(line); len(r) > 60 {
			line = string(r[:60])
		}
		return line
	}
	return ""
}