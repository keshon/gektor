package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/providers/openai"
)

// ChatGPT uses Responses with OAuth; the OpenAI-compatible dialect stays separate.
type chatgptDialect struct{ openaiDialect }

func (chatgptDialect) name() string { return "chatgpt" }
func (chatgptDialect) modelName(_ context.Context, _ *http.Client, _ string) (string, error) {
	return "", nil
}
func (chatgptDialect) vision(context.Context, *http.Client, string, string, string) (bool, error) {
	return false, nil
}

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Role      string `json:"role,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content,omitempty"`
	Summary []struct {
		Text string `json:"text"`
	} `json:"summary,omitempty"`
}

func (c *Server) responseRequest(req ChatRequest) ([]byte, error) {
	input := []any{}
	for _, m := range ModelMessages(req.Messages) {
		if m.Role == RoleAssistant && m.ProviderState != nil && m.ProviderState.Model == c.model {
			for _, item := range m.ProviderState.Reasoning {
				input = append(input, item)
			}
		}
		if m.Role == RoleTool {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			if len(m.Images) > 0 {
				content := []any{map[string]any{"type": "input_text", "text": "Images returned by tool " + m.ToolCallID}}
				for _, path := range m.Images {
					image, err := encodeImageFile(path)
					if err != nil {
						return nil, err
					}
					content = append(content, map[string]any{"type": "input_image", "image_url": image})
				}
				input = append(input, map[string]any{"role": "user", "content": content})
			}
			continue
		}
		role := string(m.Role)
		if m.Role == RoleSystem {
			role = "developer"
		}
		if m.Content != "" || len(m.Images) > 0 {
			msg := map[string]any{"role": role, "content": m.Content}
			if m.Role == RoleAssistant && m.ProviderState != nil && m.ProviderState.Model == c.model && m.ProviderState.Phase != "" {
				msg["phase"] = m.ProviderState.Phase
			}
			if len(m.Images) > 0 {
				content := []any{map[string]any{"type": "input_text", "text": m.Content}}
				for _, path := range m.Images {
					image, err := encodeImageFile(path)
					if err != nil {
						return nil, err
					}
					content = append(content, map[string]any{"type": "input_image", "image_url": image})
				}
				msg["content"] = content
			}
			input = append(input, msg)
		}
		for _, call := range m.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "namespace": "gektor", "arguments": string(call.Arguments)})
		}
	}
	wire := map[string]any{"model": c.model, "input": input, "store": false, "stream": true, "include": []string{"reasoning.encrypted_content"}}
	if len(req.Tools) > 0 {
		tools := []any{}
		for _, tool := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters, "strict": false})
		}
		wire["tools"] = []any{map[string]any{"type": "namespace", "name": "gektor", "description": "Gektor tools, executed locally under its permission policy.", "tools": tools}}
	}
	if req.JSONSchema != "" {
		if !json.Valid([]byte(req.JSONSchema)) {
			return nil, fmt.Errorf("invalid structured response schema")
		}
		wire["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "gektor_result", "schema": json.RawMessage(req.JSONSchema), "strict": true}}
	}
	// This OAuth route rejects temperature and max_output_tokens. MaxTokens
	// remains a harness budget; it is not misrepresented as a server limit.
	return json.Marshal(wire)
}

