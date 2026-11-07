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

func TestDocumentsLazyDiscoveryAgentWorkflowAndResources(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &Capabilities{}
	reg, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = reg
	if reg.Has("docx_read") || reg.Has("pdf_read") {
		t.Fatal("document schemas loaded eagerly")
	}
	if _, err := c.Enable(t.Context(), "native_documents", nil); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Invoice 73</w:t></w:r></w:p></w:body></w:document>`))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.docx", archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	nodes, err := app.AutomationRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range c.native("native_documents").Tools {
		node, err := nodes.Node(tool.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(node.InputSchema, tool.Schema()) || !node.ReadOnly || !node.RetrySafe || reg.ModeOf(tool.Name()) != agent.Concurrent {
			t.Fatal("contract drift", tool.Name())
		}
		args, _ := json.Marshal(map[string]string{"path": filepath.ToSlash(filepath.Join(ws.Root(), "input.docx"))})
		paths, err := PathResources(ws, tool.Name(), args)
		if err != nil || !reflect.DeepEqual(paths, []string{"input.docx"}) {
			t.Fatal(paths, err)
		}
	}
	args := json.RawMessage(`{"path":"input.docx"}`)
	direct, err := reg.Run(t.Context(), "docx_read", args)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := nodes.Node("docx_read")
	fromNode, err := node.Execute(t.Context(), args)
	if err != nil || direct != fromNode || !strings.Contains(direct, "Invoice 73") {
		t.Fatal(direct, fromNode, err)
	}
	if err := automation.ValidateJSON(json.RawMessage(direct), node.OutputSchema); err != nil {
		t.Fatal(err)
	}
	BindWorkflowPolicy(c, ws, permission.Default(), nil, t.TempDir())
	if _, err := c.Enable(t.Context(), "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	recipe := `{"version":3,"name":"Read document","steps":[{"id":"read","tool":"docx_read","args":{"path":"input.docx"}}],"outputs":{"document":{"$step":"read"}}}`
	if err := ws.WriteFile("recipe.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || !strings.Contains(out, "Invoice 73") {
		t.Fatal(out, err)
	}
	BindWorkflowPolicy(c, ws, permission.Policy{Rules: []permission.Rule{{Tool: "docx_read", Pattern: "input.docx", Effect: permission.Deny}}}, nil, t.TempDir())
	_, err = reg.Run(t.Context(), "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("document node bypassed permission", err)
	}
}
