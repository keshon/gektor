package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/providers"
)

func TestBackendImageCapabilityGuard(t *testing.T) {
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Images: []string{"shot.png"}}}}
	for _, tc := range []struct {
		vision bool
		err    error
		reject bool
	}{{true, nil, false}, {false, nil, true}, {false, errors.New("unknown"), false}} {
		b := &Backend{model: "test", visionProbe: func(context.Context) (bool, error) { return tc.vision, tc.err }}
		if err := b.checkImages(context.Background(), req); (err != nil) != tc.reject {
			t.Fatalf("%+v: %v", tc, err)
		}
		if err := b.checkImages(context.Background(), llm.ChatRequest{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccountVisionRetriesFailedDiscovery(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("temporary catalog failure")} {
		t.Run(failure.Error(), func(t *testing.T) {
			for _, supported := range []bool{true, false} {
				calls := 0
				backend := &Backend{visionProbe: func(context.Context) (bool, error) {
					calls++
					if calls == 1 {
						return false, failure
					}
					return supported, nil
				}}
				_, err := backend.SupportsVision(t.Context())
				if !errors.Is(err, failure) {
					t.Fatalf("first discovery: %v", err)
				}
				for range 2 {
					vision, err := backend.SupportsVision(t.Context())
					if err != nil || vision != supported {
						t.Fatalf("recovered discovery: vision=%t, err=%v", vision, err)
					}
				}
				if calls != 2 {
					t.Fatalf("successful fact was not cached: %d probes", calls)
				}
			}
		})
	}
}

func TestAccountVisionConcurrentWaiterCanCancel(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	backend := &Backend{visionProbe: func(ctx context.Context) (bool, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := backend.SupportsVision(ctx)
		finished <- err
	}()
	<-entered

	waitCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	_, err := backend.SupportsVision(waitCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting caller ignored its deadline: %v", err)
	}
	close(release)
	err = <-finished
	if err != nil {
		t.Fatal(err)
	}
	vision, err := backend.SupportsVision(t.Context())
	if err != nil || !vision || calls.Load() != 1 {
		t.Fatalf("shared discovery: vision=%t, calls=%d, err=%v", vision, calls.Load(), err)
	}
}

func TestAccountVisionRefreshRecoversLaunchFacts(t *testing.T) {
	calls := 0
	backend := &Backend{
		descriptor: providers.Descriptor{Discovery: providers.AccountModels},
		visionProbe: func(context.Context) (bool, error) {
			calls++
			if calls == 1 {
				return false, errors.New("catalog unavailable during launch")
			}
			return true, nil
		},
	}
	_, err := backend.SupportsVision(t.Context())
	if err == nil {
		t.Fatal("launch unexpectedly discovered vision")
	}

	for range 2 {
		facts := backend.RefreshModelFacts(t.Context())
		if !facts.VisionKnown || !facts.Vision {
			t.Fatalf("next run retained unknown launch capabilities: %+v", facts)
		}
		if facts.ContextKnown || facts.ModelKnown {
			t.Fatalf("account refresh replaced configured model facts: %+v", facts)
		}
	}
	if calls != 2 {
		t.Fatalf("successful account fact was reprobed: %d requests", calls)
	}
}
