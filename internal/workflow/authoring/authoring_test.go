package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/nodes/script"
	"github.com/keshon/gektor/internal/workflow/service"
)

type model struct {
	response llm.ChatResponse
	err      error
	calls    int
	request  llm.ChatRequest
	deadline time.Time
}

func (m *model) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	m.calls++
	m.request = request
	m.deadline, _ = ctx.Deadline()
	return m.response, m.err
}

func TestProposalIsBoundedAndToolFree(t *testing.T) {
	client := &model{response: llm.ChatResponse{Message: llm.Message{Content: `{"recipe_json":"{}","note":"Review this"}`}}}
	request := Request{Source: "original draft", Change: "requested change", MaxTokens: 400, Temperature: 0.2}
	proposal, err := Propose(context.Background(), client, request)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Source != "{}" || proposal.Note != "Review this" || client.calls != 1 {
		t.Fatal(proposal, client.calls)
	}
	if client.request.MaxTokens != 400 || len(client.request.Tools) != 0 || client.request.JSONSchema == "" || client.request.Grammar == "" {
		t.Fatal("request lost its output constraints")
	}
	if client.deadline.IsZero() || time.Until(client.deadline) > 2*time.Minute {
		t.Fatal("missing request deadline")
	}
	content := client.request.Messages[1].Content
	if !strings.Contains(content, request.Source) || !strings.Contains(content, request.Change) {
		t.Fatal("draft snapshot or requested change missing")
	}
}

func TestProposalRejectsInvalidEnvelopes(t *testing.T) {
	cases := []string{
		`{}`,
		`{"recipe_json":"{}"}`,
		`{"recipe_json":"{}","note":"","extra":true}`,
		`{"recipe_json":{},"note":""}`,
		`{"recipe_json":"{}","note":""} {}`,
	}
	oversized, err := json.Marshal(struct {
		Recipe string `json:"recipe_json"`
		Note   string `json:"note"`
	}{Recipe: strings.Repeat("x", (64<<10)+1)})
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, string(oversized))
	for _, content := range cases {
		client := &model{response: llm.ChatResponse{Message: llm.Message{Content: content}}}
		_, err = Propose(context.Background(), client, Request{})
		if err == nil {
			t.Fatalf("accepted invalid envelope: %.80s", content)
		}
	}
}

func TestProposalBudgetAndProviderErrors(t *testing.T) {
	client := &model{}
	_, err := Propose(context.Background(), client, Request{ContextLimit: 1})
	if err == nil || client.calls != 0 {
		t.Fatal("over-budget request reached provider", err)
	}
	client.err = context.Canceled
	_, err = Propose(context.Background(), client, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	client.err = nil
	client.response.FinishReason = "length"
	_, err = Propose(context.Background(), client, Request{})
	if err == nil {
		t.Fatal("accepted truncated response")
	}
	client.response.FinishReason = "stop"
	client.response.Message.ToolCalls = []llm.ToolCall{{}}
	_, err = Propose(context.Background(), client, Request{})
	if err == nil {
		t.Fatal("accepted unexpected tool call")
	}
}

func TestDraftValidationUsesRegisteredRecipeSchema(t *testing.T) {
	registry, err := workflow.NewRegistry(script.Definition())
	if err != nil {
		t.Fatal(err)
	}
	schema := (service.Service{Registry: registry}).DefinitionSchema()
	source := []byte(`{"version":4,"name":"Review","steps":[{"id":"transform","tool":"script_transform","args":{"input":1,"code":"return input"}}]}`)
	definition, err := Validate(source, schema)
	if err != nil || definition.Name != "Review" {
		t.Fatal(definition, err)
	}
	_, err = Validate([]byte(`{"version":4,"name":"Review","steps":[{"id":"transform","tool":"script_transform","args":{"input":1,"code":"return input"}}],"unknown":true}`), schema)
	if err == nil {
		t.Fatal("unknown recipe field accepted")
	}
}
