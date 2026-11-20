package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func failureService(t *testing.T) (Service, <-chan string) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	effects := make(chan string, 32)
	nodes := callnode.Definitions()
	nodes = append(nodes, workflow.Node{
		Name: "probe", Version: 1,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`),
		Resources:    func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
			var in struct{ Label string }
			err := json.Unmarshal(raw, &in)
			if err != nil {
				return "", err
			}
			effects <- in.Label
			if in.Label == "bad_output" {
				return `{"label":42}`, nil
			}
			if in.Label == "fail" {
				return `{"partial":true}`, &workflow.ExecutionError{Code: "probe_failed", Err: errors.New("source unavailable")}
			}
			return string(raw), nil
		},
	})
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	return Service{
		WS: ws, StoreDir: t.TempDir(), Registry: registry,
		Authorize:         func(context.Context, string, json.RawMessage) error { return nil },
		AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil },
	}, effects
}

func TestFailureRoutesDoNotBypassPolicyContractsOrCancellation(t *testing.T) {
	for _, mode := range []string{"route denied", "finalizer denied", "cancel", "output contract"} {
		t.Run(mode, func(t *testing.T) {
			svc, effects := failureService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			svc.AuthorizeResource = func(_ context.Context, name, _ string, raw json.RawMessage) error {
				if name != "probe" {
					return nil
				}
				var in struct{ Label string }
				err := json.Unmarshal(raw, &in)
				if err != nil {
					return err
				}
				if (mode == "route denied" && in.Label == "probe_failed") || (mode == "finalizer denied" && in.Label == "finish") {
					return errors.New("permission denied")
				}
				if mode == "cancel" && in.Label == "probe_failed" {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			source := failureRecipe
			if mode == "output contract" {
				source = strings.Replace(source, `"label":"fail"`, `"label":"bad_output"`, 1)
			}
			out, err := svc.RunSource(ctx, []byte(source), "root.json", nil)
			if err == nil {
				t.Fatal("blocked work reported success", out)
			}
			var calls []string
			for len(effects) > 0 {
				calls = append(calls, <-effects)
			}
			want := []string{"fail"}
			if mode == "finalizer denied" {
				want = append(want, "probe_failed")
			}
			if mode == "output contract" {
				want = []string{"bad_output"}
			}
			if !slices.Equal(calls, want) {
				t.Fatal("effects after blocked boundary", calls, want)
			}
		})
	}
}

const failureRecipe = `{"version":4,"name":"Failure","steps":[
{"id":"finish","tool":"probe","finally":true,"args":{"label":"finish"}},
{"id":"after","tool":"probe","depends_on":["fetch"],"args":{"label":"after"}},
{"id":"notify","tool":"probe","on_failure":"fetch","args":{"label":{"$step":"fetch","pointer":"/error/code"}}},
{"id":"fetch","tool":"probe","args":{"label":"fail"}}
],"outputs":{"done":true}}`

func TestFailureRoutingAcrossChildMapRepeatAndCollections(t *testing.T) {
	for _, mode := range []string{"root", "child", "map", "repeat", "collection"} {
		t.Run(mode, func(t *testing.T) {
			svc, effects := failureService(t)
			source := failureRecipe
			if mode == "child" || mode == "map" || mode == "repeat" {
				err := svc.WS.WriteFile("child.json", []byte(source), 0600)
				if err != nil {
					t.Fatal(err)
				}
				policy := ""
				if mode == "map" {
					policy = `,"for_each":[{},{}]`
				}
				if mode == "repeat" {
					policy = `,"repeat":{"max_iterations":2,"until":"/done","next_inputs":"/next"}`
				}
				source = `{"version":4,"name":"Caller","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"child.json"}` + policy + `},{"id":"notice","tool":"probe","on_failure":"call","args":{"label":"parent_notice"}},{"id":"final","tool":"probe","finally":true,"args":{"label":"parent_finish"}}]}`
			}
			if mode == "collection" {
				source = strings.Replace(source, `"id":"fetch","tool":"probe","args":{"label":"fail"}`, `"id":"fetch","tool":"probe","args":{},"for_each":[{"label":"fail"},{"label":"after"}]`, 1)
			}
			out, err := svc.RunSource(t.Context(), []byte(source), "root.json", nil)
			var execution *workflow.ExecutionError
			if !errors.As(err, &execution) || execution.Code != "probe_failed" {
				t.Fatal(out, err)
			}
			summary := out
			if summary.Status != "failed" {
				t.Fatal(out, err)
			}
			db, err := store.OpenStore(svc.StoreDir, svc.WS.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			state, err := db.Run(summary.RunID)
			if err != nil || state.Status != "failed" || state.Record.Finished == nil {
				t.Fatal(state, err)
			}
			want := []string{"fail", "probe_failed", "finish"}
			if mode == "child" || mode == "map" || mode == "repeat" {
				want = append(want, "parent_notice", "parent_finish")
			}
			var calls []string
			for len(effects) > 0 {
				calls = append(calls, <-effects)
			}
			if !slices.Equal(calls, want) {
				t.Fatal("lost or repeated effects", calls, want)
			}
		})
	}
}

