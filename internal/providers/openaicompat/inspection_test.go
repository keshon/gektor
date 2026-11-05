package openaicompat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupportsVision_OpenAIFallsBackToModelName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"gpt-4o"}]}`)
	}))
	defer srv.Close()

	for model, want := range map[string]bool{"gpt-4o": true, "gpt-3.5-turbo": false, "qwen2.5-vl-72b": true} {
		got, err := New(srv.URL, model, nil).SupportsVision(context.Background())
		if err != nil || got != want {
			t.Fatalf("%s: SupportsVision = %t, %v; want %t", model, got, err, want)
		}
	}
}

func TestOpenAIModelName_ReturnsConfigured(t *testing.T) {
	c := New("http://127.0.0.1:1", "deepseek-chat", nil)
	name, err := c.ModelName(context.Background())
	if err != nil || name != "deepseek-chat" {
		t.Fatalf("ModelName = %q, %v", name, err)
	}
}

func TestOpenAIContextLimit_Table(t *testing.T) {
	if got := ContextLimit("gpt-4o-mini"); got != 128000 {
		t.Errorf("gpt-4o-mini = %d", got)
	}
	if got := ContextLimit("anthropic/claude-sonnet-4"); got != 200000 {
		t.Errorf("claude = %d", got)
	}
	if ContextLimit("something-brand-new") <= 0 {
		t.Error("unknown model should get a safe default, not zero")
	}
}
