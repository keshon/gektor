package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/nodes/script"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func TestFrozenRecipeDiagnosticsKeepChildSource(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	nodes := append(callnode.Definitions(), script.Definition())
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	root := []byte(`{"version":4,"name":"Root","steps":[{"id":"shared","tool":"workflow_call","args":{"recipe_path":"child.json"}}]}`)
	child := []byte(`{"version":4,"name":"Child","steps":[{"id":"shared","tool":"script_transform","args":{"input":1,"code":3}}],"outputs":{"value":{"$step":"shared"}}}`)
	err = ws.WriteFile("child.json", child, 0600)
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("child read denied")
	denyRead := false
	svc := Service{
		WS:       ws,
		Registry: registry,
		Authorize: func(_ context.Context, _ string, _ json.RawMessage) error {
			if denyRead {
				return denied
			}
			return nil
		},
	}
	frozen, err := svc.FreezeRecipe(t.Context(), root, "root.json")
	if err != nil {
		t.Fatal(err)
	}
	// Compile must diagnose the frozen child, even if its saved file has changed.
	err = ws.WriteFile("child.json", []byte(`{}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Compile(t.Context(), frozen, "root.json")
	if err == nil {
		t.Fatal("invalid frozen arguments accepted")
	}
	diagnostics := workflow.Diagnose(err, workflow.Definition{})
	if len(diagnostics) != 1 || diagnostics[0].RecipePath != "child.json" || diagnostics[0].StepID != "shared" || diagnostics[0].Pointer != "/args/code" {
		t.Fatal(diagnostics)
	}

	denyRead = true
	_, _, err = svc.Compile(t.Context(), frozen, "root.json")
	diagnostics = workflow.Diagnose(err, workflow.Definition{})
	if !errors.Is(err, denied) || len(diagnostics) != 1 || diagnostics[0].RecipePath != "child.json" || diagnostics[0].StepID != "" {
		t.Fatal(diagnostics, err)
	}
}
