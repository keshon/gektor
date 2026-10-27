package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

func inspectStartup(o resolvedLaunch, out io.Writer) error {
	if o.resume != "" || o.fork != "" {
		return fmt.Errorf("-context-report previews a fresh run; resumed requests report their breakdown at runtime")
	}
	var config tools.CapabilityConfig
	if o.capabilities != "" {
		path, err := filepath.Abs(o.capabilities)
		if err != nil {
			return err
		}
		config, err = tools.LoadCapabilityConfig(path)
		if err != nil {
			return err
		}
	}
	ws, err := workspace.NewWithSkillRoots(o.workspace, append(workspace.DefaultSkillRoots(), config.SkillRoots...))
	if err != nil {
		return err
	}
	defer ws.Close()
	sources, close := tools.BuildCapabilities(config, ws.Root())
	defer close()
	identity, warnings, err := app.Compile(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature})
	if err != nil {
		return err
	}
	env := roles.Env{WS: ws, Procs: tools.NewBackgroundProcesses(), Identity: app.IdentityPrompt(identity), Capabilities: sources}
	if o.browser != "" {
		env.Browser = &tools.Browser{Channel: o.browser, Dir: ws.Root()}
	}
	var a *agent.Agent
	if o.plan {
		a = roles.Planner(env, "", "", func(string) (string, error) { return "", nil })
	} else {
		a = roles.Interactive(env, "", "", func(string) (string, error) { return "", nil }, nil)
	}
	report := a.StartupContext(o.task)
	report.Notes = append(report.Notes, warnings...)
	report.Notes = append(report.Notes, "Offline preview uses text-only file schemas; runtime vision detection may change them. No service or model was contacted.")
	if o.mcp != "" {
		report.Notes = append(report.Notes, "Eager -mcp schemas excluded: their catalogs require connecting. Runtime reports include them.")
	}
	if o.images != "" {
		report.Notes = append(report.Notes, "Image attachments excluded from offline preview.")
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
