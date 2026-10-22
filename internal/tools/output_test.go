package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOutputCanBePagedOnlyByItsSession(t *testing.T) {
	store := &OutputStore{dir: t.TempDir()}
	ctx := store.Context(t.Context())
	original := strings.Repeat("head\n", 5000) + "UNIQUE MIDDLE\n" + strings.Repeat("tail\n", 5000)
	preview := LimitOutput(ctx, "test", original, 2048)
	if !strings.Contains(preview, "read_output") || len(preview) > 2048 {
		t.Fatal("missing bounded output handle", len(preview))
	}
	entries, _ := os.ReadDir(store.dir)
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	id := strings.TrimSuffix(entries[0].Name(), ".log")
	offset := 0
	var full strings.Builder
	for {
		args, _ := json.Marshal(map[string]any{"id": id, "offset": offset, "limit": 1001})
		out, err := (ReadOutput{}).Run(ctx, args)
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
	other := (&OutputStore{dir: t.TempDir()}).Context(t.Context())
	args, _ := json.Marshal(map[string]any{"id": id})
	if _, err := (ReadOutput{}).Run(other, args); err == nil {
		t.Fatal("cross-session read allowed")
	}
}
func TestOutputFileLimitIsExplicit(t *testing.T) {
	store := &OutputStore{dir: t.TempDir()}
	file, err := store.create()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(strings.Repeat("x", outputFileLimit+100)))
	note := file.close()
	if !strings.Contains(note, "later bytes were not saved") {
		t.Fatal(note)
	}
	info, err := os.Stat(file.file.Name())
	if err != nil || info.Size() != outputFileLimit {
		t.Fatal(info, err)
	}
}
