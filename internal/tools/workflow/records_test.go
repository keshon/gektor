package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
)

func TestRecordWorkflowReferencesRevisionsAndInspection(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	s, err := automation.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "assets", []byte(`{"type":"object","required":["width"],"properties":{"width":{"type":"integer"}},"additionalProperties":false}`)); err != nil {
		t.Fatal(err)
	}
	args := recipeArgs(t, ws, []byte(`{"version":2,"name":"Record revisions","steps":[{"id":"create","tool":"record_write","args":{"kind":"assets","id":"image","expected_revision":0,"data":{"width":8}}},{"id":"read","tool":"record_read","args":{"kind":"assets","id":{"$step":"create","pointer":"/id"}}},{"id":"update","tool":"record_write","args":{"kind":"assets","id":"image","expected_revision":{"$step":"read","pointer":"/revision"},"data":{"width":16}}}]}`))
	tool := Tool{WS: ws, StoreDir: base, Authorize: allow}
	tool.ValidateOnly = true
	if _, err := tool.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entry("assets", "image"); err == nil {
		t.Fatal("validation wrote data")
	}
	tool.ValidateOnly = false
	out, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err, out)
	}
	var result struct {
		ID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	entry, err := s.Entry("assets", "image")
	if err != nil || entry.Revision != 2 || string(entry.Data) != `{"width":16}` {
		t.Fatal(entry, err)
	}
	inspect := Inspect{WS: ws, StoreDir: base}
	inspectArgs, _ := json.Marshal(map[string]string{"run_id": result.ID})
	if out, err := inspect.Run(t.Context(), inspectArgs); err != nil || !strings.Contains(out, `"status":"completed"`) {
		t.Fatal(err, out)
	}
	out, err = tool.Run(t.Context(), args)
	if err == nil || !strings.Contains(out, "record revision conflict") || !strings.Contains(out, `"status":"skipped"`) {
		t.Fatal(err, out)
	}
	entry, err = s.Entry("assets", "image")
	if err != nil || entry.Revision != 2 {
		t.Fatal("retry changed record", entry, err)
	}
}

func TestRecordWorkflowChecksResolvedPermissions(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	s, err := automation.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
	args := recipeArgs(t, ws, []byte(`{"version":2,"name":"Dynamic permission","steps":[{"id":"write","tool":"record_write","args":{"kind":"notes","id":"one","expected_revision":0,"data":{}}},{"id":"read","tool":"record_read","args":{"kind":"notes","id":{"$step":"write","pointer":"/id"}}}]}`))
	checked := false
	tool := Tool{WS: ws, StoreDir: base, Authorize: func(_ context.Context, name string, args json.RawMessage) error {
		if name == "record_read" {
			checked = true
			if strings.Contains(string(args), `"id":"one"`) {
				return fmt.Errorf("denied notes/one")
			}
			t.Fatal("authorization received placeholder", string(args))
		}
		return nil
	}}
	out, err := tool.Run(t.Context(), args)
	if err == nil || !checked || !strings.Contains(out, "denied notes/one") {
		t.Fatal(err, out)
	}
}
