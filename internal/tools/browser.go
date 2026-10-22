package tools

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/keshon/gektor/internal/agent"
)

const browserPackage = "@playwright/mcp@0.0.83"

// StartBrowser uses the existing MCP transport and a separate, ephemeral browser
// profile. --no-install permits cached packages but never downloads dependencies.
func StartBrowser(ctx context.Context, channel, dir string) ([]agent.Tool, *MCPClient, error) {
	if channel != "chrome" && channel != "msedge" {
		return nil, nil, fmt.Errorf("browser must be chrome or msedge")
	}
	command := "npx"
	args := []string{"--no-install", browserPackage, "--browser", channel, "--headless", "--isolated", "--image-responses", "allow", "--output-dir", ".gektor/browser"}
	if runtime.GOOS == "windows" {
		command = "cmd.exe"
		args = append([]string{"/d", "/c", "npx"}, args...)
	}
	list, client, err := StartMCP(ctx, "browser", dir, command, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("browser connection failed: %w; install Node.js and %s, then run npx -y %s --help once; see docs/browser.md", err, channel, browserPackage)
	}
	if len(BrowserToolNames(list)) == 0 {
		client.Close()
		return nil, nil, fmt.Errorf("browser MCP returned no browser tools")
	}
	return list, client, nil
}

func BrowserToolNames(list []agent.Tool) []string {
	var names []string
	for _, tool := range list {
		if remote, ok := tool.(*MCPTool); ok && strings.HasPrefix(remote.def.Name, "browser_") {
			names = append(names, tool.Name())
		}
	}
	return names
}

func BrowserContext(list []agent.Tool) string {
	if len(BrowserToolNames(list)) == 0 {
		return "\nBrowser automation: unavailable (no connected browser MCP tools). Report browser checks as unverified; do not improvise Chrome/headless shell commands. The operator can reconnect with -browser chrome or -browser msedge."
	}
	return "\nBrowser automation: connected over MCP. Use the provided browser tools; first navigation verifies launch. Check interactions and console before claiming the page works."
}
