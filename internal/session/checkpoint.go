package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/llm"
)

// ReadSnapshot reads only the JSON snapshot. Call ReadHistory when recovery
// from the adjacent JSONL log is appropriate.
func ReadSnapshot(path string) ([]llm.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	var history []llm.Message
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	return history, nil
}

// ReadHistory accepts a snapshot, session directory, or explicit JSONL log.
// A valid snapshot remains authoritative. Missing or corrupt snapshots fall
// back to the current log's last valid checkpoint, without rewriting files.
func ReadHistory(path string) ([]llm.Message, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "state.json")
	}
	if strings.HasSuffix(path, ".jsonl") {
		return Load(path)
	}
	if history, err := ReadSnapshot(path); err == nil {
		return history, nil
	}
	return Load(LogPath(path))
}
