// Package chat runs conversations without workspace access or tools.
package chat

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/keshon/tars/internal/llm"
)

type Config struct {
	System        string
	PostHistory   string
	Greeting      string
	CharacterName string
	Temperature   float64
	MaxTokens     int
	ContextLimit  int
}

// Conversation is used serially. Failed and canceled turns never enter history.
// History retains completed turns; only the backend projection is windowed.
type Conversation struct {
	client  llm.Client
	config  Config
	history []llm.Message
}
type Result struct {
	Response     llm.ChatResponse
	OmittedTurns int
}

func New(client llm.Client, config Config) (*Conversation, error) {
	if math.IsNaN(config.Temperature) || math.IsInf(config.Temperature, 0) || config.Temperature < 0 || config.Temperature > 2 {
		return nil, fmt.Errorf("chat: temperature must be between 0 and 2")
	}
	if client == nil {
		return nil, fmt.Errorf("chat: client is required")
	}
	if config.MaxTokens <= 0 || config.ContextLimit <= config.MaxTokens {
		return nil, fmt.Errorf("chat: context limit must exceed positive response budget")
	}
	return &Conversation{client: client, config: config}, nil
}
func (c *Conversation) Reset() { c.history = nil }

// Ask preserves whole user/assistant pairs when projecting a recent window.
// UTF-8 bytes plus message overhead intentionally overestimate most tokenizers;
// this is an estimate, not a backend tokenizer measurement.
func (c *Conversation) Ask(ctx context.Context, text string) (Result, error) {
	return c.ask(ctx, text, nil, "")
}
func (c *Conversation) AskStreaming(ctx context.Context, text string, onDelta func(string), systemNotes ...string) (Result, error) {
	return c.ask(ctx, text, onDelta, strings.Join(systemNotes, "\n"))
}
func (c *Conversation) ask(ctx context.Context, text string, onDelta func(string), systemNote string) (Result, error) {
	var result Result
	if strings.TrimSpace(text) == "" {
		return result, fmt.Errorf("chat: empty message")
	}
	messages := []llm.Message{{Role: llm.RoleSystem, Content: c.config.System}}
	user := llm.Message{Role: llm.RoleUser, Content: text}
	cost := func(m llm.Message) int { return len(m.Content) + 32 }
	budget := c.config.ContextLimit - c.config.MaxTokens - 128
	used := cost(messages[0]) + cost(user)
	if systemNote != "" {
		messages[0].Content += "\n\n" + systemNote
		used += len(systemNote) + 2
	}
	if c.config.Greeting != "" {
		greeting := llm.Message{Role: llm.RoleAssistant, Content: c.config.Greeting}
		messages = append(messages, greeting)
		used += cost(greeting)
	}
	if c.config.PostHistory != "" {
		used += cost(llm.Message{Content: c.config.PostHistory})
	}
	if used > budget {
		return result, fmt.Errorf("chat: message and system prompt exceed estimated context budget")
	}
	start := len(c.history)
	for start >= 2 {
		pair := cost(c.history[start-2]) + cost(c.history[start-1])
		if used+pair > budget {
			break
		}
		used += pair
		start -= 2
	}
	result.OmittedTurns = start / 2
	messages = append(messages, c.history[start:]...)
	messages = append(messages, user)
	if c.config.PostHistory != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: c.config.PostHistory})
	}
	request := llm.ChatRequest{Messages: messages, MaxTokens: c.config.MaxTokens, Temperature: c.config.Temperature}
	var response llm.ChatResponse
	var err error
	if stream, ok := c.client.(interface {
		Stream(context.Context, llm.ChatRequest, func(string)) (llm.ChatResponse, error)
	}); ok && onDelta != nil {
		response, err = stream.Stream(ctx, request, onDelta)
	} else {
		response, err = c.client.Chat(ctx, request)
		if err == nil && onDelta != nil {
			onDelta(response.Message.Content)
		}
	}
	if err == nil {
		var clean strings.Builder
		pos := 0
		for _, region := range llm.ThinkRegions(response.Message.Content) {
			clean.WriteString(response.Message.Content[pos:region[0]])
			pos = region[1]
		}
		clean.WriteString(response.Message.Content[pos:])
		response.Message.Content = strings.TrimSpace(clean.String())
	}
	result.Response = response
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(response.Message.ToolCalls) > 0 || response.FinishReason == "tool_calls" {
		return result, fmt.Errorf("chat: backend requested tools; this conversation has none")
	}
	if response.FinishReason == "length" {
		return result, fmt.Errorf("chat: response token limit reached; incomplete turn was not saved")
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		return result, fmt.Errorf("chat: backend returned an empty response")
	}
	c.history = append(c.history, user, llm.Message{Role: llm.RoleAssistant, Content: response.Message.Content})
	return result, nil
}

// Reconfigured preserves committed turns while updating the next request's settings.
func (c *Conversation) Reconfigured(client llm.Client, cfg Config) (*Conversation, error) {
	next, err := New(client, cfg)
	if err != nil {
		return nil, err
	}
	next.history = c.History()
	return next, nil
}

func Restore(client llm.Client, cfg Config, history []llm.Message) (*Conversation, error) {
	c, err := New(client, cfg)
	if err != nil {
		return nil, err
	}
	if len(history)%2 != 0 {
		return nil, fmt.Errorf("incomplete chat history")
	}
	for i, m := range history {
		want := llm.RoleUser
		if i%2 == 1 {
			want = llm.RoleAssistant
		}
		if m.Role != want || len(m.ToolCalls) > 0 || m.ToolCallID != "" {
			return nil, fmt.Errorf("invalid chat history")
		}
	}
	c.history = append([]llm.Message(nil), history...)
	return c, nil
}
