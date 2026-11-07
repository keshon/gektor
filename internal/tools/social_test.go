package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/integrations/social"
)

func TestReaderNormalizesAccessAndProtocolResults(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		code             int
	}{
		{"success", `{"code":200,"tweet":{"url":"https://x.com/test/status/20","text":"post text","created_at":"date","author":{"name":"Test","screen_name":"test"},"media":{"photos":[{"url":"https://example.org/image.jpg"}]}}}`, "ok", 200},
		{"private", `{"code":401}`, "access_restricted", 200},
		{"missing", `{"code":404}`, "unavailable", 200},
		{"rate_limit", ``, "transient_error", 429},
		{"server_error", ``, "transient_error", 503},
		{"invalid", `<html>gateway</html>`, "invalid_response", 200},
		{"empty", `{"code":200}`, "invalid_response", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/status/20" || !strings.Contains(r.UserAgent(), "gektor") {
					t.Errorf("unexpected request %s", r.URL)
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			tool := SocialReadPost{client: server.Client(), endpoint: server.URL}
			out, err := tool.Run(t.Context(), json.RawMessage(`{"url":"https://x.com/test/status/20"}`))
			var result social.Post
			if json.Unmarshal([]byte(out), &result) != nil || result.Status != tc.want {
				t.Fatal(out, err)
			}
			if (err == nil) != (tc.want == "ok") {
				t.Fatal("partial failure looked successful", out, err)
			}
			if tc.want == "ok" && (result.Text != "post text" || len(result.Media) != 1 || result.Handle != "test") {
				t.Fatal(result)
			}
		})
	}
}

func TestBuiltinSocialLifecycleIsLazyAndRestorable(t *testing.T) {
	sources := WithBuiltinCapabilities(nil)
	registry := agent.NewRegistry()
	catalog := &Capabilities{Sources: sources, Registry: registry}
	if _, err := (DiscoverCapabilities{Catalog: catalog}).Run(t.Context(), json.RawMessage(`{"query":"Twitter"}`)); err != nil {
		t.Fatal(err)
	}
	if sources[0].tools != nil || registry.Has("cap__social__read_post") {
		t.Fatal("discovery activated social tools")
	}
	if _, err := catalog.Enable(t.Context(), "social", nil); err != nil {
		t.Fatal(err)
	}
	if !registry.Has("cap__social__read_post") || len(catalog.Save()) != 1 {
		t.Fatal("activation did not save catalog")
	}
	resumedSources := WithBuiltinCapabilities(nil)
	resumed := &Capabilities{Sources: resumedSources, Registry: agent.NewRegistry()}
	if err := resumed.Restore(t.Context(), catalog.Save()); err != nil {
		t.Fatal(err)
	}
	if resumedSources[0].tools != nil || !resumed.Registry.Has("cap__social__read_post") {
		t.Fatal("resume eagerly loaded adapter")
	}
	output, err := resumed.Registry.Run(t.Context(), "cap__social__read_post", json.RawMessage(`{"url":"https://example.org/unsupported"}`))
	if err == nil || !strings.Contains(output, "unsupported") {
		t.Fatal(output, err)
	}
	custom := &Capability{ID: "social", Description: "custom provider"}
	if configured := WithBuiltinCapabilities([]*Capability{custom}); len(configured) != 1 || configured[0] != custom {
		t.Fatal("custom provider was shadowed")
	}
}

func TestNativeSocialLive(t *testing.T) {
	if os.Getenv("GEKTOR_SOCIAL_LIVE") != "1" {
		t.Skip("set GEKTOR_SOCIAL_LIVE=1 for one public FxTwitter request")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	output, err := (SocialReadPost{}).Run(ctx, json.RawMessage(`{"url":"https://x.com/jack/status/20"}`))
	if err != nil {
		t.Fatal(output, err)
	}
	var result social.Post
	if json.Unmarshal([]byte(output), &result) != nil || result.Status != "ok" || result.Text == "" {
		t.Fatal(output)
	}
	t.Logf("status=%s author=%s bytes=%d", result.Status, result.Handle, len(output))
}
