package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func TestFixtureNeverCallsExternalExecutorsOrCreatesRunStorage(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	source := []byte(`{"version":4,"name":"Fixture","steps":[{"id":"fetch","tool":"probe","args":{"count":2},"retry":{"max_attempts":2}}],"outputs":{"value":{"$step":"fetch","pointer":"/value"}}}`)
	err = ws.WriteFile("recipe.json", source, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	trap := func(context.Context, json.RawMessage) (string, error) { effects.Add(1); return `{}`, nil }
	nodes := callnode.Definitions()
	nodes = append(nodes, workflow.Node{Name: "probe", Version: 1, RetrySafe: true, Concurrent: true, InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object","required":["value"]}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: trap})
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, Registry: registry, StoreDir: t.TempDir(), Authorize: func(context.Context, string, json.RawMessage) error { return nil }}
	raw := []byte(`{"version":1,"inputs":{},"calls":[{"step_id":"fetch","operation":"probe","node_version":1,"args":{"count":2},"responses":[{"failure":{"code":"temporary","message":"try again"}},{"output":{"value":4}}]}],"expect":{"status":"completed","outputs":{"value":4}}}`)
	report, err := svc.Test(t.Context(), "recipe.json", raw)
	if err != nil || !report.Passed || effects.Load() != 0 {
		t.Fatal(report, effects.Load(), err)
	}
	stored, err := os.ReadDir(svc.StoreDir)
	if err != nil || len(stored) != 0 {
		t.Fatal("fixture created durable storage", stored, err)
	}
	entries, err := ws.ReadDir(".")
	if err != nil || len(entries) != 1 {
		t.Fatal("fixture changed workspace", entries, err)
	}
	for _, invalid := range []string{
		`{"version":1,"inputs":{},"calls":[],"expect":{"status":"failed"}}`,
		`{"version":1,"inputs":{},"calls":[{"step_id":"fetch","operation":"probe","node_version":1,"args":{"count":3},"responses":[{"output":{"value":4}}]}],"expect":{"status":"completed","outputs":{"value":4}}}`,
		`{"version":1,"inputs":{},"calls":[{"step_id":"fetch","operation":"probe","node_version":1,"args":{"count":2},"responses":[{"output":{}}]}],"expect":{"status":"completed","outputs":{"value":4}}}`,
	} {
		report, err = svc.Test(t.Context(), "recipe.json", []byte(invalid))
		if err == nil || report.Passed || effects.Load() != 0 {
			t.Fatal(report, effects.Load(), err)
		}
	}
	failed := strings.Replace(string(raw), `"output":{"value":4}`, `"failure":{"code":"temporary","message":"still unavailable"}`, 1)
	failed = strings.Replace(failed, `"status":"completed","outputs":{"value":4}`, `"status":"failed","outputs":null`, 1)
	report, err = svc.Test(t.Context(), "recipe.json", []byte(failed))
	if err != nil || !report.Passed || report.Status != "failed" {
		t.Fatal("expected failure was not matched", report, err)
	}

	for _, unsupported := range []string{
		strings.Replace(string(source), `"retry":{"max_attempts":2}`, `"review":true`, 1),
		`{"version":4,"name":"Wait","steps":[{"id":"pause","tool":"workflow_wait","args":{"delay_ms":0}}]}`,
	} {
		err = ws.WriteFile("recipe.json", []byte(unsupported), 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.Test(t.Context(), "recipe.json", raw)
		if err == nil || !strings.Contains(err.Error(), "does not model reviews or durable waits") {
			t.Fatal("unsupported control was not rejected", err)
		}
	}

	child := []byte(`{"version":4,"name":"Child","inputs":{"count":{"type":"integer","required":true}},"steps":[{"id":"fetch","tool":"probe","args":{"count":{"$input":"count"}}}],"outputs":{"value":{"$step":"fetch","pointer":"/value"}}}`)
	err = ws.WriteFile("child.json", child, 0600)
	if err != nil {
		t.Fatal(err)
	}
	mapped := []byte(`{"version":4,"name":"Map","steps":[{"id":"batch","tool":"workflow_call","args":{"recipe_path":"child.json"},"for_each":[{"count":2},{"count":3}]}],"outputs":{"first":{"$step":"batch","pointer":"/items/0/output/value"}}}`)
	err = ws.WriteFile("recipe.json", mapped, 0600)
	if err != nil {
		t.Fatal(err)
	}
	mappedFixture := []byte(`{"version":1,"inputs":{},"calls":[{"step_id":"batch[0]/fetch","operation":"probe","node_version":1,"args":{"count":2},"responses":[{"output":{"value":4}}]},{"step_id":"batch[1]/fetch","operation":"probe","node_version":1,"args":{"count":3},"responses":[{"output":{"value":6}}]}],"expect":{"status":"completed","outputs":{"first":4}}}`)
	report, err = svc.Test(t.Context(), "recipe.json", mappedFixture)
	if err != nil || !report.Passed || effects.Load() != 0 {
		t.Fatal("mapped child fixture lost scope", report, err)
	}

}
