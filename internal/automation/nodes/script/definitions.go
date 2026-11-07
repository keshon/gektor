package script

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	runtime "github.com/keshon/gektor/internal/automation/script"
)

func Definition() automation.Node {
	return automation.Node{Name: "script_transform", RetrySafe: true, Version: 1, Description: "Transform explicit JSON input with JavaScript. No file, network, environment or module APIs. Returns exactly the script value as JSON, without an output wrapper.", ReadOnly: true,
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
			return runtime.Execute(ctx, r)
		}}
}
