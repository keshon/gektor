package roles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools"
)

func TestFreshPlannerActivatesNativeWebOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fixture")) }))
	defer server.Close()
	args, _ := json.Marshal(map[string]string{"url": server.URL})
	for _, deny := range []bool{false, true} {
		e := testEnv(t)
		e.NativeExposure = map[string]tools.Exposure{tools.NativeWeb: tools.Lazy}
		loads := 0
		e.Capabilities = []*tools.Capability{{ID: "external", ReadOnly: true, Load: func(context.Context, func(string)) ([]agent.Tool, error) {
			loads++
			return []agent.Tool{tools.CheckURL{}}, nil
		}}}
		e.Policy = permission.Default()
		if deny {
			e.Policy = e.Policy.WithExtra([]permission.Rule{{Tool: "enable_capability", Pattern: tools.NativeWeb, Effect: permission.Deny}})
		}
		e.Client = &seqStub{steps: []llm.ChatResponse{
			extensionCall("native", "enable_capability", `{"id":"native_web"}`),
			extensionCall("read", "check_url", string(args)),
			extensionCall("external", "enable_capability", `{"id":"external"}`),
			extensionCall("write", "write_file", `{"path":"no.txt","content":"no"}`), extensionAnswer(),
		}}
		var failed []bool
		e.OnResult = func(r agent.ToolResult) { failed = append(failed, r.Failed) }
		a := Planner(e, "", "", nil)
		if _, err := a.Run(t.Context(), "Inspect the URL"); err != nil {
			t.Fatal(err)
		}
		if len(failed) != 4 || failed[0] != deny || failed[1] != deny || !failed[2] || !failed[3] || loads != 0 {
			t.Fatalf("deny=%v outcomes=%v external loads=%d", deny, failed, loads)
		}
	}
}

func TestRolePolicyRejectsForbiddenDirectExtras(t *testing.T) {
	for _, tool := range []agent.Tool{tools.AskUser{}, &tools.Delegate{}} {
		e := testEnv(t)
		e.MCPTools = []agent.Tool{tool}
		called := false
		e.Client = extensionInspectClient(func(llm.ChatRequest) llm.ChatResponse { called = true; return extensionAnswer() })
		_, err := Subagent(e, "explore").Run(t.Context(), "inspect")
		if err == nil || !strings.Contains(err.Error(), "unavailable to this role") || called {
			t.Fatal("forbidden extra accepted", tool.Name(), err, called)
		}
	}
}

func TestMainOnlyPackIsAbsentFromChildCatalog(t *testing.T) {
	e := testEnv(t)
	loads := 0
	e.Capabilities = []*tools.Capability{{ID: "questions", MainOnly: true, Description: "Questions", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return []agent.Tool{tools.AskUser{}}, nil
	}}}
	step := 0
	e.Client = extensionInspectClient(func(req llm.ChatRequest) llm.ChatResponse {
		step++
		for _, m := range req.Messages {
			if m.Role == llm.RoleSystem && strings.Contains(m.Content, "questions: Questions") {
				t.Error("main-only pack in child map")
			}
			if m.Role == llm.RoleTool && m.ToolCallID == "discover" && strings.Contains(m.Content, "questions") {
				t.Error("main-only pack in discovery")
			}
		}
		switch step {
		case 1:
			return extensionCall("discover", "discover_capabilities", `{}`)
		case 2:
			return extensionCall("enable", "enable_capability", `{"id":"questions"}`)
		}
		return extensionAnswer()
	})
	if _, err := Subagent(e, "explore").Run(t.Context(), "inspect packs"); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatal("role-excluded loader ran")
	}
}
