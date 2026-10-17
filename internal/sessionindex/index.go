// Package sessionindex reads saved conversation summaries and searchable text.
// It has no terminal dependencies; callers own scheduling and stale-result rejection.
package sessionindex

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/workspace"
)

type Text struct {
	Text    string
	Message int
}
type Entry struct {
	ID, Dir, Title        string
	Msgs                  int
	Updated               time.Time
	Status, Mode, Preview string
	Search                []Text
}

// CachedSession values and their slices are immutable once passed to Scan.
// Scan returns a new map and never writes the previous snapshot.
type CachedSession struct {
	Entry       Entry
	size        int64
	modified    time.Time
	searchable  bool
	storedTitle string
	state       string
	logSize     int64
	logModified time.Time
	hasSnapshot bool
}

func Scan(root string, previous map[string]CachedSession, search bool) ([]Entry, map[string]CachedSession) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, nil
	}
	cache := make(map[string]CachedSession, len(dirs))
	var entries []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		title, mode := workspace.ReadSessionTitle(dir), ReadMode(dir)
		state := filepath.Join(dir, "state.json")
		fi, stateErr := os.Stat(state)
		hasSnapshot := stateErr == nil
		logInfo, _ := os.Stat(session.LogPath(state))
		if stateErr != nil && logInfo != nil {
			fi, stateErr = logInfo, nil
		}
		old, ok := previous[dir]
		current := CachedSession{state: state, hasSnapshot: hasSnapshot, storedTitle: title, Entry: Entry{ID: d.Name(), Dir: dir, Title: title, Mode: mode, Status: "Saved"}}
		if logInfo != nil {
			current.logSize, current.logModified = logInfo.Size(), logInfo.ModTime()
		}
		if stateErr == nil {
			current.size, current.modified = fi.Size(), fi.ModTime()
			unchanged := ok && old.state == state && old.storedTitle == title &&
				old.hasSnapshot == current.hasSnapshot &&
				old.size == current.size && old.modified.Equal(current.modified) &&
				old.logSize == current.logSize && old.logModified.Equal(current.logModified)
			if unchanged && (!search || old.searchable) {
				current = old
				current.Entry.Mode = mode
				if title != "" {
					current.Entry.Title = title
				}
			} else {
				current.Entry.Status = "Unreadable"
				current.Entry.Updated = fi.ModTime()
				if logInfo != nil && logInfo.ModTime().After(current.Entry.Updated) {
					current.Entry.Updated = logInfo.ModTime()
				}
				if fi.Size() < 64<<20 {
					loadHistory := func() ([]llm.Message, error) {
						return session.ReadHistory(state)
					}
					if history, err := loadHistory(); err == nil {
						e := &current.Entry
						e.Status, e.Msgs = "Saved", len(history)
						if search {
							e.Search = ConversationText(history)
							current.searchable = true
						}
						if _, question, paused := agent.PausedOnQuestion(nil, history); paused {
							e.Status, e.Preview = "Needs input", question
						} else {
							for i := len(history) - 1; i >= 0; i-- {
								if history[i].Role == llm.RoleAssistant && history[i].Content != "" {
									e.Preview = truncate(history[i].Content, 500)
									break
								}
							}
						}
						if e.Title == "" {
							e.Title = session.TitleFor(history)
						}
					}
				}
			}
		}
		if fi, err := os.Stat(filepath.Join(dir, "mission.json")); err == nil {
			current.Entry.Mode = "Mission"
			if current.Entry.Updated.IsZero() {
				current.Entry.Updated = fi.ModTime()
			}
			current.Entry.Preview = "Mission snapshot. Resume with agent -tui -resume " + dir
		}
		if current.Entry.Updated.IsZero() {
			continue
		}
		if current.Entry.Title == "" {
			current.Entry.Title = current.Entry.ID
		}
		cache[dir] = current
		entries = append(entries, current.Entry)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Updated.After(entries[j].Updated) })
	return entries, cache
}

func truncate(s string, n int) string {
	count := 0
	for i := range s {
		if count >= n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
func ReadMode(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "mode"))
	if err != nil {
		return "Unknown mode"
	}
	switch strings.TrimSpace(string(data)) {
	case "plan":
		return "Plan"
	case "act":
		return "Act"
	}
	return "Unknown mode"
}

// ConversationText excludes operational messages, attachments, and private reasoning.
// Positions refer to source messages, never UI blocks or terminal geometry.
func ConversationText(history []llm.Message) []Text {
	var result []Text
	for i, msg := range history {
		text := msg.Content
		switch msg.Role {
		case llm.RoleUser:
			if strings.HasPrefix(text, "[harness] ") {
				continue
			}
			text = session.DisplayInput(text)
		case llm.RoleAssistant:
			regions := llm.ThinkRegions(text)
			var visible strings.Builder
			start := 0
			for _, region := range regions {
				visible.WriteString(text[start:region[0]])
				start = region[1]
			}
			visible.WriteString(text[start:])
			text = visible.String()
			if len(regions) > 0 {
				text = strings.TrimSpace(text)
			}
		default:
			continue
		}
		if strings.TrimSpace(text) != "" {
			result = append(result, Text{text, i})
		}
	}
	return result
}
