package roles

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/tools"
	toolbrowser "github.com/keshon/gektor/internal/tools/browser"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

func (e Env) browserContext() string {
	if (len(e.Capabilities) > 0 || e.CapabilitySources != nil) && len(toolbrowser.BrowserToolNames(e.MCPTools)) == 0 {
		return "\nBrowser automation: search discover_capabilities for a configured browser integration when needed. Use browser tools only after activation succeeds; if none are configured, report browser checks as unverified."
	}
	return toolbrowser.BrowserContext(e.MCPTools)
}

func (e Env) configureTools(cfg *agent.Config, mainAgent, allowQuestions bool, extra ...agent.Tool) error {
	configured := append([]*toolextension.Capability(nil), e.Capabilities...)
	if e.CapabilitySources != nil {
		configured = append(configured, e.CapabilitySources()...)
	}
	sources := tools.WithBuiltinCapabilities(configured)
	catalog := &toolextension.Capabilities{Sources: sources, Subagent: !mainAgent}
	if !mainAgent {
		catalog.ForbiddenTools = append(catalog.ForbiddenTools, "delegate_task")
	}
	if !allowQuestions {
		catalog.ForbiddenTools = append(catalog.ForbiddenTools, "ask_user")
	}
	// Native optional packs exist even without external capability sources.
	extra = append(extra, toolextension.DiscoverCapabilities{Catalog: catalog}, toolextension.EnableCapability{Catalog: catalog})
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
	return nil
}
