package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
)

type HistoryRow struct {
	ID         int      `json:"id"`
	Role       llm.Role `json:"role"`
	Text       string   `json:"text"`
	CallID     string   `json:"call_id,omitempty"`
	Failed     *bool    `json:"failed,omitempty"`
	NextOffset int      `json:"next_offset,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}

func ArchivedText(m llm.Message) string {
	text := m.Content
	if len(m.Images) > 0 {
		text += "\nAttachment paths (pixels not loaded): " + strings.Join(m.Images, ", ")
	}
	if m.Reasoning != "" {
		text += "\nHistorical reasoning: " + m.Reasoning
	}
	for _, call := range m.ToolCalls {
		args := string(call.Arguments)
		if call.InputError != "" {
			args = call.RawArguments
		}
		text += "\nTool call " + call.ID + " " + call.Name + " " + args
	}
	return text
}

// PageHistory is shared by interactive recovery and retrospective evaluation.
// Stable IDs index the original archive, never the compacted projection.
func PageHistory(s Snapshot, start, count, offset int) ([]HistoryRow, error) {
	if count == 0 {
		count = 8
	}
	if start < 1 || start > len(s.History) || count < 1 || count > 10 || offset < 0 || (offset > 0 && count != 1) {
		return nil, fmt.Errorf("invalid history position: start 1–%d, count 1–10; offsets require count=1", len(s.History))
	}
	var rows []HistoryRow
	size := 0
	for n := start - 1; n < min(start-1+count, len(s.History)); n++ {
		m := s.History[n]
		runes := []rune(ArchivedText(m))
		if offset > len(runes) {
			return nil, fmt.Errorf("offset exceeds message length")
		}
		end := min(offset+2000, len(runes))
		row := HistoryRow{ID: n + 1, Role: m.Role, Text: string(runes[offset:end]), CallID: m.ToolCallID, Failed: m.ToolFailed, Offset: offset}
		if end < len(runes) {
			row.NextOffset = end
		}
		data, _ := json.Marshal(row)
		if size+len(data) > 24000 {
			break
		}
		size += len(data)
		rows = append(rows, row)
	}
	return rows, nil
}

// SearchHistory returns bounded excerpts around literal matches, with stable
// archive IDs and offsets for recovering the complete original message.
func SearchHistory(s Snapshot, query string, start, count int) ([]HistoryRow, int, error) {
	if count == 0 {
		count = 8
	}
	if strings.TrimSpace(query) == "" || len([]rune(query)) > 256 || start < 1 || start > len(s.History)+1 || count < 1 || count > 10 {
		return nil, 0, fmt.Errorf("invalid history search: nonblank query up to 256 characters, start 1–%d, count 1–10", len(s.History)+1)
	}
	rows := make([]HistoryRow, 0, count)
	needle := strings.ToLower(query)
	size := 0
	for n := start - 1; n < len(s.History); n++ {
		text := ArchivedText(s.History[n])
		// Counting runes keeps offsets valid when Unicode case conversion changes
		// UTF-8 byte lengths (for example the Kelvin sign).
		lower := strings.ToLower(text)
		position := strings.Index(lower, needle)
		if position < 0 {
			continue
		}
		if len(rows) == count {
			return rows, n + 1, nil
		}
		match := utf8.RuneCountInString(lower[:position])
		page, err := PageHistory(s, n+1, 1, max(0, match-200))
		if err != nil {
			return nil, 0, err
		}
		data, _ := json.Marshal(page[0])
		if size+len(data) > 24000 {
			return rows, n + 1, nil
		}
		size += len(data)
		rows = append(rows, page[0])
	}
	return rows, 0, nil
}
