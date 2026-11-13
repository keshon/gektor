package agenttools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestInspectViewsKeepLargePayloadsOutOfDefaultContext(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	db, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Second).UTC()
	finished := started.Add(time.Second)
	output := `{"status":"stale","a/b":{"~n":9007199254740993},"body":"` + strings.Repeat("x", 100000) + `"}`
	r := workflow.Run{Status: "completed", Started: started, Finished: &finished, RecipeSource: "source-marker", Definition: workflow.Definition{Name: "Monitor", Steps: []workflow.Step{{ID: "fetch", Tool: "http_request"}}}, Steps: []workflow.StepResult{
		{ID: "fetch", Status: "completed", Args: json.RawMessage(`{"url":"https://example.test"}`), Output: output},
		{ID: "@outputs", Status: "completed", Output: output},
	}}
	if err := w.Save(r); err != nil {
		t.Fatal(err)
	}
	w.Close()
	tool := Inspect{WS: ws, StoreDir: base}
	call := func(extra map[string]any) string {
		t.Helper()
		extra["run_id"] = w.ID()
		args, _ := json.Marshal(extra)
		out, err := tool.Run(t.Context(), args)
		if err != nil || !json.Valid([]byte(out)) {
			t.Fatal(out, err)
		}
		return out
	}
	out := call(map[string]any{})
	var summary service.RunInspection
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if len(out) > 2000 || strings.Contains(out, "source-marker") || !summary.Outputs.Omitted || summary.Outputs.Bytes != len(output) || summary.DurationMS == nil || *summary.DurationMS != 1000 {
		t.Fatal(out)
	}
	out = call(map[string]any{"pointer": "/body"})
	if !strings.Contains(out, `"omitted":true`) {
		t.Fatal(out)
	}
	for _, view := range []string{"summary", "step"} {
		args := map[string]any{"view": view, "pointer": "/a~1b/~0n"}
		if view == "step" {
			args["step_id"] = "fetch"
		}
		out = call(args)
		if !strings.Contains(out, `"value":9007199254740993`) {
			t.Fatal(out)
		}
	}
	out = call(map[string]any{"view": "steps"})
	if !strings.Contains(out, `"operation":"http_request"`) || strings.Contains(out, `"body"`) {
		t.Fatal(out)
	}
	out = call(map[string]any{"view": "step", "step_id": "fetch"})
	if !strings.Contains(out, `"url":"https://example.test"`) || !strings.Contains(out, `"omitted":true`) {
		t.Fatal(out)
	}
	out = call(map[string]any{"view": "full"})
	if !strings.Contains(out, "source-marker") || !strings.Contains(out, strings.Repeat("x", 100000)) {
		t.Fatal("full view lost evidence")
	}
	for _, args := range []map[string]any{
		{"view": "step"}, {"view": "summary", "step_id": "fetch"}, {"view": "step", "step_id": "missing"},
		{"view": "steps", "pointer": "/status"}, {"pointer": "/missing"}, {"pointer": "/~bad"}, {"view": "nonsense"},
	} {
		args["run_id"] = w.ID()
		raw, _ := json.Marshal(args)
		if _, err := tool.Run(t.Context(), raw); err == nil {
			t.Fatal("accepted invalid selection", args)
		}
	}
	// A process exit must surface interrupted even though its last saved status says running.
	w2, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := w2.Save(workflow.Run{Status: "running", Started: started}); err != nil {
		t.Fatal(err)
	}
	w2.Close()
	raw, _ := json.Marshal(map[string]string{"run_id": w2.ID()})
	out, err = tool.Run(t.Context(), raw)
	if err != nil || !strings.Contains(out, `"status":"interrupted"`) {
		t.Fatal(out, err)
	}
}

func TestInspectFindsScopedChildStep(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	db, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	child := workflow.Run{Status: "completed", Definition: workflow.Definition{Steps: []workflow.Step{{ID: "fetch", Tool: "http_request"}}}, Steps: []workflow.StepResult{{ID: "fetch", Status: "completed", Output: `{"answer":42}`}}}
	r := workflow.Run{Status: "completed", Definition: workflow.Definition{Steps: []workflow.Step{{ID: "call", Tool: "workflow_call"}}}, Steps: []workflow.StepResult{{ID: "call", Status: "completed", Child: &child}}}
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	tool := Inspect{WS: ws, StoreDir: base}
	args, err := json.Marshal(map[string]string{"run_id": w.ID(), "view": "step", "step_id": "call/fetch", "pointer": "/answer"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Run(t.Context(), args)
	if err != nil || !strings.Contains(out, `"id":"call/fetch"`) || !strings.Contains(out, `"operation":"http_request"`) || !strings.Contains(out, `"value":42`) {
		t.Fatal("nested step inspection lost scope", out, err)
	}
}
