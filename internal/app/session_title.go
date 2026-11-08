package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

// GenerateSessionTitle makes one bounded request using the selected client.
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
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Messages:  []llm.Message{{Role: llm.RoleSystem, Content: prompts.SessionTitle}, {Role: llm.RoleUser, Content: string(text[:min(len(text), 2000)])}},
		MaxTokens: 384, Temperature: 0.2,
	})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	title := strings.TrimSpace(strings.Trim(strings.TrimSpace(resp.Message.Content), "\"'“”‘’"))
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
