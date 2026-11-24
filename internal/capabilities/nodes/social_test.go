package nodes_test

import (
	"context"
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

func TestSocialPageNodesAndToolsShareContracts(t *testing.T) {
	const post = `{"id":"20","text":"fixture","author":{"screen_name":"test"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/conversation/20":
			fmt.Fprint(w, `{"code":200,"status":`+post+`,"thread":[],"replies":[],"cursor":{"bottom":"next"}}`)
		case "/2/profile/test/statuses":
			fmt.Fprint(w, `{"code":200,"results":[`+post+`],"cursor":{"bottom":"next"}}`)
		case "/2/trends":
			fmt.Fprint(w, `{"code":200,"trends":[{"name":"topic"}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer server.Close()
	service := social.Service{Client: server.Client(), Endpoint: server.URL}
	definitions := nodes.SocialDefinitions(service)
	cases := []struct{ kind, args, resource string }{
		{social.ReadThread, `{"url":"https://twitter.com/test/status/20"}`, "https://x.com/i/status/20"},
		{social.ReadTimeline, `{"handle":"@TEST","limit":2}`, "https://x.com/test"},
		{social.ReadTrends, `{"limit":2}`, "https://x.com/explore"},
	}
	for i, tc := range cases {
		tool := toolsocial.PageOperation{PageOperation: contract.PageOperation{Service: service, Kind: tc.kind}}
		node := definitions[i+1]
		args := json.RawMessage(tc.args)
		if node.PermissionName != tool.Name() || !node.ReadOnly || !node.RetrySafe || string(node.InputSchema) != string(tool.Schema()) {
			t.Fatal("tool/node contract drift", tc.kind)
		}
		for _, resources := range []func(json.RawMessage) ([]string, error){node.Resources, tool.Resources} {
			got, err := resources(args)
			if err != nil || len(got) != 1 || got[0] != tc.resource {
				t.Fatal(got, err)
			}
		}
		for _, execute := range []func(context.Context, json.RawMessage) (string, error){node.Execute, tool.Run} {
			output, err := execute(t.Context(), args)
			if err != nil {
				t.Fatal(output, err)
			}
			err = workflow.ValidateJSON(json.RawMessage(output), node.OutputSchema)
			if err != nil {
				t.Fatal(output, err)
			}
		}
		if tool.Validate(json.RawMessage(`{"url":"https://x.com/test/status/20","unexpected":true}`)) == nil {
			t.Fatal("unknown fields accepted")
		}
	}
}
