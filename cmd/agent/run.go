package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/llm"
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
		if o.selectedProfile.Conversation != nil {
			if _, _, err := compileChatSettings(chatOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), GreetingIndex: selectedGreetingIndex(o.selectedProfile), Character: o.characterPath, User: o.userName, System: o.systemPrompt, Post: o.postHistory, PreferMain: o.preferCharacterPrompt, PreferPost: o.preferCharacterPost, MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature}); err != nil {
				return err
			}
		}
		return json.NewEncoder(os.Stdout).Encode(o.selectedProfile)
	}
	if o.chat {
		return runChatLaunch(launch, o)
	}
	return runAgentLaunch(launch, o)
}

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

// resolveAPIKey attaches the bearer token to a remote client. Precedence
// is the flag, then the named env var, then the built-in lookup order.
// Local backends need no key and are left alone.
func resolveAPIKey(flagVal, envName string, client *llm.Server) {
	if strings.TrimSpace(flagVal) != "" {
		client.WithAPIKey(strings.TrimSpace(flagVal))
		return
	}
	if strings.TrimSpace(envName) != "" {
		if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(envName))); v != "" {
			client.WithAPIKey(v)
			return
		}
	}
	if v := llm.APIKeyFromEnv(); v != "" {
		client.WithAPIKey(v)
	}
}

// extraHeadersFromEnv supplies gateway-specific headers without new flags:
// OpenRouter recommends identifying the app on every request.
func extraHeadersFromEnv() map[string]string {
	out := map[string]string{}
	if v := strings.TrimSpace(os.Getenv("OPENROUTER_REFERER")); v != "" {
		out["HTTP-Referer"] = v
	} else if v := strings.TrimSpace(os.Getenv("TARS_SITE_URL")); v != "" {
		out["HTTP-Referer"] = v
	}
	if v := strings.TrimSpace(os.Getenv("TARS_SITE_TITLE")); v != "" {
		out["X-Title"] = v
	}
	return out
}
