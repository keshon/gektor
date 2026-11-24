package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/workflow"
)

type pageSpec struct{ kind, description, schema string }

var pageSpecs = []pageSpec{
	{social.ReadThread, "Read a public X/Twitter post, its thread and one page of replies (recent first). Supply url; follow next_cursor for replies when more=true. Coverage is provider-limited, never a complete conversation guarantee.", `{"type":"object","properties":{"url":{"type":"string"},"cursor":{"type":"string"}},"required":["url"],"additionalProperties":false}`},
	{social.ReadTimeline, "Read one page of public posts from an X/Twitter handle, without @ or a profile URL. Follow next_cursor when more=true. Limit requests a provider page size; more posts may be returned to preserve pagination. This is an account timeline, not a personalized home feed or keyword search.", `{"type":"object","properties":{"handle":{"type":"string"},"cursor":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["handle"],"additionalProperties":false}`},
	{social.ReadTrends, "Read provider-selected X/Twitter trending topic names and context. Trends do not include matching posts, regional guarantees or keyword search. No pagination.", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":20}},"additionalProperties":false}`},
}

func PageKinds() []string {
	kinds := make([]string, 0, len(pageSpecs))
	for _, spec := range pageSpecs {
		kinds = append(kinds, spec.kind)
	}
	return kinds
}

type PageOperation struct {
	Service social.Service
	Kind    string
}

func (o PageOperation) spec() pageSpec {
	for _, spec := range pageSpecs {
		if spec.kind == o.Kind {
			return spec
		}
	}
	return pageSpec{}
}
func (o PageOperation) Name() string                { return "social_" + o.Kind }
func (o PageOperation) AgentName() string           { return "cap__social__" + o.Kind }
func (o PageOperation) Description() string         { return o.spec().description }
func (o PageOperation) Schema() json.RawMessage     { return json.RawMessage(o.spec().schema) }
func (PageOperation) ReadOnly() bool                { return true }
func (PageOperation) RetrySafe() bool               { return true }
func (PageOperation) OutputSchema() json.RawMessage { return json.RawMessage(pageSchema) }

func (o PageOperation) parse(raw json.RawMessage) (social.PageRequest, string, error) {
	var in social.PageRequest
	if o.spec().kind == "" {
		return in, "", fmt.Errorf("unsupported social operation %q", o.Kind)
	}
	err := workflow.ValidateJSON(raw, o.Schema())
	if err != nil {
		return in, "", err
	}
	err = json.Unmarshal(raw, &in)
	if err != nil {
		return in, "", err
	}
	return social.ValidatePage(o.Kind, in)
}
func (o PageOperation) Validate(raw json.RawMessage) error { _, _, err := o.parse(raw); return err }
func (o PageOperation) Resources(raw json.RawMessage) ([]string, error) {
	_, resource, err := o.parse(raw)
	if err != nil {
		return nil, err
	}
	return []string{resource}, nil
}
func (o PageOperation) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	in, _, err := o.parse(raw)
	if err != nil {
		return "", err
	}
	page, runErr := o.Service.Query(ctx, o.Kind, in)
	encoded, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	return string(encoded), runErr
}

const pageSchema = `{"type":"object","properties":{"status":{"type":"string"},"post":{"$ref":"#/$defs/post"},"posts":{"type":"array","items":{"$ref":"#/$defs/post"}},"thread":{"type":"array","items":{"$ref":"#/$defs/post"}},"replies":{"type":"array","items":{"$ref":"#/$defs/post"}},"trends":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"rank":{"type":"string"},"context":{"type":"string"}},"required":["name"],"additionalProperties":false}},"next_cursor":{"type":"string"},"more":{"type":"boolean"},"truncated":{"type":"boolean"},"source":{"type":"string"},"retrieved_at":{"type":"string"}},"required":["status","more","truncated","source","retrieved_at"],"additionalProperties":false,"$defs":{"post":` + postSchema + `}}`
