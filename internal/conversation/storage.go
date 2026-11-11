package conversation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/llm"
)

func Load(path string) (Snapshot, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "state.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	return Decode(data)
}

// Decode validates one versioned conversation snapshot.
func Decode(data []byte) (Snapshot, error) {
	var s Snapshot

	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || s.ContextStart < 0 || s.ContextStart > len(s.History) || (s.ContextStart > 0 && (s.ContextStart < 2 || s.Summary == "")) {
		return Snapshot{}, fmt.Errorf("invalid conversation snapshot")
	}
	for _, run := range s.Runs {
		if run.Start < 1 || run.End < run.Start || run.End > len(s.History) {
			return s, fmt.Errorf("invalid archived run range")
		}
	}
	if err := s.validateInspection(); err != nil {
		return Snapshot{}, err
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
