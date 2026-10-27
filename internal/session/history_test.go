package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestSearchHistoryRecoversLateUnicodeMatchAndPagesOriginalIDs(t *testing.T) {
	text := strings.Repeat("K", 2500) + "FAILED проверка" + strings.Repeat("я", 2200)
	s := New([]llm.Message{{Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleTool, Content: text}, {Role: llm.RoleAssistant, Content: "failed again"}})
	rows, next, err := SearchHistory(s, "failed", 1, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != 2 || rows[0].Offset != 2300 || next != 3 || !strings.Contains(rows[0].Text, "FAILED проверка") {
		t.Fatalf("lost match or position: %+v next=%d err=%v", rows, next, err)
	}
	page, err := PageHistory(s, rows[0].ID, 1, rows[0].NextOffset)
	if err != nil || page[0].Offset != 4300 || !strings.Contains(page[0].Text, "яяя") {
		t.Fatal("cannot continue original message", page, err)
	}
	rows, next, err = SearchHistory(s, "FAILED", next, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != 3 || next != 0 {
		t.Fatal(rows, next, err)
	}
	rows, next, err = SearchHistory(s, "absent", 1, 1)
	if err != nil || len(rows) != 0 || next != 0 {
		t.Fatal(rows, next, err)
	}
}

func TestSearchHistoryBoundsEncodedOutputWithoutLosingMatches(t *testing.T) {
	var history []llm.Message
	for range 10 {
		history = append(history, llm.Message{Role: llm.RoleTool, Content: "needle" + strings.Repeat("\x00", 2500)})
	}
	s := New(history)
	seen := 0
	for start := 1; start != 0; {
		rows, next, err := SearchHistory(s, "needle", start, 10)
		data, _ := json.Marshal(rows)
		if err != nil || len(rows) == 0 || len(data) > 24032 {
			t.Fatal(len(data), rows, err)
		}
		for _, row := range rows {
			seen++
			if row.ID != seen {
				t.Fatal("lost or repeated match", row.ID, seen)
			}
		}
		start = next
	}
	if seen != len(history) {
		t.Fatal(seen)
	}
}

func TestSearchHistoryFindsCallsAndRejectsInvalidPositions(t *testing.T) {
	s := New([]llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "check", Name: "run_program", Arguments: []byte(`{"args":["test","./..."]}`)}}}})
	rows, _, err := SearchHistory(s, "run_program", 1, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != 1 {
		t.Fatal(rows, err)
	}
	for _, args := range []struct {
		query        string
		start, count int
	}{{" ", 1, 1}, {"test", 0, 1}, {"test", 3, 1}, {"test", 1, 11}, {strings.Repeat("x", 257), 1, 1}} {
		if _, _, err := SearchHistory(s, args.query, args.start, args.count); err == nil {
			t.Fatal("accepted invalid search", args)
		}
	}
}
