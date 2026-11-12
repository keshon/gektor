package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tooloutput "github.com/keshon/gektor/internal/tools/output"
	"github.com/keshon/gektor/internal/tools/readoutput"
)

func TestOutputCanBePagedOnlyByItsSession(t *testing.T) {
	store := tooloutput.New(t.TempDir())
	ctx := store.Context(t.Context())
	original := strings.Repeat("head\n", 5000) + "UNIQUE MIDDLE\n" + strings.Repeat("tail\n", 5000)
	preview := tooloutput.Limit(ctx, original, 2048)
	if !strings.Contains(preview, "read_output") || len(preview) > 2048 {
		t.Fatal("missing bounded output handle", len(preview))
	}
	entries, _ := os.ReadDir(store.Scope())
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	id := strings.TrimSuffix(entries[0].Name(), ".log")
	offset := 0
	var full strings.Builder
	for {
		args, _ := json.Marshal(map[string]any{"id": id, "offset": offset, "limit": 1001})
		out, err := (readoutput.Tool{}).Run(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			Text string
			Next int `json:"next_offset"`
			EOF  bool
		}
		if err := json.Unmarshal([]byte(out), &row); err != nil {
			t.Fatal(err)
		}
		full.WriteString(row.Text)
		offset = row.Next
		if row.EOF {
			break
		}
	}
	if full.String() != original {
		t.Fatal("paged output lost data")
	}
	other := (tooloutput.New(t.TempDir())).Context(t.Context())
	args, _ := json.Marshal(map[string]any{"id": id})
	if _, err := (readoutput.Tool{}).Run(other, args); err == nil {
		t.Fatal("cross-session read allowed")
	}
}
