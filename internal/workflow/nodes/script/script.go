// Package script adapts isolated JavaScript transforms to workflow nodes.
package script

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/subprocess"
	"github.com/keshon/gektor/internal/workflow"
	runtime "github.com/keshon/gektor/internal/workflow/script"
)

func Definition() workflow.Node {
	return workflow.Node{
		Name:        "script_transform",
		RetrySafe:   true,
		Concurrent:  true,
		Version:     1,
		Description: "Transform explicit JSON input with JavaScript. No file, network, environment or module APIs. Returns exactly the script value as JSON, without an output wrapper.",
		ReadOnly:    true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","minLength":1,"maxLength":65536},"input":{}},"required":["code","input"],"additionalProperties":false}`),
		Resources: func(args json.RawMessage) ([]string, error) {
			var r runtime.Request
			if err := json.Unmarshal(args, &r); err != nil {
				return nil, err
			}

			return []string{fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(r.Code)))}, nil
		},
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var r runtime.Request
			if err := json.Unmarshal(args, &r); err != nil {
				return "", err
			}

			output, err := runtime.Execute(ctx, r)
			var failure *subprocess.WorkerError
			if errors.As(err, &failure) {
				return output, &workflow.ExecutionError{Err: err}
			}
			return output, err
		},
	}
}
