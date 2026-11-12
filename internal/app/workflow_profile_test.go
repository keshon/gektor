package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/config"
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

func TestWorkflowModelLoadsProfileOnExecutionAndPreservesLimits(t *testing.T) {
	type observedRequest struct {
		MaxTokens     int     `json:"max_tokens"`
		Temperature   float64 `json:"temperature"`
		Model         string  `json:"model"`
		Path          string  `json:"-"`
		Authorization string  `json:"-"`
	}
	requests := make(chan observedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request observedRequest
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request.Path = r.URL.Path
		request.Authorization = r.Header.Get("Authorization")
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"model reply"},"finish_reason":"stop"}]}`))
		if err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	t.Setenv("GEKTOR_WORKFLOW_TEST_KEY", "workflow-test-key")

	ws, err := workspace.New(t.TempDir())
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

	profile := config.DefaultProfile()
	profile.Provider = config.Provider{Kind: "openai", URL: server.URL + "/v1", Model: "workflow-test", APIKeyEnv: "GEKTOR_WORKFLOW_TEST_KEY"}
	profile.Generation = config.Generation{MaxTokens: 73, ContextLimit: 4096, Temperature: 0.6}
	path := filepath.Join(ws.Root(), "model.json")
	err = config.SaveCopy(profile, path)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(struct {
		Profile string `json:"profile"`
		Prompt  string `json:"prompt"`
	}{Profile: path, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	resources, err := node.Resources(args)
	if err != nil || len(resources) != 1 || resources[0] != "model.json" {
		t.Fatalf("profile resources = %v, err=%v", resources, err)
	}
	if len(requests) != 0 {
		t.Fatal("composition or resource resolution contacted the backend")
	}

	out, err := node.Execute(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	var request observedRequest
	select {
	case request = <-requests:
	default:
		t.Fatal("execution did not contact the backend")
	}
	if request.Path != "/v1/chat/completions" || request.Model != "workflow-test" || request.MaxTokens != 73 || request.Temperature != 0.6 || request.Authorization != "Bearer workflow-test-key" {
		t.Fatalf("profile settings were not retained: %+v", request)
	}
	if !strings.Contains(out, "model reply") || !strings.Contains(out, "openai/workflow-test") {
		t.Fatalf("model result = %s", out)
	}

	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte(strings.Repeat(" ", 64<<10))...)
	err = os.WriteFile(path, raw, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = node.Execute(t.Context(), args)
	if err == nil || !strings.Contains(err.Error(), "profile too large") || len(requests) != 0 {
		t.Fatal("oversized profile reached the backend", err)
	}
}
