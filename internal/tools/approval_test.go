package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFullApprovalIncludesAllChangesAndDetectsStaleFiles(t *testing.T) {
	ws, dir := previewWS(t)
	body := strings.Repeat("new line\n", 65) + "FINAL_PROPOSAL_LINE\n"
	args, _ := json.Marshal(map[string]string{"path": "new.txt", "content": body})
	e := FullApproval(ws, "write_file", args)
	if !strings.Contains(e.Text, "+FINAL_PROPOSAL_LINE") || !e.Current(ws) {
		t.Fatal("missing complete new-file evidence", e.Text)
	}
	if strings.Contains(PreviewArgs(ws, "write_file", args), "FINAL_PROPOSAL_LINE") {
		t.Fatal("expanded evidence leaked into bounded prompt")
	}
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("another writer"), 0600); err != nil {
		t.Fatal(err)
	}
	if e.Current(ws) {
		t.Fatal("new file creation did not stale approval")
	}
	e = FullApproval(ws, "write_file", args)
	if !strings.Contains(e.Text, "-another writer") || !strings.Contains(e.Text, "+FINAL_PROPOSAL_LINE") {
		t.Fatal(e.Text)
	}
	if err := os.WriteFile(path, []byte("changed again"), 0600); err != nil {
		t.Fatal(err)
	}
	if e.Current(ws) {
		t.Fatal("modified source did not stale approval")
	}
	patch := json.RawMessage(`{"path":"new.txt","old_content":"changed again","new_content":"replacement"}`)
	e = FullApproval(ws, "patch_file", patch)
	if !strings.Contains(e.Text, "+replacement") {
		t.Fatal(e.Text)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if e.Current(ws) {
		t.Fatal("deleted source did not stale approval")
	}
}
