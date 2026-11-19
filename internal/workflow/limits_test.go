package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedRegistrySharesSlotsAndSerializesUnsafeOperations(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			started := make(chan struct{}, 12)
			release := make(chan struct{})
			var active, peak atomic.Int32
			node := Node{
				Name:        "probe",
				Version:     1,
				Concurrent:  concurrent,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
				Execute: func(ctx context.Context, _ json.RawMessage) (string, error) {
					count := active.Add(1)
					defer active.Add(-1)
					for previous := peak.Load(); count > previous; previous = peak.Load() {
						if peak.CompareAndSwap(previous, count) {
							break
						}
					}
					started <- struct{}{}
					select {
					case <-release:
						return `{}`, nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				},
			}
			other := node
			other.Name = "other"
			original, err := NewRegistry(node, other)
			if err != nil {
				t.Fatal(err)
			}
			bounded, err := original.Bounded(2)
			if err != nil {
				t.Fatal(err)
			}
			probe, err := bounded.Node("probe")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			var workers sync.WaitGroup
			t.Cleanup(func() { cancel(); workers.Wait() })
			executors := []Node{probe}
			if concurrent {
				other, nodeErr := bounded.Node("other")
				if nodeErr != nil {
					t.Fatal(nodeErr)
				}
				executors = append(executors, other)
			}
			for i := range 12 {
				operation := executors[i%len(executors)]
				workers.Go(func() {
					_, callErr := operation.Execute(ctx, json.RawMessage(`{}`))
					if callErr != nil && !errors.Is(callErr, context.Canceled) {
						t.Error(callErr)
					}
				})
			}
			want := int32(1)
			if concurrent {
				want = 2
			}
			for range want {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("execution slots were not filled")
				}
			}
			select {
			case <-started:
				t.Fatal("operation exceeded its concurrency limit")
			case <-time.After(30 * time.Millisecond):
			}
			cancel()
			workers.Wait()
			if active.Load() != 0 || peak.Load() != want {
				t.Fatal("executions were not joined or bounded", active.Load(), peak.Load())
			}
			close(release)
			_, err = probe.Execute(t.Context(), json.RawMessage(`{}`))
			if err != nil {
				t.Fatal("cancellation leaked a permit", err)
			}
		})
	}
}

func TestBoundedRegistryRejectsInvalidLimits(t *testing.T) {
	registry := &Registry{nodes: map[string]Node{}}
	for _, limit := range []int{-1, 0, MaxConcurrency + 1} {
		_, err := registry.Bounded(limit)
		if err == nil {
			t.Fatal("invalid operation limit accepted", limit)
		}
	}
}
