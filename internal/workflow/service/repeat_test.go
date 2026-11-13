package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

const repeatSource = `{"version":4,"name":"Pages","steps":[{"id":"pages","tool":"workflow_call","args":{"recipe_path":"page.json","inputs":{"cursor":0}},"repeat":{"max_iterations":3,"until":"/done","next_inputs":"/next"}}]}`
const pageSource = `{"version":4,"name":"Page","inputs":{"cursor":{"type":"integer","required":true}},"steps":[{"id":"fetch","tool":"probe","args":{"cursor":{"$input":"cursor"}}}],"outputs":{"done":{"$step":"fetch","pointer":"/done"},"next":{"$step":"fetch","pointer":"/next"}}}`

func TestRepeatReauthorizesEachCallAndValidatesNextInputs(t *testing.T) {
	for _, mode := range []string{"success", "call denied", "operation denied", "invalid next", "reference injection"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			err = ws.WriteFile("page.json", []byte(pageSource), 0600)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			nodes := callnode.Definitions()
			nodes = append(nodes, workflow.Node{
				Name: "probe", Version: 1,
				InputSchema:  json.RawMessage(`{"type":"object","properties":{"cursor":{"type":"integer"}},"required":["cursor"],"additionalProperties":false}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
				Resources:    func(raw json.RawMessage) ([]string, error) { return []string{string(raw)}, nil },
				Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
					want := fmt.Sprintf(`{"cursor":%d}`, calls)
					if string(raw) != want {
						t.Fatalf("args=%s want=%s", raw, want)
					}
					calls++
					if mode == "invalid next" {
						return `{"done":false,"next":{"cursor":"bad"}}`, nil
					}
					if mode == "reference injection" {
						return `{"done":false,"next":{"cursor":{"$step":"fetch"}}}`, nil
					}
					return fmt.Sprintf(`{"done":%t,"next":{"cursor":%d}}`, calls == 3, calls), nil
				},
			})
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}
			svc := Service{
				WS: ws, StoreDir: t.TempDir(), Registry: registry,
				Authorize: func(context.Context, string, json.RawMessage) error { return nil },
				AuthorizeResource: func(_ context.Context, name, _ string, _ json.RawMessage) error {
					if calls == 1 && ((mode == "call denied" && name == "workflow_call") || (mode == "operation denied" && name == "probe")) {
						return fmt.Errorf("permission revoked")
					}
					return nil
				},
			}
			out, runErr := svc.RunSource(t.Context(), []byte(repeatSource), "root.json", nil)
			wantCalls, wantStatus := 3, "completed"
			if mode != "success" {
				wantCalls, wantStatus = 1, "failed"
			}
			if calls != wantCalls || (runErr != nil) != (mode != "success") {
				t.Fatalf("calls=%d output=%s err=%v", calls, out, runErr)
			}
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var summary struct {
				ID string `json:"run_id"`
			}
			err = json.Unmarshal([]byte(out), &summary)
			if err != nil {
				t.Fatal(out, err)
			}
			state, err := db.Run(summary.ID)
			if err != nil || state.Record.Status != wantStatus || state.Record.Steps[0].Iterations[0].Status != "completed" {
				t.Fatalf("status=%s err=%v", state.Record.Status, err)
			}
		})
	}
}

func TestRepeatRejectsInvalidPolicyBeforeEffects(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	err = ws.WriteFile("page.json", []byte(pageSource), 0600)
	if err != nil {
		t.Fatal(err)
	}
	nodes := callnode.Definitions()
	nodes = append(nodes, workflow.Node{
		Name: "probe", Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`),
		Resources: func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(context.Context, json.RawMessage) (string, error) {
			t.Fatal("validation executed a node")
			return "", nil
		},
	})
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, Registry: registry, ValidateOnly: true, Authorize: func(context.Context, string, json.RawMessage) error { return nil }}
	for _, change := range []struct{ old, next string }{
		{`"max_iterations":3`, `"max_iterations":0`},
		{`"max_iterations":3`, `"max_iterations":10001`},
		{`"until":"/done"`, `"until":"done"`},
		{`"next_inputs":"/next"`, `"next_inputs":"/~2bad"`},
		{`"next_inputs":"/next"`, `"next_inputs":"/next","delay_ms":10001`},
		{`"next_inputs":"/next"`, `"next_inputs":"/next","unknown":true`},
		{`,"next_inputs":"/next"`, ``},
		{`"repeat":`, `"for_each":[],"repeat":`},
		{`"repeat":`, `"retry":{"max_attempts":1},"repeat":`},
		{`"repeat":`, `"concurrency":1,"repeat":`},
		{`"repeat":`, `"on_error":"stop","repeat":`},
		{`"tool":"workflow_call"`, `"tool":"probe"`},
	} {
		source := strings.Replace(repeatSource, change.old, change.next, 1)
		_, err = svc.RunSource(t.Context(), []byte(source), "root.json", nil)
		if err == nil {
			t.Fatal("invalid repeat accepted", source)
		}
	}
}
