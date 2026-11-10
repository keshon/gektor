// Package model runs bounded model requests without agent tools or conversation history.
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workflow"
)

type Connection struct {
	Temperature             float64
	Client                  llm.Client
	Name                    string
	MaxTokens, ContextLimit int
}
type Resolver func(context.Context, string) (Connection, error)

var slots = make(chan struct{}, 1)

func Definition(resolve Resolver) workflow.Node {
	schema := json.RawMessage(`{"type":"object","properties":{"profile":{"type":"string","minLength":1},"prompt":{"type":"string","minLength":1,"maxLength":32768},"input":{},"output_schema":{"type":"object"},"max_tokens":{"type":"integer","minimum":1,"maximum":8192},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300}},"required":["profile","prompt"],"additionalProperties":false}`)
	return workflow.Node{Name: "model_generate", Version: 1, Description: "One bounded model call using a workspace profile, explicit prompt/input and optional output JSON schema. No tools or conversation history.", InputSchema: schema, OutputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"model":{"type":"string"},"usage":{"type":"object"},"data":{}},"required":["text","model","usage"]}`), Resources: func(raw json.RawMessage) ([]string, error) {
		var in struct{ Profile string }
		err := json.Unmarshal(raw, &in)
		return []string{in.Profile}, err
	}, Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in struct {
			Profile, Prompt string
			Input           json.RawMessage
			OutputSchema    json.RawMessage `json:"output_schema"`
			MaxTokens       int             `json:"max_tokens"`
			Timeout         int             `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if resolve == nil {
			return "", fmt.Errorf("model resolver unavailable")
		}
		if len(in.OutputSchema) > 0 {
			if _, err := workflow.CompileSchema(in.OutputSchema); err != nil {
				return "", err
			}
		}
		if in.Timeout == 0 {
			in.Timeout = 120
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(in.Timeout)*time.Second)
		defer cancel()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
		conn, err := resolve(ctx, in.Profile)
		if err != nil {
			return "", err
		}
		if in.MaxTokens == 0 {
			in.MaxTokens = 2048
		}
		if conn.MaxTokens > 0 {
			in.MaxTokens = min(in.MaxTokens, conn.MaxTokens)
		}
		prompt := in.Prompt
		if len(in.Input) > 0 {
			prompt += "\n\nInput JSON:\n" + string(in.Input)
		}
		request := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: in.MaxTokens, Temperature: conn.Temperature, JSONSchema: string(in.OutputSchema), Grammar: string(in.OutputSchema)}
		if conn.ContextLimit > 0 && llm.EstimatePromptTokens(request)+in.MaxTokens > conn.ContextLimit {
			return "", fmt.Errorf("model request exceeds profile context limit")
		}
		response, err := conn.Client.Chat(ctx, request)
		if err != nil {
			return "", err
		}
		if response.FinishReason == "length" || len(response.Message.ToolCalls) > 0 {
			return "", fmt.Errorf("model returned incomplete output or an unexpected tool call")
		}
		if len(response.Message.Content) > 1<<20 {
			return "", fmt.Errorf("model output exceeds 1 MiB")
		}
		result := map[string]any{"text": response.Message.Content, "model": conn.Name, "usage": response.Usage}
		if len(in.OutputSchema) > 0 {
			data := json.RawMessage(response.Message.Content)
			if err := workflow.ValidateJSON(data, in.OutputSchema); err != nil {
				return "", fmt.Errorf("model output: %w", err)
			}
			result["data"] = data
		}
		data, err := json.Marshal(result)
		return string(data), err
	}}
}
