package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestCollectionUsesRealAuthorizationAndOutputContracts(t *testing.T) {
	for _, mode := range []string{"collect", "deny", "secondary permission", "storage failure", "invalid output", "unsafe concurrency"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			var calls atomic.Int32
			var authorizing atomic.Int32
			node := workflow.Node{
				Name: "probe", Version: 1, Concurrent: mode != "unsafe concurrency",
				InputSchema:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`),
				OutputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"]}`),
				Resources:    func(raw json.RawMessage) ([]string, error) { return []string{string(raw)}, nil },
				Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
					calls.Add(1)
					if mode == "secondary permission" || mode == "storage failure" {
						return "", errors.New(mode)
					}
					if mode == "invalid output" {
						return `{}`, nil
					}
					if string(raw) == `{"value":1}` {
						return `{"value":1}`, &workflow.ExecutionError{Err: errors.New("site unavailable")}
					}
					return string(raw), nil
				},
			}
			registry, err := workflow.NewRegistry(node)
			if err != nil {
				t.Fatal(err)
			}
			svc := Service{
				WS: ws, StoreDir: t.TempDir(), Registry: registry,
				AuthorizeResource: func(context.Context, string, string, json.RawMessage) error {
					if authorizing.Add(1) != 1 {
						t.Error("concurrent permission gates")
					}
					defer authorizing.Add(-1)
					if mode == "deny" {
						return errors.New("permission denied")
					}
					return nil
				},
			}
			source := []byte(`{"version":4,"name":"Probe","steps":[{"id":"probe","tool":"probe","args":{},"for_each":[{"value":0},{"value":1},{"value":2}],"concurrency":3,"on_error":"collect"}]}`)
			out, err := svc.RunSource(t.Context(), source, "probe.json", nil)
			if mode != "collect" {
				if err == nil {
					t.Fatal("control failure collected", out)
				}
				if (mode == "deny" || mode == "unsafe concurrency") && calls.Load() != 0 {
					t.Fatal("unauthorized or unsafe operation executed", calls.Load())
				}
				return
			}
			if err != nil || calls.Load() != 3 {
				t.Fatal(out, calls.Load(), err)
			}
			var summary struct {
				RunID string `json:"run_id"`
			}
			err = json.Unmarshal([]byte(out), &summary)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			r, err := db.Run(summary.RunID)
			if err != nil || r.Status != "completed" || len(r.Record.Steps[0].Iterations) != 3 {
				t.Fatal(r, err)
			}
			if !strings.Contains(r.Record.Steps[0].Output, `"status":"failed"`) || r.Record.Steps[0].Iterations[1].Error != "site unavailable" {
				t.Fatal("lost failed item", r.Record.Steps[0])
			}
		})
	}
}

func TestCollectionBehaviorRequiresAnExplicitMap(t *testing.T) {
	registry, err := workflow.NewRegistry(workflow.Node{
		Name: "echo", Version: 1, Concurrent: true,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(context.Context, json.RawMessage) (string, error) {
			t.Error("validation executed an operation")
			return `{}`, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, behavior := range []string{`"concurrency":2`, `"on_error":"collect"`} {
		source := fmt.Sprintf(`{"version":4,"name":"Not a map","steps":[{"id":"echo","tool":"echo","args":{},%s}]}`, behavior)
		var d workflow.Definition
		err = json.Unmarshal([]byte(source), &d)
		if err != nil {
			t.Fatal(err)
		}
		h := &handler{registry: registry, templates: true}
		err = h.Validate(d.Steps[0])
		if err == nil || !strings.Contains(err.Error(), "require for_each") {
			t.Fatal("unmapped behavior accepted", err)
		}
	}
}
