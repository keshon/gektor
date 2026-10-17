// Package session keeps a recovery log next to state.json.
// state.json is the resume snapshot (rewritten atomically each step);
// the log holds the last few checkpoints, so a snapshot lost to
// something atomic rename cannot prevent — bad sectors, a restored
// backup — can be read back. A corrupt log line is skipped, never fatal.
package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/tars/internal/llm"
)

// logKeepEntries is how many checkpoints a log holds. Recovery reads the
// last one, and the one before it covers an append that did not land.
// Every entry is a full checkpoint, so the count never needs to grow: a
// 90-turn session used to append one per step and keep all of them, 6 MB
// of history to resume from 46 KB, which the next step made 99% stale.
const logKeepEntries = 2

// maxLogEntry bounds one entry read back. An entry is a whole
// conversation, so this is generous rather than tight.
const maxLogEntry = 64 * 1024 * 1024

// Event is one checkpoint.
type Event struct {
	Seq      int           `json:"seq"`
	Time     string        `json:"time"`
	Messages int           `json:"messages"`
	History  []llm.Message `json:"history"`
}

// LogPath returns the .jsonl path next to a state.json path.
func LogPath(stateFile string) string {
	ext := filepath.Ext(stateFile)
	if ext == "" {
		return stateFile + ".jsonl"
	}
	return stateFile[:len(stateFile)-len(ext)] + ".jsonl"
}

// Append writes a bounded recovery log and returns persistence failures.
// Every entry is a full checkpoint, so trimming to logKeepEntries cannot
// leave recovery depending on an earlier delta that has since rotated.
func Append(stateFile string, seq int, history []llm.Message) error {
	if stateFile == "" {
		return nil
	}
	path := LogPath(stateFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(Event{
		Seq: seq, Time: time.Now().UTC().Format(time.RFC3339),
		Messages: len(history), History: history,
	})
	if err != nil {
		return err
	}
	keep, err := tailEntries(path, logKeepEntries-1)
	if err != nil {
		return err
	}
	return rewrite(path, keep, data)
}

// tailEntries reads at most n whole entries from the end of the log,
// skipping a torn line. Returns fewer than n when the log is shorter,
// and none when there is no log yet.
func tailEntries(path string, n int) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	var ring [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), maxLogEntry)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			ring = append(ring, append([]byte(nil), line...))
			if len(ring) > n {
				ring = ring[1:]
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}

// rewrite replaces the log with keep plus newest, atomically: a crash
// mid-write leaves the previous log whole rather than a half one.
func rewrite(path string, keep [][]byte, newest []byte) error {
	var buf bytes.Buffer
	for _, e := range keep {
		buf.Write(e)
		buf.WriteByte('\n')
	}
	buf.Write(newest)
	buf.WriteByte('\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads the last event's history from a .jsonl log, skipping corrupt
// lines. Returns an error only when no valid event exists.
func Load(path string) ([]llm.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read session log: %w", err)
	}
	defer f.Close()
	var last []llm.Message
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.History != nil {
			last = ev.History
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read session log: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("session log %s holds no valid events", path)
	}
	return last, nil
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