type responseEnvelope struct {
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Incomplete struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage struct {
		Input        int `json:"input_tokens"`
		Output       int `json:"output_tokens"`
		InputDetails struct {
			Cached int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			Reasoning int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func (c *Server) responses(ctx context.Context, req ChatRequest, onReasoning, onContent func(string)) (ChatResponse, error) {
	body, err := c.responseRequest(req)
	if err != nil {
		return ChatResponse{}, err
	}
	token, err := openai.Default().Token(ctx)
	if err != nil {
		return ChatResponse{}, err
	}
	return c.sendResponses(ctx, req, body, token, onReasoning, onContent)
}

// sendResponses is also used by protocol tests with a local SSE server.
func (c *Server) sendResponses(ctx context.Context, req ChatRequest, body []byte, token string, onReasoning, onContent func(string)) (ChatResponse, error) {
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.baseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ChatGPT request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatResponse{}, responseFailure(resp.StatusCode, raw, resp.Header.Get("x-request-id"))
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !strings.Contains(contentType, "text/event-stream") {
		return ChatResponse{}, fmt.Errorf("ChatGPT returned a non-stream response")
	}
	if req.OnProgress != nil {
		req.OnProgress(Progress{Phase: "waiting", Detail: "Waiting for ChatGPT"})
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 8192), 8<<20)
	var eventData []string
	completedItems := map[int]json.RawMessage{}
	process := func(raw []byte) (ChatResponse, bool, error) {
		var ev struct {
			Item        json.RawMessage  `json:"item"`
			OutputIndex int              `json:"output_index"`
			Type        string           `json:"type"`
			Delta       string           `json:"delta"`
			Response    responseEnvelope `json:"response"`
			Code        string           `json:"code"`
			Message     string           `json:"message"`
		}
		if string(raw) == "[DONE]" {
			return ChatResponse{}, false, nil
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			return ChatResponse{}, false, fmt.Errorf("invalid ChatGPT stream event")
		}
		switch ev.Type {
		case "response.output_item.done":
			if !json.Valid(ev.Item) {
				return ChatResponse{}, false, fmt.Errorf("invalid completed ChatGPT output item")
			}
			completedItems[ev.OutputIndex] = append(json.RawMessage(nil), ev.Item...)
		case "response.output_text.delta":
			if onContent != nil {
				onContent(ev.Delta)
			}
		case "response.reasoning_summary_text.delta":
			if onReasoning != nil {
				onReasoning(ev.Delta)
			}
		case "response.completed":
			if ev.Response.Status != "completed" {
				return ChatResponse{}, true, fmt.Errorf("ChatGPT response did not complete")
			}
			if len(ev.Response.Output) == 0 && len(completedItems) > 0 {
				indexes := make([]int, 0, len(completedItems))
				for index := range completedItems {
					indexes = append(indexes, index)
				}
				sort.Ints(indexes)
				for _, index := range indexes {
					ev.Response.Output = append(ev.Response.Output, completedItems[index])
				}
			}
			out, err := c.decodeResponse(ev.Response)
			return out, true, err
		case "response.failed":
			return ChatResponse{}, true, responseFailure(0, mustJSON(ev.Response.Error), resp.Header.Get("x-request-id"))
		case "response.incomplete":
			return ChatResponse{FinishReason: "length"}, true, fmt.Errorf("ChatGPT response incomplete: %s", ev.Response.Incomplete.Reason)
		case "error":
			return ChatResponse{}, true, responseFailure(0, mustJSON(map[string]any{"code": ev.Code, "message": ev.Message}), resp.Header.Get("x-request-id"))
		}
		return ChatResponse{}, false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if line == "" && len(eventData) > 0 {
			out, done, err := process([]byte(strings.Join(eventData, "\n")))
			eventData = nil
			if err != nil {
				return ChatResponse{}, err
			}
			if done {
				c.recordUsage(out.Usage.PromptTokens, out.Usage.CompletionTokens, out.Usage.CachedTokens)
				return out, nil
			}
		}
	}
	if ctx.Err() != nil {
		return ChatResponse{}, ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("ChatGPT stream: %w", err)
	}
	return ChatResponse{}, ErrIncompleteStream
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

func responseFailure(status int, raw []byte, requestID string) error {
	var detail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &detail)
	if detail.Error.Code != "" {
		detail.Code, detail.Message = detail.Error.Code, detail.Error.Message
	}
	message := "ChatGPT request failed"
	switch detail.Code {
	case "subscription_sharing_user_not_eligible":
		message = "ChatGPT plan usage is unavailable for this account or workspace"
	case "subscription_sharing_usage_limit_exceeded":
		message = "ChatGPT usage limit reached; check ChatGPT Settings > Usage before retrying"
	case "subscription_sharing_usage_unavailable":
		message = "ChatGPT usage availability could not be checked; retry later"
	case "context_length_exceeded":
		return &APIError{Status: 400, Body: "ChatGPT context length exceeded", Overflow: true}
	default:
		if status == 401 {
			message = "ChatGPT authorization expired or was revoked; run: gektor auth login"
		}
	}
	if detail.Code != "" {
		message += " (" + detail.Code + ")"
	}
	if requestID != "" {
		message += "; request " + requestID
	}
	// Plan limits must not trigger the generic 429 retry loop.
	if status >= 500 || (status == 429 && detail.Code != "subscription_sharing_usage_limit_exceeded") {
		return &APIError{Status: status, Body: message}
	}
	if status != 0 {
		message += fmt.Sprintf(" [HTTP %d]", status)
	}
	return fmt.Errorf("%s", message)
}

func (c *Server) decodeResponse(w responseEnvelope) (ChatResponse, error) {
	out := ChatResponse{Message: Message{Role: RoleAssistant}, FinishReason: "stop", Usage: Usage{PromptTokens: w.Usage.Input, CompletionTokens: w.Usage.Output, CachedTokens: w.Usage.InputDetails.Cached}}
	state := &ResponseState{Model: c.model, ReasoningTokens: w.Usage.OutputDetails.Reasoning}
	for _, raw := range w.Output {
		var item responseItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return ChatResponse{}, fmt.Errorf("invalid ChatGPT output item")
		}
		switch item.Type {
		case "reasoning":
			state.Reasoning = append(state.Reasoning, raw)
			for _, s := range item.Summary {
				out.Message.Reasoning += s.Text
			}
		case "message":
			state.Phase = item.Phase
			for _, part := range item.Content {
				if part.Type == "output_text" {
					out.Message.Content += part.Text
				}
				if part.Type == "refusal" {
					out.Message.Content += part.Refusal
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" || !json.Valid([]byte(item.Arguments)) {
				return ChatResponse{}, fmt.Errorf("ChatGPT returned an incomplete tool call; nothing executed")
			}
			if item.Namespace != "" && item.Namespace != "gektor" {
				return ChatResponse{}, fmt.Errorf("ChatGPT returned an unknown tool namespace")
			}
			out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
		default:
			return ChatResponse{}, fmt.Errorf("unsupported ChatGPT output item %q", item.Type)
		}
	}
	if len(state.Reasoning) > 0 || state.Phase != "" {
		out.Message.ProviderState = state
	}
	if len(out.Message.ToolCalls) > 0 {
		out.FinishReason = "tool_calls"
	}
	return out, nil
}