func TestFailureRoutesSurviveReviewWaitAndOperatorRecovery(t *testing.T) {
	for _, mode := range []string{"review", "wait", "finalizer wait", "recovery", "callback expiry"} {
		t.Run(mode, func(t *testing.T) {
			svc, effects := failureService(t)
			source := failureRecipe
			if mode == "callback expiry" {
				source = strings.Replace(source, `"id":"fetch","tool":"probe","args":{"label":"fail"}`, `"id":"fetch","tool":"workflow_wait_callback","args":{"channel":"approval","timeout_ms":1}`, 1)
			} else if mode == "wait" {
				source = strings.Replace(source, `"id":"notify","tool":"probe","on_failure":"fetch","args":{"label":{"$step":"fetch","pointer":"/error/code"}}`, `"id":"notify","tool":"workflow_wait","on_failure":"fetch","args":{"delay_ms":0}`, 1)
			} else if mode == "finalizer wait" {
				source = strings.Replace(source, `"id":"finish","tool":"probe","finally":true,"args":{"label":"finish"}`, `"id":"finish","tool":"workflow_wait","finally":true,"args":{"delay_ms":0}`, 1)
			} else {
				source = strings.Replace(source, `"id":"notify"`, `"id":"notify","review":true`, 1)
			}
			out, err := svc.RunSource(t.Context(), []byte(source), "root.json", nil)
			if err != nil {
				t.Fatal(out, err)
			}
			summary := out
			db, err := store.OpenStore(svc.StoreDir, svc.WS.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if mode == "recovery" {
				writer, r, editErr := db.EditRun(summary.RunID)
				if editErr != nil {
					t.Fatal(editErr)
				}
				r.Status = "running"
				for i := range r.Steps {
					if r.Steps[i].Status == "waiting_review" {
						r.Steps[i].Status = "pending"
					}
				}
				err = writer.Save(r)
				closeErr := writer.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				out, err = svc.Recover(t.Context(), summary.RunID, workflow.Recovery{Note: "Stopped before error notification"})
				if err != nil {
					t.Fatal(out, err)
				}
			}
			if mode == "review" || mode == "recovery" {
				out, err = svc.Review(t.Context(), summary.RunID, "notify", true)
				if err == nil {
					t.Fatal("review erased original failure", out)
				}
			} else {
				state, readErr := db.Run(summary.RunID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				timer := time.NewTimer(time.Until(*state.Record.WakeAt))
				<-timer.C
				writer, r, ok, claimErr := db.ClaimWait(t.Context(), time.Now())
				if claimErr != nil || !ok {
					t.Fatal(ok, claimErr)
				}
				err = svc.ContinueWait(t.Context(), db, writer, r)
				closeErr := writer.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
			state, err := db.Run(summary.RunID)
			if err != nil || state.Status != "failed" || state.Record.Steps[0].Failure == nil {
				t.Fatal(state, err)
			}
			var calls []string
			for len(effects) > 0 {
				calls = append(calls, <-effects)
			}
			want := []string{"fail", "probe_failed", "finish"}
			if mode == "wait" {
				want = []string{"fail", "finish"}
			}
			if mode == "finalizer wait" {
				want = []string{"fail", "probe_failed"}
			}
			if mode == "callback expiry" {
				want = []string{"callback_expired", "finish"}
			}
			if !slices.Equal(calls, want) {
				t.Fatal("failed work replayed or route lost", calls, want)
			}
		})
	}
}
