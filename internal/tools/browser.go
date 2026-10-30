package tools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

const browserPackage = "@playwright/mcp@0.0.83"

// PrepareBrowser makes first use explicit and keeps npm setup outside the MCP
// handshake timeout. Only the pinned package in npm's cache is installed.
func PrepareBrowser(ctx context.Context, channel, dir string, note func(string)) error {
	if channel != "chrome" && channel != "msedge" {
		return fmt.Errorf("browser must be chrome or msedge")
	}
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("browser requires Node.js 20+ with npm on PATH; install Node.js LTS from https://nodejs.org/en/download, reopen the terminal and retry (or use -browser=)")
	}
	if _, err := exec.LookPath("npx"); err != nil {
		return fmt.Errorf("browser requires npx from npm on PATH; repair the Node.js/npm installation and reopen the terminal (or use -browser=)")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	version, err := exec.CommandContext(probeCtx, "node", "-p", "process.versions.node").CombinedOutput()
	cancel()
	var major int
	if _, parseErr := fmt.Sscanf(strings.TrimSpace(string(version)), "%d.", &major); err != nil || parseErr != nil || major < 20 {
		return fmt.Errorf("browser requires working Node.js 20+; detected %q (%v); install Node.js LTS and reopen the terminal", strings.TrimSpace(string(version)), err)
	}
	found := false
	for _, path := range installedBrowsers() {
		name := strings.ToLower(path)
		if channel == "chrome" && strings.Contains(name, "chrome") || channel == "msedge" && (strings.Contains(name, "msedge") || strings.Contains(name, "microsoft edge") || strings.Contains(name, "microsoft-edge")) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("browser %s is not installed in a standard location or PATH; install it, select the other channel with -browser chrome/-browser msedge, or disable with -browser=", channel)
	}
	return prepareBrowserPackage(ctx, note, func(ctx context.Context, install bool) (string, error) {
		mode := "--no-install"
		if install {
			mode = "--yes"
		}
		command := "npx"
		if runtime.GOOS == "windows" {
			command = "npx.cmd"
		}
		return runCommand(ctx, command+" "+mode+" "+browserPackage+" --help", dir, 3*time.Minute, nil)
	})
}

func prepareBrowserPackage(ctx context.Context, note func(string), run func(context.Context, bool) (string, error)) error {
	if note == nil {
		note = func(string) {}
	}
	note("browser: checking cached " + browserPackage + "...")
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err := run(probeCtx, false)
	cancel()
	if err == nil {
		note("browser: Playwright MCP is ready")
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	note("browser: preparing " + browserPackage + " in npm cache (first use needs internet; up to 3 minutes). Node.js and the browser are not installed or changed by this step.")
	setupCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := run(setupCtx, true)
	if err != nil {
		return fmt.Errorf("browser setup failed: %w\n%s\nCheck network/npm access and retry; use -browser= to run without a browser", err, agent.TruncateMiddle(strings.TrimSpace(out), 2048))
	}
	note("browser: Playwright MCP prepared; connecting browser tools...")
	return nil
}

// StartBrowser retains an isolated headless entry point for automated checks.
func StartBrowser(ctx context.Context, channel, dir string) ([]agent.Tool, *MCPClient, error) {
	return startBrowser(ctx, channel, dir, "", true)
}

func startBrowser(ctx context.Context, channel, dir, profileDir string, headless bool) ([]agent.Tool, *MCPClient, error) {
	if channel != "chrome" && channel != "msedge" {
		return nil, nil, fmt.Errorf("browser must be chrome or msedge")
	}
	command := "npx"
	args := browserArgs(channel, profileDir, headless)
	if runtime.GOOS == "windows" {
		command = "cmd.exe"
		args = append([]string{"/d", "/c", "npx"}, args...)
	}
	list, client, err := StartMCP(ctx, "browser", dir, command, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("browser connection failed: %w; see docs/browser.md or use -browser= to run without a browser", err)
	}
	if len(BrowserToolNames(list)) == 0 {
		client.Close()
		return nil, nil, fmt.Errorf("browser MCP returned no browser tools")
	}
	return list, client, nil
}

func browserArgs(channel, profileDir string, headless bool) []string {
	args := []string{"--no-install", browserPackage, "--browser", channel, "--image-responses", "allow", "--output-dir", ".gektor/browser"}
	if headless {
		args = append(args, "--headless")
	}
	if profileDir == "" {
		args = append(args, "--isolated")
	} else {
		args = append(args, "--user-data-dir", profileDir)
	}
	return args
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
