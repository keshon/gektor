// Package contract shares the public-post operation across frontends.
package contract

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/workflow"
)

const Name = "social_read_post"
const AgentName = "cap__social__read_post"

type Operation struct{ Service social.Service }

func (Operation) Name() string   { return Name }
func (Operation) ReadOnly() bool { return true }

// RetrySafe declares whether a failed attempt may be repeated automatically.
func (Operation) RetrySafe() bool { return true }

func (Operation) Description() string {
	return "Read one public X/Twitter post by HTTPS URL. Returns author, text, dates, media references and explicit access status."
}
func (Operation) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)
}
func (Operation) OutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"},"url":{"type":"string"},"author":{"type":"string"},"handle":{"type":"string"},"text":{"type":"string"},"created_at":{"type":"string"},"media":{"type":"array","items":{"type":"string"}},"retrieved_at":{"type":"string"},"source":{"type":"string"},"truncated":{"type":"boolean"}},"required":["status","retrieved_at","source","truncated"],"additionalProperties":false}`)
}
func (o Operation) parse(raw json.RawMessage) (social.Request, error) {
	var in social.Request
	if err := workflow.ValidateJSON(raw, o.Schema()); err != nil {
		return in, err
	}

	if err := json.Unmarshal(raw, &in); err != nil {
		return in, err
	}

	return o.Service.Validate(in)
}
func (o Operation) Validate(raw json.RawMessage) error { _, err := o.parse(raw); return err }
func (o Operation) Resources(raw json.RawMessage) ([]string, error) {
	in, err := o.parse(raw)
	if err != nil {
		return nil, err
	}

	return []string{in.URL}, nil
}
func (o Operation) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	in, err := o.parse(raw)
	if err != nil {
		return "", err
	}

	result, runErr := o.Service.Read(ctx, in)
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}

	return string(data), runErr
}
