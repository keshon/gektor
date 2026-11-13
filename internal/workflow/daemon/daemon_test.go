package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestServiceCancellationAndRunLink(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	directory := t.TempDir()
	s, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	started := make(chan struct{})
	registry, err := workflow.NewRegistry(workflow.Node{Name: "wait", Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: func(ctx context.Context, _ json.RawMessage) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Enqueue(t.Context(), "recipe.json", json.RawMessage(`{"version":4,"name":"cancel","steps":[{"id":"wait","tool":"wait","args":{}}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, s, service.Service{WS: ws, StoreDir: directory, Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("job never started")
	}
	if err := s.CancelJob(t.Context(), job.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		jobs, err := s.Jobs(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if jobs[0].Status == "cancelled" {
			if jobs[0].RunID == "" {
				t.Fatal("missing run linkage")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal(jobs)
		case <-time.After(20 * time.Millisecond):
		}
	}
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
