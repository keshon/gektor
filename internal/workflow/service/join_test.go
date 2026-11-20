package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

const joinRecipe = `{"version":4,"name":"Join","steps":[
	{"id":"left","tool":"probe","args":{"rows":[{"id":2},{"id":1}]}},
	{"id":"right","tool":"probe","args":{"rows":[{"id":1},{"id":3}]}},
	{"id":"join","tool":"workflow_join","args":{"left":{"$step":"left","pointer":"/rows"},"right":{"$step":"right","pointer":"/rows"},"left_key":"/id","right_key":"/id","mode":"left"}}
],"outputs":{"pairs":{"$step":"join"}}}`

func TestJoinFixturesExecuteRealLogicAndDetectChangedResults(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	nodes := callnode.Definitions()
	nodes = append(nodes, workflow.Node{
		Name:        "probe",
		Version:     1,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute:     func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil },
	})
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{
		WS:        ws,
		Registry:  registry,
		StoreDir:  t.TempDir(),
		Authorize: func(context.Context, string, json.RawMessage) error { return nil },
	}
	err = ws.WriteFile("join.json", []byte(joinRecipe), 0600)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":1,"inputs":{},"calls":[{"step_id":"left","operation":"probe","node_version":1,"args":{"rows":[{"id":2},{"id":1}]},"responses":[{"output":{"rows":[{"id":2},{"id":1}]}}]},{"step_id":"right","operation":"probe","node_version":1,"args":{"rows":[{"id":1},{"id":3}]},"responses":[{"output":{"rows":[{"id":1},{"id":3}]}}]}],"expect":{"status":"completed","outputs":{"pairs":[{"left":{"id":2},"right":null},{"left":{"id":1},"right":{"id":1}}]}}}`)
	report, err := svc.Test(t.Context(), "join.json", raw)
	if err != nil || !report.Passed {
		t.Fatal(report, err)
	}
	changed := strings.Replace(joinRecipe, `"mode":"left"`, `"mode":"inner"`, 1)
	err = ws.WriteFile("join.json", []byte(changed), 0600)
	if err != nil {
		t.Fatal(err)
	}
	report, err = svc.Test(t.Context(), "join.json", raw)
	if err == nil || report.Passed {
		t.Fatal("fixture substituted the old join result", report, err)
	}
}

func TestJoinPersistenceReviewSkipAndFailureRouting(t *testing.T) {
	for _, mode := range []string{"review", "skip", "failure"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			var calls atomic.Int32
			nodes := callnode.Definitions()
			nodes = append(nodes, workflow.Node{
				Name:        "probe",
				Version:     1,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
				Execute:     func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil },
			})
			for i := range nodes {
				if nodes[i].Name == "workflow_join" {
					execute := nodes[i].Execute
					nodes[i].Execute = func(ctx context.Context, raw json.RawMessage) (string, error) {
						calls.Add(1)
						return execute(ctx, raw)
					}
				}
			}
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}
			svc := Service{
				WS:                ws,
				StoreDir:          t.TempDir(),
				Registry:          registry,
				AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil },
			}
			var definition workflow.Definition
			err = json.Unmarshal([]byte(joinRecipe), &definition)
			if err != nil {
				t.Fatal(err)
			}
			definition.Outputs = nil
			switch mode {
			case "review":
				definition.Steps = append(definition.Steps, workflow.Step{ID: "publish", Tool: "probe", Review: true, Args: json.RawMessage(`{"pairs":{"$step":"join"}}`)})
			case "skip":
				definition.Steps[1].When = json.RawMessage(`false`)
			case "failure":
				definition.Steps[0].Args = json.RawMessage(`{"rows":[{"id":null}]}`)
				definition.Steps = append(definition.Steps, workflow.Step{ID: "report", Tool: "probe", OnFailure: "join", Args: json.RawMessage(`{"handled":true}`)})
			}
			raw, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			out, err := svc.RunSource(t.Context(), raw, "join.json", nil)
			if err != nil && mode != "failure" {
				t.Fatal(out, err)
			}
			summary := out
			if mode == "review" {
				if summary.Status != "waiting_review" || calls.Load() != 1 {
					t.Fatal("join did not finish before review", out, calls.Load())
				}
				restarted := svc
				out, err = restarted.Review(t.Context(), summary.RunID, "publish", true)
				if err != nil || calls.Load() != 1 || out.Status != "completed" {
					t.Fatal("continuation repeated a completed join", out, calls.Load(), err)
				}
			}
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			saved, err := db.Run(summary.RunID)
			if err != nil {
				t.Fatal(err)
			}
			join := saved.Record.Steps[2]
			if mode == "skip" && (calls.Load() != 0 || join.Status != "skipped") {
				t.Fatal("skipped dependency became an empty join input", join, calls.Load())
			}
			if mode == "failure" {
				route := saved.Record.Steps[3]
				if join.Failure == nil || join.Failure.Code != "join_failed" || route.Status != "completed" {
					t.Fatal("join failure was not routed", join, route)
				}
			}
		})
	}
}
