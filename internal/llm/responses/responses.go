package responses

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

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

func (c *Client) responseRequest(req llm.ChatRequest) ([]byte, error) {
	body, _, err := c.buildRequest(req)
	return body, err
}

func (c *Client) buildRequest(req llm.ChatRequest) ([]byte, bool, error) {
	fallback := false
	input := []any{}
	for _, m := range llm.ModelMessages(req.Messages) {
		if state := m.ProviderState; m.Role == llm.RoleAssistant && state.Matches(m) && state.Integration == "chatgpt:"+strings.TrimRight(c.baseURL, "/") && state.Model == c.model && state.Scope == c.replayScope && c.replayScope != "" {
			var items []json.RawMessage
			if json.Unmarshal(state.Payload, &items) == nil {
				// Validate loaded snapshots too: the stored fingerprint is not authority.
				decoded, err := c.decodeResponse(responseEnvelope{Output: items})
				if err == nil && llm.ProjectionID(decoded.Message) == llm.ProjectionID(m) {
					for _, item := range items {
						input = append(input, item)
					}
					continue
				}
			}
		}
		if m.ProviderState != nil {
			fallback = true
		}
		if m.Role == llm.RoleTool {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			if len(m.Images) > 0 {
				content := []any{map[string]any{"type": "input_text", "text": "Images returned by tool " + m.ToolCallID}}
				for _, path := range m.Images {
					image, err := encodeImage(path)
					if err != nil {
						return nil, fallback, err
					}
					content = append(content, map[string]any{"type": "input_image", "image_url": image})
				}
				input = append(input, map[string]any{"role": "user", "content": content})
			}
			continue
		}
		role := string(m.Role)
		if m.Role == llm.RoleSystem {
			role = "developer"
		}
		if m.Content != "" || len(m.Images) > 0 {
			msg := map[string]any{"role": role, "content": m.Content}
			if len(m.Images) > 0 {
				content := []any{}
				if m.Content != "" {
					content = append(content, map[string]any{"type": "input_text", "text": m.Content})
				}
				for _, path := range m.Images {
					image, err := encodeImage(path)
					if err != nil {
						return nil, fallback, err
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
	if c.reasoningEffort != "" || c.reasoningSummary {
		reasoning := map[string]string{}
		if c.reasoningEffort != "" {
			reasoning["effort"] = c.reasoningEffort
		}
		if c.reasoningSummary {
			reasoning["summary"] = "auto"
		}
		wire["reasoning"] = reasoning
	}
	if len(req.Tools) > 0 {
		tools := []any{}
		for _, tool := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters, "strict": false})
		}
		wire["tools"] = []any{map[string]any{"type": "namespace", "name": "gektor", "description": "Gektor tools, executed locally under its permission policy.", "tools": tools}}
	}
	if req.JSONSchema != "" {
		if !json.Valid([]byte(req.JSONSchema)) {
			return nil, fallback, fmt.Errorf("invalid structured response schema")
		}
		wire["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "gektor_result", "schema": json.RawMessage(req.JSONSchema), "strict": true}}
	}
	// This OAuth route rejects temperature and max_output_tokens. MaxTokens
	// remains a harness budget; it is not misrepresented as a server limit.
	body, err := json.Marshal(wire)
	return body, fallback, err
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

func (c *Client) responses(ctx context.Context, req llm.ChatRequest, onReasoning, onContent func(string)) (llm.ChatResponse, error) {
	body, fallback, err := c.buildRequest(req)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	if c.tokenSource == nil {
		return llm.ChatResponse{}, fmt.Errorf("ChatGPT credentials were not configured")
	}
	if req.OnProgress != nil {
		req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "credentials", Detail: "Preparing ChatGPT credentials"})
	}
	token, err := c.tokenSource(ctx)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	result, err := c.sendResponses(ctx, req, body, token, onReasoning, onContent)
	if api, ok := llm.AsAPIError(err); fallback && ok && api.Status == http.StatusBadRequest && !api.Overflow {
		err = fmt.Errorf("ChatGPT rejected normalized continuation after incompatible replay state was discarded; start a new session or restore the original account, model and unchanged history: %w", err)
	}
	return result, err
}

// sendResponses is also used by protocol tests with a local SSE server.
func (c *Client) sendResponses(ctx context.Context, req llm.ChatRequest, body []byte, token string, onReasoning, onContent func(string)) (result llm.ChatResponse, resultErr error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	partial := llm.Message{Role: llm.RoleAssistant}
	defer func() {
		if resultErr != nil {
			if cause := context.Cause(ctx); cause != nil {
				resultErr = cause
			}
			// Keep what observers already saw, but never authorize a partial call.
			result = llm.ChatResponse{Message: partial, FinishReason: "incomplete_stream"}
		}
	}()
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.baseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return llm.ChatResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if req.OnProgress != nil {
		req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "sending", Detail: "Sending request to ChatGPT"})
	}
	var traceMu sync.Mutex
	traceFinished := false
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		traceMu.Lock()
		defer traceMu.Unlock()
		if traceFinished {
			return
		}
		if info.Err == nil && req.OnProgress != nil {
			req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "headers", Detail: "Waiting for ChatGPT response headers (90s limit)"})
		}
	}}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	resp, err := c.http.Do(request)
	// httptrace callbacks may outlive RoundTrip; finish delivery before the
	// request observer or its session log can close.
	traceMu.Lock()
	traceFinished = true
	traceMu.Unlock()
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ChatGPT request: %w", err)
	}
	defer resp.Body.Close()
	idle := time.AfterFunc(c.idleTimeout(), func() {
		cancel(fmt.Errorf("ChatGPT stream idle for %s: %w", c.idleTimeout(), context.DeadlineExceeded))
	})
	defer idle.Stop()

	if req.OnProgress != nil {
		req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "response_headers", HTTPStatus: resp.StatusCode, RequestID: resp.Header.Get("x-request-id"), Detail: "ChatGPT response headers received"})
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return llm.ChatResponse{}, responseFailure(resp.StatusCode, raw, resp.Header.Get("x-request-id"))
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !strings.Contains(contentType, "text/event-stream") {
		return llm.ChatResponse{}, fmt.Errorf("ChatGPT returned a non-stream response")
	}
	if req.OnProgress != nil {
		req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "stream", RequestID: resp.Header.Get("x-request-id"), Detail: "ChatGPT connected; waiting for response"})
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 8192), 8<<20)
	var eventData []string
	completedItems := map[int]json.RawMessage{}
	firstEvent := true
	process := func(raw []byte) (llm.ChatResponse, bool, error) {
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
			return llm.ChatResponse{}, false, nil
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			return llm.ChatResponse{}, false, fmt.Errorf("invalid ChatGPT stream event")
		}
		if firstEvent {
			firstEvent = false
			if req.OnProgress != nil {
				req.OnProgress(llm.Progress{Phase: "waiting", Subphase: "first_event", Detail: "ChatGPT is processing the response"})
			}
		}
		switch ev.Type {
		case "response.output_item.done":
			if !json.Valid(ev.Item) {
				return llm.ChatResponse{}, false, fmt.Errorf("invalid completed ChatGPT output item")
			}
			completedItems[ev.OutputIndex] = append(json.RawMessage(nil), ev.Item...)
		case "response.output_text.delta":
			partial.Content += ev.Delta
			if onContent != nil {
				onContent(ev.Delta)
			}
		case "response.reasoning_summary_text.delta":
			partial.Reasoning += ev.Delta
			if onReasoning != nil {
				onReasoning(ev.Delta)
			}
		case "response.completed":
			if ev.Response.Status != "completed" {
				return llm.ChatResponse{}, true, fmt.Errorf("ChatGPT response did not complete")
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
			return llm.ChatResponse{}, true, responseFailure(0, mustJSON(ev.Response.Error), resp.Header.Get("x-request-id"))
		case "response.incomplete":
			return llm.ChatResponse{FinishReason: "length"}, true, fmt.Errorf("ChatGPT response incomplete: %s", ev.Response.Incomplete.Reason)
		case "error":
			return llm.ChatResponse{}, true, responseFailure(0, mustJSON(map[string]any{"code": ev.Code, "message": ev.Message}), resp.Header.Get("x-request-id"))
		}
		return llm.ChatResponse{}, false, nil
	}
	for scanner.Scan() {
		idle.Reset(c.idleTimeout())
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if line == "" && len(eventData) > 0 {
			out, done, err := process([]byte(strings.Join(eventData, "\n")))
			eventData = nil
			if err != nil {
				return llm.ChatResponse{}, err
			}
			if done {
				c.recordUsage(out.Usage.PromptTokens, out.Usage.CompletionTokens, out.Usage.CachedTokens)
				return out, nil
			}
		}
	}
	if ctx.Err() != nil {
		return llm.ChatResponse{}, ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("ChatGPT stream: %w", err)
	}
	return llm.ChatResponse{}, llm.ErrIncompleteStream
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
		return &llm.APIError{Status: 400, Body: "ChatGPT context length exceeded", Overflow: true}
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
	if status == 400 || status >= 500 || (status == 429 && detail.Code != "subscription_sharing_usage_limit_exceeded") {
		return &llm.APIError{Status: status, Body: message}
	}
	if status != 0 {
		message += fmt.Sprintf(" [HTTP %d]", status)
	}
	return fmt.Errorf("%s", message)
}

