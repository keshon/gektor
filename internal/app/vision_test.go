package app

import (
	"context"
	"errors"
	"github.com/keshon/gektor/internal/llm"
	"testing"
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
