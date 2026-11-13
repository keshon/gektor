package roles

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	toolbrowser "github.com/keshon/gektor/internal/tools/browser"
)

func TestBrowserIsLazyAcrossRoles(t *testing.T) {
	env := testEnv(t)
	env.Browser = &toolbrowser.Browser{Channel: "chrome", Dir: env.WS.Root()}
	env.Capabilities = append(env.Capabilities, env.Browser.Capability())
	client := &subagentBudgetClient{}
	env.Client = client
	var system string
	interactiveAgent, interactiveAgentErr := Interactive(env, "", "", nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	plannerAgent, plannerAgentErr := Planner(env, "", "", nil)
	if plannerAgentErr != nil {
		t.Fatal(plannerAgentErr)
	}
	childAgent, childAgentErr := Subagent(env, "")
	if childAgentErr != nil {
		t.Fatal(childAgentErr)
	}
	for _, a := range []*agent.Agent{interactiveAgent, plannerAgent, childAgent} {
		if !hasTool(a, "enable_capability") || hasTool(a, "enable_browser") || len(toolbrowser.BrowserToolNames(env.MCPTools)) != 0 {
			t.Fatal("browser entry point missing")
		}
		if _, err := a.Run(t.Context(), "hello"); err != nil {
			t.Fatal(err)
		}
		text := client.history[0].Content
		if !strings.Contains(text, "search discover_capabilities for a configured browser integration") {
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
