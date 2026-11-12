package roles

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/tools"
)

func (e Env) browserContext() string {
	if e.Browser != nil {
		return "\nBrowser automation: available on request through enable_browser. Call it when browser work is needed. If browser tools are already present, use them directly."
	}
	if len(e.Capabilities) > 0 && len(tools.BrowserToolNames(e.MCPTools)) == 0 {
		return "\nBrowser automation: search discover_capabilities for a configured browser integration when needed. Use browser tools only after activation succeeds; if none are configured, report browser checks as unverified."
	}
	return tools.BrowserContext(e.MCPTools)
}

func (e Env) configureTools(cfg *agent.Config, mainAgent, allowQuestions bool, extra ...agent.Tool) error {
	configured := append([]*tools.Capability(nil), e.Capabilities...)
	if e.CapabilitySources != nil {
		configured = append(configured, e.CapabilitySources()...)
	}
	sources := tools.WithBuiltinCapabilities(configured)
	if e.Browser != nil {
		sources = append(sources, e.Browser.Capability())
	}
	catalog := &tools.Capabilities{Sources: sources, Subagent: !mainAgent}
	if !mainAgent {
		catalog.ForbiddenTools = append(catalog.ForbiddenTools, "delegate_task")
	}
	if !allowQuestions {
		catalog.ForbiddenTools = append(catalog.ForbiddenTools, "ask_user")
	}
	var browser *tools.EnableBrowser
	if e.Browser != nil {
		browser = &tools.EnableBrowser{Browser: e.Browser, Catalog: catalog}
		extra = append(extra, browser)
	}
	// Native optional packs exist even without external capability sources.
	extra = append(extra, tools.DiscoverCapabilities{Catalog: catalog}, tools.EnableCapability{Catalog: catalog}, agent.InvokeTool{})
	workflow, err := app.NewWorkflow(e.WS, e.WorkflowDir, e.policy(), e.Gate)
	if err != nil {
		return err
	}

	cfg.Tools, err = tools.Compose(tools.Dependencies{
		WS:       e.WS,
		Procs:    e.Procs,
		Checks:   e.Checks,
		Vision:   e.Vision,
		Extra:    extra,
		StoreDir: e.WorkflowDir,
		Policy:   e.policy(),
		Gate:     e.Gate,
		Workflow: workflow,
	}, catalog, e.NativeExposure)
	if err != nil {
		return err
	}
	catalog.Registry = cfg.Tools
	cfg.Catalog = catalog
	cfg.System += catalog.Map()
	if browser != nil {
		browser.Registry = cfg.Tools
	}
	return nil
}
