package roles

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/tools"
)

func TestBrowserIsLazyAcrossRoles(t *testing.T) {
	env := testEnv(t)
	env.Browser = &tools.Browser{Channel: "chrome", Dir: env.WS.Root()}
	client := &subagentBudgetClient{}
	env.Client = client
	var system string
	for _, a := range []*agent.Agent{Interactive(env, "", "", nil, nil), Planner(env, "", "", nil), Subagent(env, "")} {
		if !hasTool(a, "enable_browser") || len(tools.BrowserToolNames(env.MCPTools)) != 0 {
			t.Fatal("browser entry point missing")
		}
		if _, err := a.Run(t.Context(), "hello"); err != nil {
			t.Fatal(err)
		}
		text := client.history[0].Content
		if !strings.Contains(text, "available on request through enable_browser") {
			t.Fatal("browser capability not advertised")
		}
		if system != "" && !strings.Contains(text, "Your budget is") && system != text {
			t.Fatal("Plan/Act changed the prefix")
		}
		if system == "" {
			system = text
		}
	}
}
