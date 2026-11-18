// Package authoring prepares model proposals without saving recipes or executing nodes.
package authoring

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

// Request is a snapshot of the draft and its operation contracts.
type Request struct {
	Source           string
	Change           string
	DefinitionSchema json.RawMessage
	Contracts        map[string]workflow.Contract
	MaxTokens        int
	ContextLimit     int
	Temperature      float64
}

type Proposal struct {
	Source string
	Note   string
}

// A small envelope works with strict remote schemas and local GBNF alike.
// The contained recipe is checked against the registry before it can be applied.
const proposalSchema = `{"type":"object","properties":{"recipe_json":{"type":"string"},"note":{"type":"string"}},"required":["recipe_json","note"],"additionalProperties":false}`
const proposalGrammar = `root ::= "{" ws "\"recipe_json\"" ws ":" ws string ws "," ws "\"note\"" ws ":" ws string ws "}"
string ::= "\"" ([^"\\\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
ws ::= [ \t\n\r]*`

// Propose borrows the client for one bounded request. It never supplies tools.
// The caller reviews the returned recipe before applying or saving it.
func Propose(ctx context.Context, client llm.Client, in Request) (Proposal, error) {
	if client == nil {
		return Proposal{}, fmt.Errorf("model unavailable")
	}
	contracts, err := json.Marshal(in.Contracts)
	if err != nil {
		return Proposal{}, err
	}

	output := 8192
	if in.MaxTokens > 0 {
		output = min(output, in.MaxTokens)
	}
	request := llm.ChatRequest{
		MaxTokens:   output,
		Temperature: in.Temperature,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: prompts.WorkflowEdit + "\n\n" + workspace.WorkflowGuide},
			{Role: llm.RoleUser, Content: "Definition schema:\n" + string(in.DefinitionSchema) + "\nOperation contracts:\n" + string(contracts) + "\nCurrent draft:\n" + in.Source + "\nRequested change:\n" + in.Change},
		},
		JSONSchema: proposalSchema,
		Grammar:    proposalGrammar,
	}
	if in.ContextLimit > 0 && llm.EstimatePromptTokens(request)+output > in.ContextLimit {
		return Proposal{}, fmt.Errorf("proposal exceeds the model context limit")
	}

	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := client.Chat(requestCtx, request)
	if err != nil {
		return Proposal{}, err
	}
	if response.FinishReason == "length" || len(response.Message.ToolCalls) > 0 {
		return Proposal{}, fmt.Errorf("model returned incomplete output or an unexpected tool call")
	}

	raw := []byte(response.Message.Content)
	if len(raw) > 512<<10 {
		return Proposal{}, fmt.Errorf("proposal envelope exceeds 512 KiB")
	}
	err = workflow.ValidateJSON(raw, json.RawMessage(proposalSchema))
	if err != nil {
		return Proposal{}, err
	}
	var envelope struct {
		Recipe string `json:"recipe_json"`
		Note   string `json:"note"`
	}
	err = json.Unmarshal(raw, &envelope)
	if err != nil {
		return Proposal{}, err
	}
	if len(envelope.Recipe) > 64<<10 || len(envelope.Note) > 4096 {
		return Proposal{}, fmt.Errorf("proposal recipe exceeds 64 KiB or note exceeds 4 KiB")
	}

	return Proposal{Source: envelope.Recipe, Note: envelope.Note}, nil
}

// Validate checks the draft shape without requiring runtime inputs or executing nodes.
func Validate(source []byte, schema json.RawMessage) (workflow.Definition, error) {
	err := workflow.ValidateJSON(source, schema)
	if err != nil {
		return workflow.Definition{}, err
	}
	var definition workflow.Definition
	err = json.Unmarshal(source, &definition)
	return definition, err
}
