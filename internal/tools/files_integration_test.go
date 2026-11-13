package tools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func fileCapability(t *testing.T) (*workspace.Workspace, *toolextension.Capabilities) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	c := &toolextension.Capabilities{}
	r, err := Compose(Dependencies{WS: ws, Vision: false}, c, nil)
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

func TestArchiveCreateAndExtractThroughAgentAndWorkflow(t *testing.T) {
	ws, c := fileCapability(t)
	c = policyCatalog(t, Dependencies{WS: ws, Policy: permission.Default(), Gate: nil, StoreDir: t.TempDir()})
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("source.bin", []byte{0, 255, 10, 128}, 0600); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ name, args string }{
		{"archive_create", `{"paths":["source.bin"],"path":"direct.zip"}`},
		{"archive_extract", `{"path":"direct.zip","out_dir":"direct"}`},
	} {
		out, err := c.Registry.Run(t.Context(), call.name, json.RawMessage(call.args))
		if err != nil {
			t.Fatal(call.name, out, err)
		}
		reg, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		node, _ := reg.Node(call.name)
		if err := workflow.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
			t.Fatal(err)
		}
	}
	recipe := `{"version":4,"name":"ZIP roundtrip","steps":[{"id":"pack","tool":"archive_create","args":{"paths":["source.bin"],"path":"workflow.zip"}},{"id":"unpack","tool":"archive_extract","args":{"path":{"$step":"pack","pointer":"/path"},"out_dir":"workflow"}}],"outputs":{"result":{"$step":"unpack"}}}`
	if err := ws.WriteFile("recipe.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := c.Registry.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || !strings.Contains(out, `"files":1`) {
		t.Fatal(out, err)
	}
	a, e1 := ws.ReadFile("direct/source.bin")
	b, e2 := ws.ReadFile("workflow/source.bin")
	if e1 != nil || e2 != nil || !bytes.Equal(a, b) || !bytes.Equal(a, []byte{0, 255, 10, 128}) {
		t.Fatal(a, b, e1, e2)
	}
}

func TestArchiveGeneratedOutputDenyAppliesBeforeAnyWrite(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "workflow"}[workflow], func(t *testing.T) {
			ws, c := fileCapability(t)
			if err := ws.WriteFile("source.txt", []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Registry.Run(t.Context(), "archive_create", json.RawMessage(`{"paths":["source.txt"],"path":"input.zip"}`)); err != nil {
				t.Fatal(err)
			}
			c = policyCatalog(t, Dependencies{WS: ws, Policy: permission.Policy{Rules: []permission.Rule{{Tool: "archive_extract", Pattern: "out/source.txt", Effect: permission.Deny}}}, Gate: nil, StoreDir: t.TempDir()})
			var err error
			if workflow {
				if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
					t.Fatal(err)
				}
				if err := ws.WriteFile("recipe.json", []byte(`{"version":4,"name":"Denied ZIP","steps":[{"id":"unpack","tool":"archive_extract","args":{"path":"input.zip","out_dir":"out"}}]}`), 0600); err != nil {
					t.Fatal(err)
				}
				_, err = c.Registry.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
			} else {
				_, err = c.Registry.Run(t.Context(), "archive_extract", json.RawMessage(`{"path":"input.zip","out_dir":"out"}`))
			}
			if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
				t.Fatal("generated output bypassed deny", err)
			}
			if _, err := ws.Stat("out"); !os.IsNotExist(err) {
				t.Fatal("denied extraction created output", err)
			}
		})
	}
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
	reg, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range nativeFixture(c, "native_files").Tools {
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
	if err := workflow.ValidateJSON(json.RawMessage(direct), node.OutputSchema); err != nil {
		t.Fatal(err)
	}
	if c.Registry.ModeOf("download_file") != agent.Exclusive || c.Registry.ModeOf("archive_list") != agent.Concurrent {
		t.Fatal("wrong execution modes")
	}
	// The actual workflow service must also execute the operation, not just
	// advertise its schema or expose a callable node.
	c = policyCatalog(t, Dependencies{WS: ws, Policy: permission.Default(), Gate: nil, StoreDir: t.TempDir()})
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":4,"name":"List ZIP","steps":[{"id":"list","tool":"archive_list","args":{"path":"input.zip"}}],"outputs":{"listing":{"$step":"list"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := c.Registry.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || !strings.Contains(output, "hello.txt") {
		t.Fatal(output, err)
	}
}

func TestFileOperationResourcesAreCanonicalAndWorkflowDenyApplies(t *testing.T) {
	ws, c := fileCapability(t)
	for _, tool := range nativeFixture(c, "native_files").Tools {
		args := json.RawMessage(`{"path":"` + filepath.ToSlash(filepath.Join(ws.Root(), "blocked.zip")) + `"}`)
		want := []string{"blocked.zip"}
		if tool.Name() == "download_file" {
			args = json.RawMessage(`{"url":"https://example.com/file#ignored","path":"` + filepath.ToSlash(filepath.Join(ws.Root(), "blocked.zip")) + `"}`)
			want = []string{"https://example.com/file", "blocked.zip"}
		}
		if tool.Name() == "archive_create" {
			args = json.RawMessage(`{"paths":["source.txt"],"path":"blocked.zip"}`)
			want = []string{"source.txt", "blocked.zip"}
		}
		if tool.Name() == "archive_extract" {
			args = json.RawMessage(`{"path":"blocked.zip","out_dir":"out"}`)
			want = []string{"blocked.zip", "out"}
		}
		got, err := tool.(interface {
			Resources(json.RawMessage) ([]string, error)
		}).Resources(args)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("missing direct resources", got, err)
		}
		fallback, err := (WorkspacePaths{WS: ws}).Resources(tool.Name(), args)
		if err != nil || !reflect.DeepEqual(fallback, want) {
			t.Fatal("missing fallback resources", fallback, err)
		}
	}
	c = policyCatalog(t, Dependencies{WS: ws, Policy: permission.Policy{Rules: []permission.Rule{{Tool: "download_file", Pattern: "blocked.zip", Effect: permission.Deny}}}, Gate: nil, StoreDir: t.TempDir()})
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", []byte(`{"version":4,"name":"Denied download","steps":[{"id":"get","tool":"download_file","args":{"url":"https://example.com/file","path":"blocked.zip"}}]}`), 0600); err != nil {
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
