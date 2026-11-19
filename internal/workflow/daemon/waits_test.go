package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWaitReleasesWorkerAndSurvivesRestart(t *testing.T) {
	for _, mode := range []string{"resume", "cancel", "callback", "callback cancel"} {
		callback := strings.HasPrefix(mode, "callback")
		cancelWait := strings.Contains(mode, "cancel")
		name := mode
		if cancelWait {
			name = mode
		}
		t.Run(name, func(t *testing.T) {
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

			effects := make(chan string, 8)
			nodes := callnode.Definitions()
			nodes = append(nodes, workflow.Node{
				Name: "probe", Version: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]}`),
				Resources: func(json.RawMessage) ([]string, error) { return nil, nil },
				Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
					var in struct{ Label string }
					decodeErr := json.Unmarshal(raw, &in)
					if decodeErr != nil {
						return "", decodeErr
					}
					effects <- in.Label
					return `{}`, nil
				},
			})
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}

			svc := service.Service{WS: ws, StoreDir: base, Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
			source := `{"version":4,"name":"Wait","steps":[{"id":"before","tool":"probe","args":{"label":"before"}},{"id":"delay","tool":"workflow_wait","args":{"delay_ms":2000}},{"id":"after","tool":"probe","depends_on":["delay"],"args":{"label":"after"}}]}`
			if cancelWait {
				source = strings.Replace(source, `"delay_ms":2000`, `"delay_ms":604800000`, 1)
			}
			if callback {
				source = strings.ReplaceAll(source, `"tool":"workflow_wait","args":{"delay_ms":2000}`, `"tool":"workflow_wait_callback","args":{"channel":"approval","timeout_ms":60000}`)
				source = strings.ReplaceAll(source, `"tool":"workflow_wait","args":{"delay_ms":604800000}`, `"tool":"workflow_wait_callback","args":{"channel":"approval","timeout_ms":60000}`)
			}
			job, err := db.Enqueue(t.Context(), "wait.json", json.RawMessage(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			other, err := db.Enqueue(t.Context(), "other.json", json.RawMessage(`{"version":4,"name":"Other","steps":[{"id":"other","tool":"probe","args":{"label":"other"}}]}`), nil)
			if err != nil {
				t.Fatal(err)
			}

			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			done := make(chan error, 1)
			go func() { done <- Serve(ctx, db, svc, Config{}) }()
			running := true
			defer func() {
				if running {
					stop()
					<-done
				}
			}()
			for _, want := range []string{"before", "other"} {
				select {
				case got := <-effects:
					if got != want {
						t.Fatalf("effect=%s want=%s", got, want)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("waiting run held the execution slot")
				}
			}
			otherDeadline := time.After(10 * time.Second)
			for {
				jobs, readErr := db.Jobs(t.Context())
				if readErr != nil {
					t.Fatal(readErr)
				}
				completed := false
				for _, candidate := range jobs {
					if candidate.ID == other.ID && candidate.Status == "completed" {
						completed = true
					}
				}
				if completed {
					break
				}
				select {
				case <-otherDeadline:
					t.Fatal("other job did not finish", jobs)
				case <-time.After(10 * time.Millisecond):
				}
			}
			stop()
			workerErr := <-done
			running = false
			if !errors.Is(workerErr, context.Canceled) {
				t.Fatal(workerErr)
			}
			if callback {
				jobs, readErr := db.Jobs(t.Context())
				if readErr != nil {
					t.Fatal(readErr)
				}
				for _, candidate := range jobs {
					if candidate.ID != job.ID {
						continue
					}
					state, stateErr := db.Run(candidate.RunID)
					if stateErr != nil {
						t.Fatal(stateErr)
					}
					receipt := workflow.PendingCallback(state.Record)
					if receipt == nil {
						t.Fatal("missing pending receipt")
					}
					_, deliveryErr := db.AcceptCallback(t.Context(), store.CallbackDelivery{RunID: candidate.RunID, WaitID: receipt.ID, Channel: receipt.Channel, EventID: "approval", Payload: json.RawMessage(`true`)})
					if deliveryErr != nil {
						t.Fatal(deliveryErr)
					}
				}
			}
			if cancelWait {
				err = db.CancelJob(t.Context(), job.ID)
				if err != nil {
					t.Fatal(err)
				}
			}

			// A fresh worker must recover the durable deadline rather than replay the recipe.
			ctx, stop = context.WithCancel(t.Context())
			defer stop()
			running = true
			go func() { done <- Serve(ctx, db, svc, Config{}) }()
			deadline := time.After(10 * time.Second)
			for {
				jobs, readErr := db.Jobs(t.Context())
				if readErr != nil {
					t.Fatal(readErr)
				}
				status := ""
				for _, candidate := range jobs {
					if candidate.ID == job.ID {
						status = candidate.Status
					}
				}
				want := "completed"
				if cancelWait {
					want = "cancelled"
				}
				if status == want {
					break
				}
				select {
				case <-deadline:
					t.Fatal("wait did not finish", jobs)
				case <-time.After(20 * time.Millisecond):
				}
			}

			if !cancelWait {
				select {
				case got := <-effects:
					if got != "after" {
						t.Fatal("replayed an earlier effect", got)
					}
				default:
					t.Fatal("missing resumed effect")
				}
			}
			select {
			case got := <-effects:
				t.Fatal("duplicate or cancelled effect", got)
			default:
			}
		})
	}
}
