package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowModelRejectsUnknownProfileSettings(t *testing.T) {
	root := t.TempDir()
	profile := `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:9931","model":"local"},"generation":{"max_tokens":256,"context_limit":4096,"temprature":0.7}}`
	if err := os.WriteFile(filepath.Join(root, "model.json"), []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	registry, err := WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node, err := registry.Node("model_generate")
	if err != nil {
		t.Fatal(err)
	}
	_, err = node.Execute(t.Context(), json.RawMessage(`{"profile":"model.json","prompt":"test"}`))
	if err == nil || !strings.Contains(err.Error(), "temprature") {
		t.Fatal("workflow discarded invalid setting", err)
	}
}
