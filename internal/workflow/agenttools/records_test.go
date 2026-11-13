package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/nodes/record"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestRecordAdaptersShareStateAndFailureSemantics(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	s, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
	nodes := record.Definitions(ws, base)
	read, write := Data{WS: ws, StoreDir: base}, Data{WS: ws, StoreDir: base, Write: true}
	missing := json.RawMessage(`{"kind":"notes","id":"state","allow_missing":true}`)
	fromTool, err := read.Run(t.Context(), missing)
	if err != nil {
		t.Fatal(err)
	}
	fromNode, err := nodes[0].Execute(t.Context(), missing)
	if err != nil || fromTool != fromNode || !strings.Contains(fromTool, `"found":false`) {
		t.Fatalf("missing record differs: tool=%s node=%s error=%v", fromTool, fromNode, err)
	}
	create := json.RawMessage(`{"kind":"notes","id":"state","expected_revision":0,"data":{"value":7}}`)
	if out, err := write.Run(t.Context(), create); err != nil {
		t.Fatal(out, err)
	} else if err := workflow.ValidateJSON(json.RawMessage(out), nodes[1].OutputSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes[1].Execute(t.Context(), create); !errors.Is(err, store.ErrConflict) {
		t.Fatal("node did not preserve optimistic revision conflict", err)
	}
	fromTool, err = read.Run(t.Context(), missing)
	if err != nil {
		t.Fatal(err)
	}
	fromNode, err = nodes[0].Execute(t.Context(), missing)
	if err != nil || fromTool != fromNode || !strings.Contains(fromNode, `"found":true`) {
		t.Fatalf("stored record differs: tool=%s node=%s error=%v", fromTool, fromNode, err)
	}
	if err := workflow.ValidateJSON(json.RawMessage(fromNode), nodes[0].OutputSchema); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	update := json.RawMessage(`{"kind":"notes","id":"state","expected_revision":1,"data":{"value":8}}`)
	for name, run := range map[string]func(context.Context, json.RawMessage) (string, error){"tool": write.Run, "node": nodes[1].Execute} {
		if _, err := run(cancelled, update); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s ignored cancellation: %v", name, err)
		}
	}
	entry, err := s.Entry("notes", "state")
	if err != nil || entry.Revision != 1 || string(entry.Data) != `{"value":7}` {
		t.Fatal("failed operations changed record", entry, err)
	}
	other, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for name, run := range map[string]func(context.Context, json.RawMessage) (string, error){
		"tool": (Data{WS: other, StoreDir: base}).Run,
		"node": record.Definitions(other, base)[0].Execute,
	} {
		if out, err := run(t.Context(), missing); err == nil {
			t.Fatalf("%s accessed another workspace's record schema: %s", name, out)
		}
	}
}

func TestRecordWorkflowReferencesRevisionsAndInspection(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	s, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "assets", []byte(`{"type":"object","required":["width"],"properties":{"width":{"type":"integer"}},"additionalProperties":false}`)); err != nil {
		t.Fatal(err)
	}
	args := recipeArgs(t, ws, []byte(`{"version":4,"name":"Record revisions","steps":[{"id":"create","tool":"record_write","args":{"kind":"assets","id":"image","expected_revision":0,"data":{"width":8}}},{"id":"read","tool":"record_read","args":{"kind":"assets","id":{"$step":"create","pointer":"/id"}}},{"id":"update","tool":"record_write","args":{"kind":"assets","id":"image","expected_revision":{"$step":"read","pointer":"/revision"},"data":{"width":16}}}]}`))
	tool := Tool{WS: ws, StoreDir: base, Authorize: allow, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, base, nil, nil)
	},
	}
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
	s, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
	args := recipeArgs(t, ws, []byte(`{"version":4,"name":"Dynamic permission","steps":[{"id":"write","tool":"record_write","args":{"kind":"notes","id":"one","expected_revision":0,"data":{}}},{"id":"read","tool":"record_read","args":{"kind":"notes","id":{"$step":"write","pointer":"/id"}}}]}`))
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
	}, Registry: func() (*workflow.Registry, error) {
		return app.WorkflowRegistry(ws, base, nil, nil)
	},
	}
	out, err := tool.Run(t.Context(), args)
	if err == nil || !checked || !strings.Contains(out, "denied notes/one") {
		t.Fatal(err, out)
	}
}
