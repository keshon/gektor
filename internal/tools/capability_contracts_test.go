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
	r, err := Compose(Dependencies{WS: ws, Vision: false}, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	toolSchemas := map[string]json.RawMessage{}
	descriptions := map[string]string{}
	for _, id := range []string{"native_documents", "native_files", "native_images", "native_media", "social", "native_workflows"} {
		if _, err := c.Enable(t.Context(), id, nil); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, def := range r.Defs() {
		toolSchemas[def.Name] = def.Parameters
		descriptions[def.Name] = def.Description
	}
	nodes, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, args                      string
		readOnly, concurrent, retrySafe bool
	}{
		{"docx_read", `{"path":"input.docx"}`, true, true, true},
		{"xlsx_read", `{"path":"input.xlsx"}`, true, true, true},
		{"pdf_read", `{"path":"input.pdf"}`, true, true, true},
		{"download_file", `{"url":"https://example.com/file","path":"download.bin"}`, false, false, false},
		{"archive_list", `{"path":"input.zip"}`, true, true, true},
		{"archive_create", `{"paths":["input.txt"],"path":"out.zip"}`, false, false, false},
		{"archive_extract", `{"path":"input.zip","out_dir":"out"}`, false, false, false},
		{"image_inspect", `{"paths":["input.png"]}`, true, true, true},
		{"image_preview", `{"paths":["input.png"]}`, true, true, true},
		{"image_compare", `{"paths":["a.png","b.png"]}`, true, true, true},
		{"image_variants", `{"paths":["input.png"],"widths":[8],"out_dir":"out"}`, false, false, false},
		{"media_status", `{}`, true, true, true},
		{"video_info", `{"url":"https://youtu.be/abcdefghijk"}`, true, true, true},
		{"video_transcript", `{"url":"https://youtu.be/abcdefghijk"}`, true, true, true},
		{"video_download", `{"url":"https://youtu.be/abcdefghijk","path":"video.webm"}`, false, false, false},
		{"media_inspect", `{"inputs":["input.wav"]}`, true, true, true},
		{"media_convert", `{"inputs":["input.wav"],"path":"out.flac","format":"flac"}`, false, false, false},
		{"media_clip", `{"inputs":["input.wav"],"path":"out.wav","format":"wav","duration_seconds":1}`, false, false, false},
		{"media_frame", `{"inputs":["input.mp4"],"path":"frame.png","format":"png"}`, false, false, false},
		{"media_merge", `{"inputs":["video.mp4","audio.wav"],"path":"out.mkv","format":"mkv"}`, false, false, false},
		{"social_read_post", `{"url":"https://x.com/test/status/20"}`, true, true, true},
		{"record_read", `{"kind":"notes","id":"state","allow_missing":true}`, true, true, true},
		{"record_write", `{"kind":"notes","id":"state","expected_revision":0,"data":{}}`, false, false, false},
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
			if descriptions[toolName] != node.Description {
				t.Fatal("tool and node descriptions differ")
			}
			if (r.ModeOf(toolName) == agent.Concurrent) != tc.concurrent || node.ReadOnly != tc.readOnly || node.RetrySafe != tc.retrySafe {
				t.Fatalf("execution policy differs: mode=%s readOnly=%v retrySafe=%v", r.ModeOf(toolName), node.ReadOnly, node.RetrySafe)
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
	svc, err := app.NewWorkflow(ws, t.TempDir(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"version":3,"name":"Post","steps":[{"id":"post","tool":"social_read_post","args":{"url":"https://x.com/test/status/20"}}]}`)
	if _, err := svc.RunSource(t.Context(), source, "post.json", nil); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("node bypassed the Agent permission identity", err)
	}
}
