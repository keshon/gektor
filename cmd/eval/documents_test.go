package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

func TestDocumentEvalFixturesThroughAgentAndWorkflow(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	inputs := map[string][]byte{}
	for _, name := range []string{"brief.docx", "sales.xlsx", "approval.pdf"} {
		data, err := os.ReadFile(filepath.Join("../../eval/fixtures-documents", name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
		if err := ws.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	capabilities := &tools.Capabilities{}
	registry, err := tools.Compose(ws, nil, nil, nil, false, capabilities, nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities.Registry = registry
	if _, err := capabilities.Enable(t.Context(), "native_documents", nil); err != nil {
		t.Fatal(err)
	}
	nodes, err := app.AutomationRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ tool, args, want string }{
		{"docx_read", `{"path":"brief.docx"}`, "Cedar Works"},
		{"xlsx_read", `{"path":"sales.xlsx","sheet":"Sales"}`, "3694"},
		{"pdf_read", `{"path":"approval.pdf"}`, "ORBIT-52"},
	} {
		out, err := registry.Run(t.Context(), call.tool, json.RawMessage(call.args))
		if err != nil || !strings.Contains(out, call.want) {
			t.Fatal(call.tool, out, err)
		}
		node, _ := nodes.Node(call.tool)
		if err := automation.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
			t.Fatal(call.tool, err)
		}
	}
	recipe := `{"version":3,"name":"Read fixture documents","steps":[{"id":"brief","tool":"docx_read","args":{"path":"brief.docx"}},{"id":"sales","tool":"xlsx_read","args":{"path":"sales.xlsx","sheet":"Sales"}},{"id":"approval","tool":"pdf_read","args":{"path":"approval.pdf"}}],"outputs":{"brief":{"$step":"brief"},"sales":{"$step":"sales"},"approval":{"$step":"approval"}}}`
	if err := ws.WriteFile("recipe.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	tool := tools.NewWorkflow(ws, false, permission.Default(), nil)
	tool.StoreDir = t.TempDir()
	out, err := tool.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`))
	if err != nil {
		t.Fatal(out, err)
	}
	for _, want := range []string{"Cedar Works", "INVOICE-73", "1847", "3694", "ORBIT-52"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
	for name, before := range inputs {
		after, err := ws.ReadFile(name)
		if err != nil || string(after) != string(before) {
			t.Fatal("document source changed", name, err)
		}
	}
}
