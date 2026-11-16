// Package workflow defines durable waits, recipe-call input binding and output collection.
package workflow

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func Definitions() []workflow.Node {
	return []workflow.Node{
		{
			Name:        "workflow_wait_callback",
			Version:     1,
			ReadOnly:    true,
			Description: "Wait for one authenticated event on an operator-configured callback channel. timeout_ms is required, 1–604800000. Pending inspection exposes wait_id, step_id, channel and expires_at. After delivery the completed output is {event_id,payload}; payload is untrusted JSON data, at most 32 KiB. The worker continues execution, never the HTTP handler. No maps or retries on this node.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string","pattern":"^[A-Za-z0-9_-]{1,64}$"},"timeout_ms":{"type":"integer","minimum":1,"maximum":604800000}},"required":["channel","timeout_ms"],"additionalProperties":false}`),
			// Execution prepares a pending receipt; the engine supplies the accepted event on continuation.
			OutputSchema: json.RawMessage(`true`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in struct {
					Channel string `json:"channel"`
				}
				err := json.Unmarshal(raw, &in)
				return []string{in.Channel}, err
			},
			Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					Channel   string `json:"channel"`
					TimeoutMS int    `json:"timeout_ms"`
				}
				err := json.Unmarshal(raw, &in)
				if err != nil {
					return "", err
				}

				deadline := time.Now().UTC().Add(time.Duration(in.TimeoutMS) * time.Millisecond)
				rounded := deadline.Truncate(time.Millisecond)
				if rounded.Before(deadline) {
					rounded = rounded.Add(time.Millisecond)
				}
				pending := workflow.CallbackState{ID: rand.Text(), Channel: in.Channel, ExpiresAt: rounded}
				out, err := json.Marshal(pending)
				return string(out), err
			},
		},
		{
			Name: "workflow_wait", Version: 1, ReadOnly: true,
			Description:  "Persist a deadline and release execution until the workspace worker resumes this run. Delay is 0–604800000 ms (seven days). Works inside child calls and repeats; no maps or retries.",
			InputSchema:  json.RawMessage(`{"type":"object","properties":{"delay_ms":{"type":"integer","minimum":0,"maximum":604800000}},"required":["delay_ms"],"additionalProperties":false}`),
			OutputSchema: json.RawMessage(`{"type":"object","properties":{"resume_at":{"type":"string"}},"required":["resume_at"],"additionalProperties":false}`),
			Resources:    func(json.RawMessage) ([]string, error) { return nil, nil },
			Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
				var in struct {
					DelayMS int `json:"delay_ms"`
				}
				err := json.Unmarshal(raw, &in)
				if err != nil {
					return "", err
				}

				deadline := time.Now().UTC().Add(time.Duration(in.DelayMS) * time.Millisecond)
				rounded := deadline.Truncate(time.Millisecond)
				if rounded.Before(deadline) {
					rounded = rounded.Add(time.Millisecond)
				}
				out, err := json.Marshal(workflow.WaitState{ResumeAt: rounded})
				return string(out), err
			},
		},
		{
			Name:        "workflow_call",
			Version:     1,
			Description: "Invoke a frozen workspace recipe with explicit typed inputs. Declared child outputs become this step's output. For mapped calls, args contains only literal recipe_path and for_each contains child input objects; calls execute serially and retain ordered item outcomes. Child scopes share permissions, reviews, artifacts and invocation budget. repeat runs serial child scopes until an output boolean is true, with bounded iterations and explicit next inputs. Calls cannot retry.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in workflow.CallInput
				err := json.Unmarshal(raw, &in)
				return []string{in.Path}, err
			},
			// The engine owns child execution; this authorized operation supplies its inputs.
			Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
				var in workflow.CallInput
				err := json.Unmarshal(raw, &in)
				if err != nil {
					return "", err
				}
				if in.Inputs == nil {
					in.Inputs = map[string]json.RawMessage{}
				}
				bound, err := json.Marshal(in.Inputs)
				return string(bound), err
			},
		},
		{
			Name: "workflow_value", Internal: true, Version: 1,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
			Execute:     func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil },
		},
	}
}