func (c *Client) decodeResponse(w responseEnvelope) (llm.ChatResponse, error) {
	out := llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant}, FinishReason: "stop", Usage: llm.Usage{PromptTokens: w.Usage.Input, CompletionTokens: w.Usage.Output, CachedTokens: w.Usage.InputDetails.Cached}}
	seenCalls := map[string]bool{}
	for _, raw := range w.Output {
		var item responseItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return llm.ChatResponse{}, fmt.Errorf("invalid ChatGPT output item")
		}
		switch item.Type {
		case "reasoning":
			for _, s := range item.Summary {
				out.Message.Reasoning += s.Text
			}
		case "message":
			if item.Role != "assistant" {
				return llm.ChatResponse{}, fmt.Errorf("invalid ChatGPT message role")
			}
			for _, part := range item.Content {
				if part.Type != "output_text" && part.Type != "refusal" {
					return llm.ChatResponse{}, fmt.Errorf("unsupported ChatGPT content type %q", part.Type)
				}
				if part.Type == "output_text" {
					out.Message.Content += part.Text
				}
				if part.Type == "refusal" {
					out.Message.Content += part.Refusal
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" || seenCalls[item.CallID] || !json.Valid([]byte(item.Arguments)) {
				return llm.ChatResponse{}, fmt.Errorf("ChatGPT returned an incomplete tool call; nothing executed")
			}
			if item.Namespace != "" && item.Namespace != "gektor" {
				return llm.ChatResponse{}, fmt.Errorf("ChatGPT returned an unknown tool namespace")
			}
			seenCalls[item.CallID] = true
			out.Message.ToolCalls = append(out.Message.ToolCalls, llm.ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
		default:
			return llm.ChatResponse{}, fmt.Errorf("unsupported ChatGPT output item %q", item.Type)
		}
	}
	if c.replayScope != "" && len(w.Output) > 0 {
		raw, err := json.Marshal(w.Output)
		if err != nil {
			return llm.ChatResponse{}, err
		}
		out.Message.ProviderState = llm.CaptureReplay(out.Message, "chatgpt:"+strings.TrimRight(c.baseURL, "/"), c.model, c.replayScope, raw, w.Usage.OutputDetails.Reasoning)
	}
	if len(out.Message.ToolCalls) > 0 {
		out.FinishReason = "tool_calls"
	}
	return out, nil
}

func encodeImage(path string) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".bmp") {
		return "", fmt.Errorf("ChatGPT image input does not support BMP; convert %q to PNG", path)
	}
	return llm.EncodeImageFile(path)
}
