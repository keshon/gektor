package tools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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

func TestDocumentsContinuationAcrossAgentWorkflowAndOutputLimit(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	want := strings.Repeat("Ж", 20000) + "TAIL-EVIDENCE"
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	f, _ := w.Create("word/document.xml")
	f.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + want + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("long.docx", archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Capabilities{}
	reg, err := Compose(ws, nil, nil, nil, false, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = reg
	if _, err := c.Enable(t.Context(), "native_documents", nil); err != nil {
		t.Fatal(err)
	}
	first, err := reg.Run(t.Context(), "docx_read", json.RawMessage(`{"path":"long.docx"}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		More   bool
		Blocks []struct {
			Text string
			Next int `json:"next_text_offset"`
		}
	}
	if err := json.Unmarshal([]byte(first), &page); err != nil || page.More || len(page.Blocks) != 1 || page.Blocks[0].Next == 0 {
		t.Fatal(first, err)
	}
	// The 48 KiB batch budget gives each of two concurrent calls 24 KiB.
	store := &OutputStore{dir: t.TempDir()}
	ctx := store.Context(t.Context())
	preview := LimitOutput(ctx, "docx_read", first, 24*1024)
	if !strings.Contains(preview, "read_output") || len(preview) > 24*1024 {
		t.Fatal("missing bounded recovery reference")
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	id := strings.TrimSuffix(entries[0].Name(), ".log")
	var recovered strings.Builder
	for offset := 0; ; {
		args, _ := json.Marshal(map[string]any{"id": id, "offset": offset, "limit": 8192})
		out, err := (ReadOutput{}).Run(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			Text string
			Next int `json:"next_offset"`
			EOF  bool
		}
		if err := json.Unmarshal([]byte(out), &row); err != nil {
			t.Fatal(err)
		}
		recovered.WriteString(row.Text)
		if row.EOF {
			break
		}
		offset = row.Next
	}
	if recovered.String() != first {
		t.Fatal("output limiter lost document JSON")
	}
	nodes, err := app.AutomationRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := nodes.Node("docx_read")
	args, _ := json.Marshal(map[string]any{"path": "long.docx", "offset": 0, "text_offset": page.Blocks[0].Next})
	if err := automation.ValidateJSON(args, node.InputSchema); err != nil {
		t.Fatal(err)
	}
	tail, err := reg.Run(ctx, "docx_read", args)
	if err != nil {
		t.Fatal(err)
	}
	fromNode, err := node.Execute(ctx, args)
	if err != nil || fromNode != tail {
		t.Fatal("agent/node contract differs", err)
	}
	for _, out := range []string{first, tail} {
		if err := automation.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
			t.Fatal(err)
		}
	}
	var last struct {
		Blocks []struct {
			Text string
			Next int `json:"next_text_offset"`
		}
	}
	if err := json.Unmarshal([]byte(tail), &last); err != nil || len(last.Blocks) != 1 || last.Blocks[0].Next != 0 || page.Blocks[0].Text+last.Blocks[0].Text != want {
		t.Fatal("reconstruction failed", err)
	}
	BindWorkflowPolicy(c, ws, permission.Default(), nil, t.TempDir())
	if _, err := c.Enable(ctx, "native_workflows", nil); err != nil {
		t.Fatal(err)
	}
	recipe := `{"version":3,"name":"Continue document","steps":[{"id":"first","tool":"docx_read","args":{"path":"long.docx"}},{"id":"tail","tool":"docx_read","args":{"path":"long.docx","offset":0,"text_offset":{"$step":"first","pointer":"/blocks/0/next_text_offset"}}}],"outputs":{"tail":{"$step":"tail"}}}`
	if err := ws.WriteFile("recipe.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Run(ctx, "workflow_run", json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil || !strings.Contains(out, "TAIL-EVIDENCE") {
		t.Fatal(out, err)
	}
	// A recipe beginning directly at a continuation must still authorize its source.
	recipe = fmt.Sprintf(`{"version":3,"name":"Denied continuation","steps":[{"id":"tail","tool":"docx_read","args":%s}]}`, args)
	if err := ws.WriteFile("denied.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	BindWorkflowPolicy(c, ws, permission.Policy{Rules: []permission.Rule{{Tool: "docx_read", Pattern: "long.docx", Effect: permission.Deny}}}, nil, t.TempDir())
	if _, err := reg.Run(ctx, "workflow_run", json.RawMessage(`{"recipe_path":"denied.json"}`)); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("continuation bypassed policy", err)
	}
	after, err := ws.ReadFile("long.docx")
	if err != nil || !bytes.Equal(after, archive.Bytes()) {
		t.Fatal("source changed", err)
	}
}
