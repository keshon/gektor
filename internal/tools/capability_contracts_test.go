package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

func TestCapabilityToolsAndNodesShareContractsAndResources(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &Capabilities{Sources: WithBuiltinCapabilities(nil)}
	r, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	toolSchemas := map[string]json.RawMessage{}
	for _, id := range []string{"native_documents", "native_files", "native_images", "native_media", "social"} {
		if _, err := c.Enable(t.Context(), id, nil); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, def := range r.Defs() {
		toolSchemas[def.Name] = def.Parameters
	}
	nodes, err := app.AutomationRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, args string }{
		{"docx_read", `{"path":"input.docx"}`}, {"xlsx_read", `{"path":"input.xlsx"}`}, {"pdf_read", `{"path":"input.pdf"}`},
		{"download_file", `{"url":"https://example.com/file","path":"download.bin"}`},
		{"archive_list", `{"path":"input.zip"}`}, {"archive_create", `{"paths":["input.txt"],"path":"out.zip"}`}, {"archive_extract", `{"path":"input.zip","out_dir":"out"}`},
		{"image_inspect", `{"paths":["input.png"]}`}, {"image_preview", `{"paths":["input.png"]}`}, {"image_compare", `{"paths":["a.png","b.png"]}`}, {"image_variants", `{"paths":["input.png"],"widths":[8],"out_dir":"out"}`},
		{"media_status", `{}`}, {"video_info", `{"url":"https://youtu.be/abcdefghijk"}`}, {"video_transcript", `{"url":"https://youtu.be/abcdefghijk"}`}, {"video_download", `{"url":"https://youtu.be/abcdefghijk","path":"video.webm"}`},
		{"media_inspect", `{"inputs":["input.wav"]}`}, {"media_convert", `{"inputs":["input.wav"],"path":"out.flac","format":"flac"}`}, {"media_clip", `{"inputs":["input.wav"],"path":"out.wav","format":"wav","duration_seconds":1}`}, {"media_frame", `{"inputs":["input.mp4"],"path":"frame.png","format":"png"}`}, {"media_merge", `{"inputs":["video.mp4","audio.wav"],"path":"out.mkv","format":"mkv"}`},
		{"social_read_post", `{"url":"https://x.com/test/status/20"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := nodes.Node(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			toolName := node.Name
			if node.PermissionName != "" {
				toolName = node.PermissionName
			}
			var toolSchema, nodeSchema any
			if err := json.Unmarshal(toolSchemas[toolName], &toolSchema); err != nil {
				t.Fatal(toolName, err)
			}
			if err := json.Unmarshal(node.InputSchema, &nodeSchema); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(toolSchema, nodeSchema) {
				t.Fatal("tool and node argument contracts differ")
			}
			if (r.ModeOf(toolName) == agent.Concurrent) != node.ReadOnly {
				t.Fatal("read-only mode differs")
			}
			if node.ReadOnly && !node.RetrySafe {
				t.Fatal("read-only operation lost retry safety")
			}
			raw := json.RawMessage(tc.args)
			direct, err := PathResources(ws, toolName, raw)
			if err != nil {
				t.Fatal(err)
			}
			nested, err := node.Resources(raw)
			if err != nil || !reflect.DeepEqual(direct, nested) {
				t.Fatalf("resource mismatch: direct=%v node=%v error=%v", direct, nested, err)
			}
		})
	}
}

func TestSocialNodeHonorsExistingAgentPermissionName(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	policy := permission.Policy{Rules: []permission.Rule{{Tool: "cap__social__read_post", Pattern: "*", Effect: permission.Deny}}}
	svc, err := app.NewAutomation(ws, t.TempDir(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"version":3,"name":"Post","steps":[{"id":"post","tool":"social_read_post","args":{"url":"https://x.com/test/status/20"}}]}`)
	if _, err := svc.RunSource(t.Context(), source, "post.json", nil); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("node bypassed the Agent permission identity", err)
	}
}
