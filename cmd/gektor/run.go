package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/tools"
)

func runConfigured(launch launchRequest) error {
	o, err := resolveLaunch(launch)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return runResolved(launch, o)
}

func runResolved(launch launchRequest, o resolvedLaunch) error {
	if o.contextReport {
		return inspectStartup(o, os.Stdout)
	}
	if o.checkProfile {
		if o.selectedProfile.Identity != nil {
			if _, _, err := app.Compile(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature}); err != nil {
				return err
			}
		}
		return json.NewEncoder(os.Stdout).Encode(o.selectedProfile)
	}
	return runAgentLaunch(launch, o)
}

// discoverMCP starts each -mcp server and returns its tools and clients.
// A spec entry is either name=command args... (stdio) or name=https://...
// (Streamable HTTP). A server that fails to answer risks nothing: it is
// reported and skipped, and the run continues with the rest. Close every
// client when the run ends so no server process outlives it.
func discoverMCP(ctx context.Context, spec, dir string) ([]agent.Tool, []MCPCloser) {
	names, commands, argss, parseErr := tools.ParseMCPFlag(spec)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "mcp: %v (skipped)\n", parseErr)
		return nil, nil
	}
	var out []agent.Tool
	var clients []MCPCloser
	for i, name := range names {
		dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var mcpTools []agent.Tool
		var mcpClient MCPCloser
		var err error
		if tools.IsMCPURL(commands[i]) {
			mcpTools, mcpClient, err = tools.StartMCPHTTP(dctx, name, commands[i])
		} else {
			mcpTools, mcpClient, err = tools.StartMCP(dctx, name, dir, commands[i], argss[i]...)
		}
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp %s: %v (skipped)\n", name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "mcp %s: %d tools\n", name, len(mcpTools))
		out = append(out, mcpTools...)
		clients = append(clients, mcpClient)
	}
	return out, clients
}

// MCPCloser is either MCP transport: stdio kills its process, HTTP ends
// its session. Both are best-effort by design.
type MCPCloser interface {
	Close()
}
