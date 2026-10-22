package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupportsVision_LlamaReadsModalities(t *testing.T) {
	for _, want := range []bool{true, false} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/props" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"modalities":{"vision":%t}}`, want)
			}))
			defer srv.Close()

			got, err := NewLlamaClient(srv.URL, "local").SupportsVision(context.Background())
			if err != nil || got != want {
				t.Fatalf("SupportsVision = %t, %v; want %t", got, err, want)
			}
		})
	}
}

func TestSupportsVision_OpenAIReadsModelCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("probe did not send the API key")
		}
		fmt.Fprint(w, `{"data":[
			{"id":"text-only","architecture":{"input_modalities":["text"]}},
			{"id":"sees","architecture":{"modality":"text+image->text","input_modalities":["text","image"]}}
		]}`)
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL+"/v1", "sees").WithAPIKey("secret")
	if got, err := c.SupportsVision(context.Background()); err != nil || !got {
		t.Fatalf("vision model probe = %t, %v", got, err)
	}

	c = NewOpenAIClient(srv.URL+"/v1", "text-only").WithAPIKey("secret")
	if got, err := c.SupportsVision(context.Background()); err != nil || got {
		t.Fatalf("text model probe = %t, %v", got, err)
	}
}

// A provider that publishes only ids cannot answer, so a known vision
// family is accepted and an unknown name is treated as text-only.
func TestSupportsVision_OpenAIFallsBackToModelName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"gpt-4o"}]}`)
	}))
	defer srv.Close()

	for model, want := range map[string]bool{"gpt-4o": true, "gpt-3.5-turbo": false, "qwen2.5-vl-72b": true} {
		got, err := NewOpenAIClient(srv.URL, model).SupportsVision(context.Background())
		if err != nil || got != want {
			t.Fatalf("%s: SupportsVision = %t, %v; want %t", model, got, err, want)
		}
	}
}

func TestSupportsVision_KoboldIsAlwaysFalse(t *testing.T) {
	got, err := NewKoboldClient("http://127.0.0.1:1", "local").SupportsVision(context.Background())
	if err != nil || got {
		t.Fatalf("kobold SupportsVision = %t, %v", got, err)
	}
}
