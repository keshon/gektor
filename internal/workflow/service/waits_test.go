package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestDurableWaitsResumeRootChildMapAndRepeat(t *testing.T) {
	for _, mode := range []string{"root", "child", "map", "repeat", "review", "run denied", "operation denied"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()

			before, after := 0, 0
			resuming := false
			nodes := callnode.Definitions()
			nodes = append(nodes, workflow.Node{
				Name: "probe", Version: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`),
				Resources: func(raw json.RawMessage) ([]string, error) { return []string{string(raw)}, nil },
				Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
					if strings.Contains(string(raw), "before") {
						before++
						return `{}`, nil
					}
					after++
					return fmt.Sprintf(`{"done":%t,"next":{"cursor":%d}}`, after == 3, after), nil
				},
			})
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}

			svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: registry,
				Authorize: func(context.Context, string, json.RawMessage) error { return nil },
				AuthorizeResource: func(_ context.Context, name, _ string, raw json.RawMessage) error {
					if resuming && ((mode == "run denied" && name == "workflow_run") || (mode == "operation denied" && name == "probe")) {
						return fmt.Errorf("permission revoked")
					}
					return nil
				},
			}

			body := `[{"id":"before","tool":"probe","args":{"label":"before"}},{"id":"delay","tool":"workflow_wait","args":{"delay_ms":0}},{"id":"after","tool":"probe","args":{"label":"after"},"depends_on":["delay"]}]`
			if mode == "review" {
				body = strings.Replace(body, `"id":"delay"`, `"id":"delay","review":true`, 1)
			}
			source := `{"version":4,"name":"Wait","steps":` + body + `}`
			waits := 1
			if mode == "child" || mode == "map" || mode == "repeat" {
				child := `{"version":4,"name":"Child","inputs":{"cursor":{"type":"integer","default":0}},"steps":` + body + `,"outputs":{"done":{"$step":"after","pointer":"/done"},"next":{"$step":"after","pointer":"/next"}}}`
				err = ws.WriteFile("child.json", []byte(child), 0600)
				if err != nil {
					t.Fatal(err)
				}
				policy := ""
				if mode == "map" {
					policy, waits = `,"for_each":[{"cursor":0},{"cursor":1}]`, 2
				}
				if mode == "repeat" {
					policy, waits = `,"repeat":{"max_iterations":3,"until":"/done","next_inputs":"/next"}`, 3
				}
				source = `{"version":4,"name":"Caller","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"child.json"}` + policy + `}]}`
			}

			out, err := svc.RunSource(t.Context(), []byte(source), "root.json", nil)
			if err != nil {
				t.Fatal(out, err)
			}
			var summary struct {
				ID     string `json:"run_id"`
				Status string `json:"status"`
			}
			err = json.Unmarshal([]byte(out), &summary)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "review" {
				_, err = svc.Review(t.Context(), summary.ID, "delay", true)
				if err != nil {
					t.Fatal(err)
				}
			}
			// Reopening uses only persisted state, including nested wait checkpoints.
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for n := 0; n < waits; n++ {
				state, readErr := db.Run(summary.ID)
				if readErr != nil || state.Status != "waiting" || state.Record.WakeAt == nil || before != n+1 || after != n {
					t.Fatalf("wait=%d before=%d after=%d status=%s err=%v", n, before, after, state.Status, readErr)
				}
				writer, record, ok, claimErr := db.ClaimWait(t.Context(), time.Now())
				if claimErr != nil || !ok {
					t.Fatal("wait not claimable", claimErr)
				}
				duplicate, _, claimed, duplicateErr := db.ClaimWait(t.Context(), time.Now())
				if duplicate != nil {
					duplicate.Close()
				}
				if duplicateErr != nil || claimed {
					writer.Close()
					t.Fatal("wait claimed twice", duplicateErr)
				}
				resuming = true
				err = svc.ContinueWait(t.Context(), db, writer, record)
				closeErr := writer.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}

			state, err := db.Run(summary.ID)
			wantStatus, wantAfter := "completed", waits
			if strings.Contains(mode, "denied") {
				wantStatus, wantAfter = "failed", 0
			}
			if err != nil || state.Status != wantStatus || after != wantAfter || before != waits || state.Record.WakeAt != nil {
				t.Fatalf("before=%d after=%d status=%s err=%v", before, after, state.Status, err)
			}
			duplicate, _, claimed, claimErr := db.ClaimWait(t.Context(), time.Now())
			if duplicate != nil {
				duplicate.Close()
			}
			if claimErr != nil || claimed {
				t.Fatal("terminal run retained its wake-up", claimErr)
			}
		})
	}
}

func TestWaitRejectsInvalidLimitsMapsAndRetries(t *testing.T) {
	registry, err := workflow.NewRegistry(callnode.Definitions()...)
	if err != nil {
		t.Fatal(err)
	}
	h := handler{registry: registry, templates: true}
	for _, raw := range []string{
		`{"tool":"workflow_wait","args":{"delay_ms":-1}}`,
		`{"tool":"workflow_wait","args":{"delay_ms":604800001}}`,
		`{"tool":"workflow_wait","args":{"delay_ms":1.5}}`,
		`{"tool":"workflow_wait","args":{}}`,
		`{"tool":"workflow_wait","args":{},"for_each":[{"delay_ms":1}]}`,
		`{"tool":"workflow_wait","args":{"delay_ms":1},"retry":{"max_attempts":1}}`,
	} {
		var step workflow.Step
		err = json.Unmarshal([]byte(raw), &step)
		if err != nil {
			t.Fatal(err)
		}
		err = h.Validate(step)
		if err == nil {
			t.Fatal("invalid wait accepted", raw)
		}
	}
}
