package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/workspace"
)

// savedText is one searchable message and its index in the source history.
// Positions refer to source messages, never UI blocks or geometry.
//
// lower is the case-folded copy the filter matches against. Folding it per
// keystroke instead cost one allocation per message per keypress, over the
// whole conversation corpus of every session.
type savedText struct {
	Text    string
	Message int
	lower   string
}

// savedSummary is one row of the sessions list.
type savedSummary struct {
	ID, Dir, Title        string
	Msgs                  int
	Updated               time.Time
	Status, Mode, Preview string
	Search                []savedText
}

// cachedSession values and their slices are immutable once passed to
// scanSavedSessions, which returns a new map and never writes the
// previous snapshot. The size/mtime pair is what makes an unchanged
// session free to re-list.
type cachedSession struct {
	savedSummary
	size        int64
	modified    time.Time
	searchable  bool
	storedTitle string
	state       string
	hasSnapshot bool
}

func scanSavedSessions(root string, previous map[string]cachedSession, search bool) ([]savedSummary, map[string]cachedSession) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, nil
	}
	cache := make(map[string]cachedSession, len(dirs))
	var entries []savedSummary
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		title, mode := workspace.ReadSessionTitle(dir), readSavedMode(dir)
		state := filepath.Join(dir, "state.json")
		fi, stateErr := os.Stat(state)
		current := cachedSession{state: state, hasSnapshot: stateErr == nil, storedTitle: title,
			savedSummary: savedSummary{ID: d.Name(), Dir: dir, Title: title, Mode: mode, Status: "Saved"}}
		old, ok := previous[dir]
		if stateErr == nil {
			current.size, current.modified = fi.Size(), fi.ModTime()
			unchanged := ok && old.state == state && old.storedTitle == title &&
				old.hasSnapshot == current.hasSnapshot &&
				old.size == current.size && old.modified.Equal(current.modified)
			if unchanged && (!search || old.searchable) {
				current = old
				current.Mode = mode
				if title != "" {
					current.Title = title
				}
			} else {
				current.Status = "Unreadable"
				current.Updated = fi.ModTime()
				if fi.Size() < 64<<20 {
					if history, err := session.ReadHistory(state); err == nil {
						e := &current.savedSummary
						e.Status, e.Msgs = "Saved", len(history)
						if search {
							e.Search = conversationText(history)
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
		if current.Updated.IsZero() {
			continue
		}
		if current.Title == "" {
			current.Title = current.ID
		}
		cache[dir] = current
		entries = append(entries, current.savedSummary)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Updated.After(entries[j].Updated) })
	return entries, cache
}

func readSavedMode(dir string) string {
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

// conversationText excludes operational messages, attachments, and private
// reasoning, so search never surfaces a harness nudge or a thought.
func conversationText(history []llm.Message) []savedText {
	var result []savedText
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
			result = append(result, savedText{Text: text, Message: i, lower: strings.ToLower(text)})
		}
	}
	return result
}
