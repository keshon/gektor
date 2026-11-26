package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

const sessionTitleSchema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`
const sessionTitleGrammar = `root ::= "{" ws "\"title\"" ws ":" ws string ws "}"
string ::= "\"" ([^"\\\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
ws ::= [ \t\n\r]*`

// GenerateSessionTitle names a session using the selected client and a bounded budget.
// It receives no conversation history, tools, files or attachments.
func GenerateSessionTitle(ctx context.Context, client llm.Client, request string) (string, error) {
	text := []rune(strings.TrimSpace(request))
	if client == nil || len(text) == 0 {
		return "", fmt.Errorf("session title needs a client and user text")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	req := llm.ChatRequest{
		Messages:  []llm.Message{{Role: llm.RoleSystem, Content: prompts.SessionTitle}, {Role: llm.RoleUser, Content: string(text[:min(len(text), 2000)])}},
		MaxTokens: 96, Temperature: 0.2, SkipReasoning: true, JSONSchema: sessionTitleSchema, Grammar: sessionTitleGrammar,
	}
	resp, err := client.Chat(ctx, req)
	if err == nil && ctx.Err() == nil && resp.FinishReason == "length" &&
		strings.TrimSpace(resp.Message.Content) == "" && resp.Message.Reasoning != "" && len(resp.Message.ToolCalls) == 0 {
		// Some reasoning models spend the entire small budget before writing the title.
		req.MaxTokens = 1536
		resp, err = client.Chat(ctx, req)
	}
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	title := strings.TrimSpace(resp.Message.Content)
	if strings.HasPrefix(title, "{") {
		var result struct {
			Title string `json:"title"`
		}
		err = json.Unmarshal([]byte(title), &result)
		if err != nil {
			return "", fmt.Errorf("model returned malformed session title JSON")
		}
		title = result.Title
	}
	title = strings.TrimSpace(strings.Trim(strings.TrimSpace(title), "\"'“”‘’"))
	if title == "" || utf8.RuneCountInString(title) > 60 || strings.ContainsAny(title, "\r\n") || strings.ContainsAny(title, "<>") || len(resp.Message.ToolCalls) > 0 || resp.FinishReason == "length" {
		return "", fmt.Errorf("model returned an invalid session title")
	}
	for _, r := range title {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return "", fmt.Errorf("session title contains control characters")
		}
	}
	return title, nil
}
