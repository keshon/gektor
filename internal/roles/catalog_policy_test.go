package roles

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools"
	toolaskuser "github.com/keshon/gektor/internal/tools/askuser"
	toolcheckurl "github.com/keshon/gektor/internal/tools/checkurl"
	tooldelegate "github.com/keshon/gektor/internal/tools/delegate"
)

func TestPlannerCanInspectZIPButCannotWriteFiles(t *testing.T) {
	e := testEnv(t)
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	if _, err := w.Create("hello.txt"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.WS.WriteFile("input.zip", data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	e.Client = &seqStub{steps: []llm.ChatResponse{
		extensionCall("enable", "enable_capability", `{"id":"native_files"}`),
		extensionCall("list", "archive_list", `{"path":"input.zip"}`),
		extensionCall("download", "download_file", `{"url":"https://example.com/file","path":"download.bin"}`),
		extensionCall("create", "archive_create", `{"paths":["input.zip"],"path":"bundle.zip"}`),
		extensionCall("extract", "archive_extract", `{"path":"input.zip","out_dir":"extracted"}`), extensionAnswer(),
	}}
	var results []agent.ToolResult
	e.OnResult = func(r agent.ToolResult) { results = append(results, r) }
	plannerAgent, plannerAgentErr := Planner(e, "", "", nil)
	if plannerAgentErr != nil {
		t.Fatal(plannerAgentErr)
	}
	if _, err := plannerAgent.Run(t.Context(), "Inspect ZIP without writing"); err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 || results[0].Failed || results[1].Failed || !results[2].Failed || !results[3].Failed || !results[4].Failed || !strings.Contains(results[1].Output, "hello.txt") {
		t.Fatal(results)
	}
	if _, err := e.WS.Stat("download.bin"); !os.IsNotExist(err) {
		t.Fatal("Plan wrote a download", err)
	}
	for _, path := range []string{"bundle.zip", "extracted"} {
		if _, err := e.WS.Stat(path); !os.IsNotExist(err) {
			t.Fatal("Plan wrote archive output", path, err)
		}
	}
}

func TestInteractiveDownloadHonorsURLAndDestinationDenies(t *testing.T) {
	for _, resource := range []string{"https://example.com/file", "download.bin"} {
		t.Run(resource, func(t *testing.T) {
			e := testEnv(t)
			e.Policy = permission.Default().WithExtra([]permission.Rule{{Tool: "download_file", Pattern: resource, Effect: permission.Deny}})
			e.Client = &seqStub{steps: []llm.ChatResponse{
				extensionCall("enable", "enable_capability", `{"id":"native_files"}`),
				extensionCall("download", "download_file", `{"url":"https://example.com/file#ignored","path":"download.bin"}`), extensionAnswer(),
			}}
			var results []agent.ToolResult
			e.OnResult = func(r agent.ToolResult) { results = append(results, r) }
			interactiveAgent, interactiveAgentErr := Interactive(e, "", "", nil, nil)
			if interactiveAgentErr != nil {
				t.Fatal(interactiveAgentErr)
			}
			if _, err := interactiveAgent.Run(t.Context(), "Try the download"); err != nil {
				t.Fatal(err)
			}
			if len(results) != 2 || results[0].Failed || !results[1].Failed || !strings.Contains(results[1].Output, "blocked by policy") {
				t.Fatal(results)
			}
			if _, err := e.WS.Stat("download.bin"); !os.IsNotExist(err) {
				t.Fatal("denied call wrote output", err)
			}
		})
	}
}

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
			return []agent.Tool{toolcheckurl.Tool{}}, nil
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
		a, aErr := Planner(e, "", "", nil)
		if aErr != nil {
			t.Fatal(aErr)
		}
		if _, err := a.Run(t.Context(), "Inspect the URL"); err != nil {
			t.Fatal(err)
		}
		if len(failed) != 4 || failed[0] != deny || failed[1] != deny || !failed[2] || !failed[3] || loads != 0 {
			t.Fatalf("deny=%v outcomes=%v external loads=%d", deny, failed, loads)
		}
	}
}

func TestRolePolicyRejectsForbiddenDirectExtras(t *testing.T) {
	for _, tool := range []agent.Tool{toolaskuser.Tool{}, &tooldelegate.Tool{}} {
		e := testEnv(t)
		e.MCPTools = []agent.Tool{tool}
		called := false
		e.Client = extensionInspectClient(func(llm.ChatRequest) llm.ChatResponse { called = true; return extensionAnswer() })
		a, err := Subagent(e, "explore")
		if a != nil || err == nil || !strings.Contains(err.Error(), "unavailable to this role") || called {
			t.Fatal("forbidden extra accepted", tool.Name(), err, called)
		}
	}
}

func TestMainOnlyPackIsAbsentFromChildCatalog(t *testing.T) {
	e := testEnv(t)
	loads := 0
	e.Capabilities = []*tools.Capability{{ID: "questions", MainOnly: true, Description: "Questions", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return []agent.Tool{toolaskuser.Tool{}}, nil
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
	subagentAgent, subagentAgentErr := Subagent(e, "explore")
	if subagentAgentErr != nil {
		t.Fatal(subagentAgentErr)
	}
	if _, err := subagentAgent.Run(t.Context(), "inspect packs"); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatal("role-excluded loader ran")
	}
}

func TestRoleConstructionRejectsCompositionBeforeActivation(t *testing.T) {
	for _, role := range []string{"interactive", "planner", "subagent"} {
		t.Run(role, func(t *testing.T) {
			e := testEnv(t)
			loads := 0
			e.Capabilities = []*tools.Capability{{ID: "external", Load: func(context.Context, func(string)) ([]agent.Tool, error) { loads++; return nil, nil }}}
			e.NativeExposure = map[string]tools.Exposure{"unknown-package": tools.Eager}
			called := false
			e.Client = extensionInspectClient(func(llm.ChatRequest) llm.ChatResponse { called = true; return extensionAnswer() })
			var a *agent.Agent
			var err error
			switch role {
			case "interactive":
				a, err = Interactive(e, "", "", nil, nil)
			case "planner":
				a, err = Planner(e, "", "", nil)
			case "subagent":
				a, err = Subagent(e, "inspect")
			}
			if a != nil || err == nil || !strings.Contains(err.Error(), "unsupported native exposure") || loads != 0 || called {
				t.Fatalf("agent=%v error=%v loads=%d model=%v", a, err, loads, called)
			}
		})
	}
}
