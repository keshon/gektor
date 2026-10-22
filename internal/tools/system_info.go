package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// SystemInfo discovers the host on demand, without probing arbitrary programs.
type SystemInfo struct {
	WS           *workspace.Workspace
	BrowserTools []string
}

func (SystemInfo) Name() string         { return "system_info" }
func (SystemInfo) Mode() agent.ToolMode { return agent.Concurrent }
func (SystemInfo) Description() string {
	return "Inspect one group: os, hardware, environment, tools, or browser. tools lists executables in PATH with name filter and paging. browser reports installed browser paths, Playwright executables and browser tools discovered over MCP; npx alone does not prove Playwright is ready. No credential dump or automatic installation."
}
func (SystemInfo) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"group":{"type":"string","enum":["os","hardware","environment","tools","browser"]},"filter":{"type":"string","description":"Case-insensitive tool filename substring"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100,"default":50}},"required":["group"],"additionalProperties":false}`)
}
func (t SystemInfo) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Group  string `json:"group"`
		Filter string `json:"filter"`
		Offset int    `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := rejectUnknownFields(args, "group", "filter", "offset", "limit"); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	limit := 50
	if in.Limit != nil {
		limit = *in.Limit
	}
	if in.Offset < 0 || limit < 1 || limit > 100 {
		return "", fmt.Errorf("offset must be >= 0; limit must be 1–100")
	}
	if in.Group != "tools" && (in.Filter != "" || in.Offset != 0 || in.Limit != nil) {
		return "", fmt.Errorf("filter, offset and limit apply only to tools")
	}
	var result any
	switch in.Group {
	case "browser":
		result = t.browserInfo(ctx)
	case "environment":
		result = map[string]any{"workspace": t.WS.Root(), "temp_dir": os.TempDir(), "path": filepath.SplitList(os.Getenv("PATH")), "pathext": os.Getenv("PATHEXT")}
	case "tools":
		entries, warnings, err := pathTools(ctx, t.WS.Root(), in.Filter)
		if err != nil {
			return "", err
		}
		start := min(in.Offset, len(entries))
		end := start + min(limit, len(entries)-start)
		page := map[string]any{"tools": entries[start:end], "total": len(entries), "warnings": warnings}
		if end < len(entries) {
			page["next_offset"] = end
		}
		result = page
	case "os", "hardware":
		result = hostInfo(ctx, in.Group, t.WS.Root())
	default:
		return "", fmt.Errorf("group must be os, hardware, environment, tools or browser")
	}
	b, err := json.Marshal(result)
	return string(b), err
}

func (t SystemInfo) browserInfo(ctx context.Context) map[string]any {
	installed := []string{}
	paths := []string{}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge", "firefox"} {
		if path, err := exec.LookPath(name); err == nil {
			paths = append(paths, path)
		}
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			for _, suffix := range []string{"Google/Chrome/Application/chrome.exe", "Microsoft/Edge/Application/msedge.exe", "Mozilla Firefox/firefox.exe"} {
				paths = append(paths, filepath.Join(base, filepath.FromSlash(suffix)))
			}
		}
	} else if runtime.GOOS == "darwin" {
		paths = append(paths, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "/Applications/Firefox.app/Contents/MacOS/firefox")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && !seen[path] {
			installed = append(installed, path)
			seen[path] = true
		}
	}
	executables, warnings, _ := pathTools(ctx, t.WS.Root(), "playwright")
	status := "not connected: no browser MCP tools discovered; do not assume npx means Playwright is installed"
	if len(t.BrowserTools) > 0 {
		status = "browser MCP tools discovered at startup; first navigation still verifies browser launch"
	}
	return map[string]any{"browser_paths": installed, "playwright_executables": executables, "mcp_tools": t.BrowserTools, "status": status, "operator_setup": "Restart GEKTOR with -browser chrome or -browser msedge; see docs/browser.md. Do not improvise browser shell commands when MCP is unavailable.", "package_inventory": "PATH does not include every local/global/npm-cache package; absence here is not proof of absence. No installation or download attempted.", "warnings": warnings}
}

type pathTool struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func pathTools(ctx context.Context, root, filter string) ([]pathTool, []string, error) {
	entries := []pathTool{}
	warnings := []string{}
	seen := map[string]bool{}
	extensions := strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";")
	filter = strings.ToLower(filter)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		dir = strings.Trim(dir, `"`)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if !strings.Contains(strings.ToLower(f.Name()), filter) {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			full := filepath.Join(dir, f.Name())
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(full)
				if err != nil {
					continue
				}
			}
			if !info.Mode().IsRegular() {
				continue
			}
			executable := info.Mode().Perm()&0111 != 0
			key := full
			if runtime.GOOS == "windows" {
				key = strings.ToLower(full)
				executable = false
				for _, ext := range extensions {
					if ext != "" && strings.EqualFold(filepath.Ext(full), ext) {
						executable = true
						break
					}
				}
			}
			if executable && !seen[key] {
				entries = append(entries, pathTool{f.Name(), full})
				seen[key] = true
			}
		}
	}
	// Stable paging; retain different paths for shadowed executable names.
	sort.SliceStable(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name) })
	return entries, warnings, nil
}

func hostInfo(ctx context.Context, group, root string) map[string]any {
	result := map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH}
	if group == "hardware" {
		result["logical_cpus"] = runtime.NumCPU()
	}
	var program string
	var args []string
	switch runtime.GOOS {
	case "windows":
		program = "powershell.exe"
		script := "Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber | ConvertTo-Json -Compress"
		if group == "hardware" {
			script = "@{cpu=@(Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors);ram_bytes=(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory;gpu=@(Get-CimInstance Win32_VideoController | Select-Object Name)} | ConvertTo-Json -Depth 4 -Compress"
			result["gpu_memory"] = "unavailable: Win32_VideoController.AdapterRAM is unreliable for modern GPUs"
		}
		args = []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $ErrorActionPreference='Stop'; " + script}
	case "darwin":
		program, args = "sw_vers", nil
		if group == "hardware" {
			program, args = "system_profiler", []string{"SPHardwareDataType", "SPDisplaysDataType", "-json"}
		}
	case "linux":
		if group == "os" {
			program, args = "uname", []string{"-sr"}
			if b, err := os.ReadFile("/etc/os-release"); err == nil {
				result["distribution"] = string(b)
			}
		} else {
			program, args = "lscpu", []string{"-J"}
			if b, err := os.ReadFile("/proc/meminfo"); err == nil {
				result["memory"] = strings.SplitN(string(b), "\n", 2)[0]
			}
			result["gpu"] = "unavailable; query a vendor utility if needed"
		}
	default:
		result["details_unavailable"] = "no host probe for this OS"
		return result
	}
	out, err := runProgram(ctx, program, args, root, 15*time.Second)
	if err != nil {
		result["details_unavailable"] = err.Error()
	}
	if strings.TrimSpace(out) != "" {
		var structured any
		if json.Unmarshal([]byte(out), &structured) == nil {
			result["details"] = structured
		} else {
			result["details"] = strings.TrimSpace(out)
		}
	}
	if group == "hardware" && ctx.Err() == nil {
		if program, err := exec.LookPath("nvidia-smi"); err == nil {
			gpu, probeErr := runProgram(ctx, program, []string{"--query-gpu=name,memory.total", "--format=csv,noheader"}, root, 5*time.Second)
			if probeErr == nil {
				result["nvidia_gpu"] = strings.TrimSpace(gpu)
				delete(result, "gpu_memory")
				delete(result, "gpu")
			} else {
				result["nvidia_unavailable"] = probeErr.Error()
			}
		}
	}
	return result
}
