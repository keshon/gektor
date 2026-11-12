package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/workflow"
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
	capabilities := &toolextension.Capabilities{}
	registry, err := tools.Compose(tools.Dependencies{WS: ws, Vision: false}, capabilities, nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilities.Registry = registry
	if _, err := capabilities.Enable(t.Context(), "native_documents", nil); err != nil {
		t.Fatal(err)
	}
	nodes, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
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
		if err := workflow.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
			t.Fatal(call.tool, err)
		}
	}
	recipe := `{"version":3,"name":"Read fixture documents","steps":[{"id":"brief","tool":"docx_read","args":{"path":"brief.docx"}},{"id":"sales","tool":"xlsx_read","args":{"path":"sales.xlsx","sheet":"Sales"}},{"id":"approval","tool":"pdf_read","args":{"path":"approval.pdf"}}],"outputs":{"brief":{"$step":"brief"},"sales":{"$step":"sales"},"approval":{"$step":"approval"}}}`
	if err := ws.WriteFile("recipe.json", []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := app.NewWorkflow(ws, t.TempDir(), permission.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := tools.NewWorkflow(service)
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

func TestContinuationEvalRequiresHiddenEvidence(t *testing.T) {
	files, err := generatedFixture("documents_continuation")
	if err != nil {
		t.Fatal(err)
	}
	again, err := generatedFixture("documents_continuation")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for name, data := range files {
		if string(data) != string(again[name]) {
			t.Fatal("fixture is not deterministic", name)
		}
		if err := ws.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tool, path, key, marker string }{
		{"docx_read", "release.docx", "blocks", "EMBER-7391"},
		{"xlsx_read", "audit.xlsx", "cells", "QUARTZ-2846"},
		{"pdf_read", "approval.pdf", "pages", "HARBOR-6152"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			node, _ := nodes.Node(tc.tool)
			args := map[string]any{"path": tc.path}
			if tc.tool == "xlsx_read" {
				args["sheet"] = "Audit"
			}
			call, _ := json.Marshal(args)
			out, err := node.Execute(t.Context(), call)
			if err != nil || strings.Contains(out, tc.marker) {
				t.Fatal("evidence must be absent from initial page", err)
			}
			if err := workflow.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result["more"] != false {
				t.Fatal("fixture must exercise more=false")
			}
			items := result[tc.key].([]any)
			if len(items) != 1 {
				t.Fatal("expected exactly one item")
			}
			next := items[0].(map[string]any)["next_text_offset"]
			if next == nil {
				t.Fatal("fixture did not require continuation")
			}
			args["offset"], args["text_offset"] = 0, next
			call, _ = json.Marshal(args)
			if err := workflow.ValidateJSON(call, node.InputSchema); err != nil {
				t.Fatal(err)
			}
			out, err = node.Execute(t.Context(), call)
			if err != nil || !strings.Contains(out, tc.marker) {
				t.Fatal("continuation lost hidden evidence", err)
			}
			if err := workflow.ValidateJSON(json.RawMessage(out), node.OutputSchema); err != nil {
				t.Fatal(err)
			}
			after, err := ws.ReadFile(tc.path)
			if err != nil || string(after) != string(files[tc.path]) {
				t.Fatal("source changed", err)
			}
		})
	}
	// Correct prose without continuation calls must fail the frozen trace criteria.
	root, _ := filepath.Abs("../..")
	t.Chdir(root)
	probes, err := loadProbes(filepath.Join(root, "eval/probes"), "39-documents-continuation")
	if err != nil || len(probes) != 1 {
		t.Fatal(probes, err)
	}
	if failures := checkTrace(probes[0], nil); len(failures) != 3 {
		t.Fatal("missing continuation calls were not rejected", failures)
	}
}
