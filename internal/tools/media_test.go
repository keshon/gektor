package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/media/contract"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMediaLazyToolsShareWorkflowContractsAndResources(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &Capabilities{}
	r, err := Compose(Dependencies{WS: ws, Vision: false}, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	if r.Has("video_info") || r.Has("media_convert") {
		t.Fatal("media schemas loaded eagerly")
	}
	discovered, err := (DiscoverCapabilities{Catalog: c}).Run(t.Context(), json.RawMessage(`{"query":"YouTube"}`))
	if err != nil || !strings.Contains(discovered, "native_media") {
		t.Fatal(discovered, err)
	}
	if strings.Contains(c.Map(), "Call media_status") || strings.Contains(c.Map(), "Follow transcript next_offset") {
		t.Fatal("activation instructions leaked into startup context")
	}
	activated, err := c.Enable(t.Context(), "native_media", nil)
	if err != nil || !strings.Contains(activated, "native YouTube") {
		t.Fatal(activated, err)
	}
	registry, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range contract.Names() {
		node, err := registry.Node(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range c.native("native_media").Tools {
			if entry.Name() == name {
				var a, b any
				_ = json.Unmarshal(entry.Schema(), &a)
				_ = json.Unmarshal(node.InputSchema, &b)
				if !reflect.DeepEqual(a, b) {
					t.Fatal("tool/node contract differs", name)
				}
				if entry.Mode() == agent.Concurrent && !node.ReadOnly {
					t.Fatal("mutating node exposed as concurrent", name)
				}
			}
		}
	}
	paths, err := PathResources(ws, "video_download", json.RawMessage(`{"url":"https://youtu.be/abcdefghijk","path":"saved.mp4"}`))
	if err != nil || len(paths) != 2 || paths[1] != "saved.mp4" {
		t.Fatal(paths, err)
	}
	if _, err := PathResources(ws, "media_convert", json.RawMessage(`{"inputs":["../secret.wav"],"path":"out.mp3","format":"mp3"}`)); err == nil {
		t.Fatal("outside path authorized")
	}
	if _, err := r.Run(t.Context(), "media_convert", json.RawMessage(`{"inputs":["in.wav"],"path":"out.mp3","format":"mp3","args":["-anything"]}`)); err == nil {
		t.Fatal("arbitrary CLI options accepted")
	}
}

func TestMediaWorkflowValidationDoesNotProbeAndExecutionHonorsDeny(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	svc, err := app.NewWorkflow(ws, t.TempDir(), permission.Policy{Rules: []permission.Rule{{Tool: "media_convert", Pattern: "*", Effect: permission.Deny}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"version":3,"name":"Media validation","steps":[{"id":"convert","tool":"media_convert","args":{"inputs":["missing.wav"],"path":"out.flac","format":"flac"}}]}`)
	svc.ValidateOnly = true
	if _, err := svc.RunSource(t.Context(), source, "media.json", nil); err != nil {
		t.Fatal("validation depended on installed programs or existing input", err)
	}
	svc.ValidateOnly = false
	if _, err := svc.RunSource(t.Context(), source, "media.json", nil); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("media deny bypassed", err)
	}
	svc.AuthorizeResource = func(context.Context, string, string, json.RawMessage) error { return nil }
	if _, err := svc.RunSource(t.Context(), []byte(`{"version":3,"name":"Media template","inputs":{"source":{"type":"string","default":"../outside.wav"}},"steps":[{"id":"convert","tool":"media_convert","args":{"inputs":[{"$input":"source"}],"path":"out.flac","format":"flac"}}]}`), "media.json", nil); err == nil {
		t.Fatal("resolved path escaped workspace")
	}
}
