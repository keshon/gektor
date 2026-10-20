// Package session owns durable conversation history and its compacted model view.
// A single snapshot stores each original message once; context is a retained tail
// plus a summary. Old array snapshots remain readable.
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/llm"
)

// Snapshot keeps the archive independent of the model's working context.
// ContextStart is an index into History; zero means the entire conversation.
type Snapshot struct {
	Version      int           `json:"version"`
	History      []llm.Message `json:"history"`
	ContextStart int           `json:"context_start,omitempty"`
	Summary      string        `json:"summary,omitempty"`
}

func New(history []llm.Message) Snapshot {
	return Snapshot{Version: 1, History: append([]llm.Message(nil), history...)}
}

func (s Snapshot) Context() []llm.Message {
	if s.ContextStart == 0 {
		return append([]llm.Message(nil), s.History...)
	}
	out := append([]llm.Message(nil), s.History[:2]...)
	out = append(out, llm.Message{Role: llm.RoleUser, Content: s.Summary})
	return append(out, s.History[s.ContextStart:]...)
}

// Compact records the projection produced by the agent's compactor. Its tail
// is a suffix of the current context and therefore of the archive.
func (s *Snapshot) Compact(context []llm.Message) {
	s.ContextStart = len(s.History) - len(context[3:])
	s.Summary = context[2].Content
}

func Load(path string) (Snapshot, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "state.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		var history []llm.Message
		if err := json.Unmarshal(data, &history); err != nil {
			return s, err
		}
		return New(history), nil
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || s.ContextStart < 0 || s.ContextStart > len(s.History) || (s.ContextStart > 0 && (s.ContextStart < 2 || s.Summary == "")) {
		return Snapshot{}, fmt.Errorf("invalid conversation snapshot")
	}
	return s, nil
}

// Save replaces the snapshot only after the complete candidate is written.
func (s Snapshot) Save(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// WriteSnapshot starts or explicitly replaces a complete conversation.
func WriteSnapshot(path string, history []llm.Message) error { return New(history).Save(path) }

// ReadHistory returns the complete archive for display, search and export.
func ReadHistory(path string) ([]llm.Message, error) {
	s, err := Load(path)
	return s.History, err
}

// ReadContext returns the model projection for continuation and budgeting.
func ReadContext(path string) ([]llm.Message, error) {
	s, err := Load(path)
	if err != nil {
		return nil, err
	}
	return s.Context(), nil
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
