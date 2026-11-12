package nodes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/nodes"
	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/capabilities/social/contract"
	toolsocial "github.com/keshon/gektor/internal/tools/social"
	"github.com/keshon/gektor/internal/workflow"
)

func TestPublicPostNodeAndToolExecuteTheSameReader(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/status/20" {
			t.Errorf("unexpected provider request %s", r.URL)
		}
		fmt.Fprint(w, `{"code":200,"tweet":{"text":"fixture post","author":{"screen_name":"fixture"}}}`)
	}))
	defer server.Close()
	svc := social.Service{Client: server.Client(), Endpoint: server.URL}
	node := nodes.SocialDefinitions(svc)[0]
	tool := toolsocial.Operation{Operation: contract.Operation{Service: svc}}
	args := json.RawMessage(`{"url":"https://x.com/test/status/20"}`)
	for _, execute := range []func() (string, error){func() (string, error) { return tool.Run(t.Context(), args) }, func() (string, error) { return node.Execute(t.Context(), args) }} {
		out, err := execute()
		if err != nil {
			t.Fatal(err)
		}
		if err := workflow.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
			t.Fatal(err)
		}
		var post social.Post
		if err := json.Unmarshal([]byte(out), &post); err != nil || post.Text != "fixture post" || post.Source != "FxTwitter" {
			t.Fatal(out, err)
		}
	}
	if requests != 2 {
		t.Fatal(requests)
	}
}
