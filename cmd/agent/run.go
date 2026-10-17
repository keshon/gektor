package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/tools"
)

func runConfigured(launch launchRequest) error {
	o, err := resolveLaunch(launch)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
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
func discoverMCP(ctx context.Context, spec string) ([]agent.Tool, []MCPCloser) {
	names, commands, argss := tools.ParseMCPFlag(spec)
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
			mcpTools, mcpClient, err = tools.StartMCP(dctx, name, commands[i], argss[i]...)
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
