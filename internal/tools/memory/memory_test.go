package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/memory"
)

func TestSearchScopesPagesAndPreservesWholeEntries(t *testing.T) {
	directory, project, other := t.TempDir(), t.TempDir(), t.TempDir()
	store, err := memory.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	text := strings.Repeat("\x01", memory.MaxTextRunes)
	for _, in := range []memory.Input{{Scope: memory.User, Text: text}, {Scope: memory.Project, Text: "visible project fact"}} {
		_, _, err = store.Add(t.Context(), project, in)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = store.Add(t.Context(), other, memory.Input{Scope: memory.Project, Text: "unrelated project fact"})
	if err != nil {
		t.Fatal(err)
	}
	tool := &Tool{Directory: directory, Workspace: project}
	result, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Entries []recalledEntry `json:"entries"`
		Total   int             `json:"total"`
		Next    int             `json:"next_offset"`
	}
	err = json.Unmarshal([]byte(result), &page)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Next == 0 || len(page.Entries) == 0 || page.Entries[0].Text != text || utf8.RuneCountInString(result) > searchBudget {
		t.Fatalf("invalid bounded page: total=%d next=%d", page.Total, page.Next)
	}
	if strings.Contains(result, "unrelated project") {
		t.Fatal("project leaked")
	}
	filtered, err := tool.Run(t.Context(), json.RawMessage(`{"query":"VISIBLE"}`))
	if err != nil || !strings.Contains(filtered, "visible project fact") || strings.Contains(filtered, "unrelated project") {
		t.Fatal(filtered, err)
	}
	if tool.ReuseInContext(nil, result, nil) != result {
		t.Fatal("current delivery hidden")
	}
	next := &Tool{Directory: directory, Workspace: other}
	if strings.Contains(next.ReuseInContext(nil, result, nil), "visible project fact") {
		t.Fatal("historical result leaked to next run")
	}
}

func TestSearchAbsentStoreAndCancellationDoNotCreateStorage(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	tool := &Tool{Directory: directory, Workspace: t.TempDir()}
	result, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(result, `"total":0`) {
		t.Fatal(result, err)
	}
	if _, err = os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("lookup created storage", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = tool.Run(ctx, json.RawMessage(`{}`))
	if err != context.Canceled {
		t.Fatal("ignored cancellation", err)
	}
}
