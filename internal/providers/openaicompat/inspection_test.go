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
		if (err == nil) != want || got != want {
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

func TestVisionMetadataOverridesNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"qwen3-vl","architecture":{"input_modalities":["text"]}}]}`)
	}))
	defer srv.Close()
	got, err := New(srv.URL, "qwen3-vl", nil).SupportsVision(context.Background())
	if err != nil || got {
		t.Fatalf("explicit text-only metadata ignored: %v %v", got, err)
	}
	got, err = New(srv.URL, "my-local-vision-weights", nil).SupportsVision(context.Background())
	if err != nil || !got {
		t.Fatalf("known vision fallback lost: %v %v", got, err)
	}
	got, err = New(srv.URL, "custom-weights", nil).SupportsVision(context.Background())
	if err == nil || got {
		t.Fatalf("unknown model misclassified: %v %v", got, err)
	}
}
