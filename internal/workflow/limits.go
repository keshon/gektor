package workflow

import (
	"context"
	"encoding/json"
	"fmt"
)

// Bounded copies the registry and owns shared execution slots for its callers.
// Non-concurrent operations are serialized by name. The original is unchanged.
// This limits execution in this process, not independent CLI processes.
func (r *Registry) Bounded(maxActive int) (*Registry, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow registry unavailable")
	}
	if maxActive < 1 || maxActive > MaxConcurrency {
		return nil, fmt.Errorf("active operations must be between 1 and %d", MaxConcurrency)
	}
	slots := make(chan struct{}, maxActive)
	bounded := &Registry{nodes: make(map[string]Node, len(r.nodes))}
	for name, node := range r.nodes {
		execute := node.Execute
		var serial chan struct{}
		if !node.Concurrent {
			serial = make(chan struct{}, 1)
		}
		node.Execute = func(ctx context.Context, raw json.RawMessage) (string, error) {
			if serial != nil {
				err := acquireExecution(ctx, serial)
				if err != nil {
					return "", err
				}
				defer func() { <-serial }()
			}
			err := acquireExecution(ctx, slots)
			if err != nil {
				return "", err
			}
			defer func() { <-slots }()
			return execute(ctx, raw)
		}
		bounded.nodes[name] = node
	}
	return bounded, nil
}

func acquireExecution(ctx context.Context, slots chan struct{}) error {
	err := ctx.Err()
	if err != nil {
		return err
	}
	select {
	case slots <- struct{}{}:
		err = ctx.Err()
		if err != nil {
			<-slots
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
