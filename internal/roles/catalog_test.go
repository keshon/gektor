package roles

import (
	"context"
	"encoding/json"
	"github.com/keshon/gektor/internal/tools"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	toolaskuser "github.com/keshon/gektor/internal/tools/askuser"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

func TestNativeWebActivationCompactResumeAndPlan(t *testing.T) {
	e := testEnv(t)
	e.NativeExposure = map[string]toolextension.Exposure{tools.NativeWeb: toolextension.Lazy}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("native fixture")) }))
	defer server.Close()
	args, _ := json.Marshal(map[string]string{"url": server.URL})
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("enable", "enable_capability", `{"id":"native_web"}`), extensionCall("read", "check_url", string(args)), extensionAnswer()}}
	state := filepath.Join(t.TempDir(), "state.json")
	a, aErr := Interactive(e, "", state, nil, nil)
	if aErr != nil {
		t.Fatal(aErr)
	}
	if hasTool(a, "check_url") {
		t.Fatal("lazy tools visible before activation")
	}
	if _, err := a.Run(t.Context(), "Check the fixture URL"); err != nil {
		t.Fatal(err)
	}
	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Capabilities) != 1 || saved.Capabilities[0].ID != tools.NativeWeb {
		t.Fatal("native activation not saved")
	}
	if _, _, _, err := agent.CompactFile(state, 1); err != nil {
		t.Fatal(err)
	}
	saved, err = conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	// Restore before eager preparation, and enforce Plan on the same catalog.
	e.NativeExposure = nil
	var failures []bool
	e.OnResult = func(r agent.ToolResult) { failures = append(failures, r.Failed) }
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("read-again", "check_url", string(args)), extensionCall("write", "write_file", `{"path":"forbidden.txt","content":"no"}`), extensionAnswer()}}
	plannerAgent, plannerAgentErr := Planner(e, "", state, nil)
	if plannerAgentErr != nil {
		t.Fatal(plannerAgentErr)
	}
	if _, err := plannerAgent.Resume(t.Context(), saved.Context(), "Inspect only"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(failures, []bool{false, true}) {
		t.Fatalf("Plan outcomes: %v", failures)
	}
	if _, err := os.Stat(filepath.Join(e.WS.Root(), "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("Plan wrote a file", err)
	}
	// A fresh child does not inherit the parent's lazy activation.
	e.NativeExposure = map[string]toolextension.Exposure{tools.NativeWeb: toolextension.Lazy}
	subagentAgent, subagentAgentErr := Subagent(e, "explore")
	if subagentAgentErr != nil {
		t.Fatal(subagentAgentErr)
	}
	if hasTool(subagentAgent, "check_url") {
		t.Fatal("child inherited active web")
	}
}

func TestRolePackageCompositionAndExcludedActivation(t *testing.T) {
	e := testEnv(t)
	ask := func(context.Context, string) (string, error) { return "ok", nil }
	act, actErr := Interactive(e, "", "", ask, nil)
	if actErr != nil {
		t.Fatal(actErr)
	}
	plan, planErr := Planner(e, "", "", ask)
	if planErr != nil {
		t.Fatal(planErr)
	}
	if !reflect.DeepEqual(act.ToolNames(), plan.ToolNames()) {
		t.Fatal("Plan/Act catalogs diverged")
	}
	child, childErr := Subagent(e, "explore")
	if childErr != nil {
		t.Fatal(childErr)
	}
	if hasTool(child, "ask_user") || hasTool(child, "delegate_task") || !hasTool(child, "todo") {
		t.Fatal("child role changed")
	}
	e.Capabilities = []*toolextension.Capability{{ID: "questions", Description: "Ask", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		return []agent.Tool{toolaskuser.Tool{AskFn: ask}}, nil
	}}}
	var got llm.ChatRequest
	step := 0
	e.Client = extensionInspectClient(func(req llm.ChatRequest) llm.ChatResponse {
		step++
		got = req
		if step == 1 {
			return extensionCall("enable", "enable_capability", `{"id":"questions"}`)
		}
		return extensionAnswer()
	})
	subagentAgent, subagentAgentErr := Subagent(e, "explore")
	if subagentAgentErr != nil {
		t.Fatal(subagentAgentErr)
	}
	if _, err := subagentAgent.Run(t.Context(), "Try the configured pack"); err != nil {
		t.Fatal(err)
	}
	for _, def := range got.Tools {
		if def.Name == "ask_user" {
			t.Fatal("child acquired ask_user")
		}
	}
	found := false
	for _, m := range got.Messages {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "unavailable to this role") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing role exclusion diagnostic")
	}
}
