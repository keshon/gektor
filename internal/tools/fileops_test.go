package tools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

func fileCapability(t *testing.T) (*workspace.Workspace, *Capabilities) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	c := &Capabilities{}
	r, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	if r.Has("download_file") || r.Has("archive_list") {
		t.Fatal("file schemas loaded eagerly")
	}
	if _, err := c.Enable(t.Context(), "native_files", nil); err != nil {
		t.Fatal(err)
	}
	return ws, c
}

func TestFilePackAndWorkflowShareContractsAndResults(t *testing.T) {
	ws, c := fileCapability(t)
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	f, err := w.Create("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.zip", data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := app.AutomationRegistry(ws, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range c.native("native_files").Tools {
		node, err := reg.Node(tool.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(tool.Schema(), node.InputSchema) || node.ReadOnly != (tool.Name() == "archive_list") || node.RetrySafe != node.ReadOnly {
			t.Fatal("agent/node contract drift", tool.Name())
		}
	}
	args := json.RawMessage(`{"path":"input.zip"}`)
	direct, err := c.Registry.Run(t.Context(), "archive_list", args)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := reg.Node("archive_list")
	fromNode, err := node.Execute(t.Context(), args)
	if err != nil || fromNode != direct {
		t.Fatal("agent/node result drift", direct, fromNode, err)
	}
	if err := automation.ValidateJSON(json.RawMessage(direct), node.OutputSchema); err != nil {
		t.Fatal(err)
	}
	if c.Registry.ModeOf("download_file") != agent.Exclusive || c.Registry.ModeOf("archive_list") != agent.Concurrent {
		t.Fatal("wrong execution modes")
	}
	// The actual workflow service must also execute the operation, not just
	// advertise its schema or expose a callable node.
	BindWorkflowPolicy(c, ws, permission.Default(), nil, t.TempDir())
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":3,"name":"List ZIP","steps":[{"id":"list","tool":"archive_list","args":{"path":"input.zip"}}],"outputs":{"listing":{"$step":"list"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := c.Registry.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || !strings.Contains(output, "hello.txt") {
		t.Fatal(output, err)
	}
}

func TestFileOperationResourcesAreCanonicalAndWorkflowDenyApplies(t *testing.T) {
	ws, c := fileCapability(t)
	for _, tool := range c.native("native_files").Tools {
		args := json.RawMessage(`{"path":"` + filepath.ToSlash(filepath.Join(ws.Root(), "blocked.zip")) + `"}`)
		want := []string{"blocked.zip"}
		if tool.Name() == "download_file" {
			args = json.RawMessage(`{"url":"https://example.com/file#ignored","path":"` + filepath.ToSlash(filepath.Join(ws.Root(), "blocked.zip")) + `"}`)
			want = []string{"https://example.com/file", "blocked.zip"}
		}
		got, err := tool.(interface {
			Resources(json.RawMessage) ([]string, error)
		}).Resources(args)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("missing direct resources", got, err)
		}
		fallback, err := PathResources(ws, tool.Name(), args)
		if err != nil || !reflect.DeepEqual(fallback, want) {
			t.Fatal("missing fallback resources", fallback, err)
		}
	}
	BindWorkflowPolicy(c, ws, permission.Policy{Rules: []permission.Rule{{Tool: "download_file", Pattern: "blocked.zip", Effect: permission.Deny}}}, nil, t.TempDir())
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":3,"name":"Denied download","steps":[{"id":"get","tool":"download_file","args":{"url":"https://example.com/file","path":"blocked.zip"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := c.Registry.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "blocked by policy") {
		t.Fatal("workflow bypassed destination deny", err)
	}
	if _, err := ws.Stat("blocked.zip"); err == nil {
		t.Fatal("denied download created output")
	}
}
