package tools

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

var browserHostHome = os.Getenv("USERPROFILE")
var browserUnixHome = os.Getenv("HOME")

// Explicit integration check: requires Node/npm and installed Chrome.
func TestBrowserMCPIntegration(t *testing.T) {
	if os.Getenv("GEKTOR_BROWSER_SMOKE") != "1" {
		t.Skip("set GEKTOR_BROWSER_SMOKE=1 to launch local Chrome")
	}
	// TestMain isolates agent state; browser profile APIs need the real host home.
	t.Setenv("USERPROFILE", browserHostHome)
	t.Setenv("HOME", browserUnixHome)
	if os.Getenv("GEKTOR_BROWSER_COLD") == "1" {
		t.Setenv("npm_config_cache", t.TempDir())
	}
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/broken" {
			w.Write([]byte(`<script type="importmap">{"imports":{"modules":"/lib/"}}</script><script type="module">import 'modules/missing.js';</script>`))
			return
		}
		w.Write([]byte(`<button onclick="document.querySelector('output').textContent='42'">Calculate</button><output>0</output>`))
	}))
	defer server.Close()
	channel := os.Getenv("GEKTOR_BROWSER_CHANNEL")
	if channel == "" {
		channel = "chrome"
	}
	if err := PrepareBrowser(t.Context(), channel, root, func(message string) { t.Log(message) }); err != nil {
		t.Fatal(err)
	}
	list, client, err := StartBrowser(t.Context(), channel, root)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var attached int
	call := func(name string, input any) string {
		t.Helper()
		data, _ := json.Marshal(input)
		for _, tool := range list {
			if tool.Name() == "mcp__browser__"+name {
				out, err := tool.Run(VisionContext(t.Context(), true), data)
				if err != nil {
					t.Fatalf("%s: %v\n%s", name, err, out)
				}
				if at, ok := tool.(interface {
					Attach(json.RawMessage) ([]string, error)
				}); ok {
					paths, err := at.Attach(data)
					if err != nil {
						t.Fatal(err)
					}
					for _, path := range paths {
						if err := llm.ValidateImage(path); err != nil {
							t.Fatal(err)
						}
						attached++
					}
				}
				return out
			}
		}
		t.Fatalf("tool %s missing", name)
		return ""
	}
	out := call("browser_navigate", map[string]any{"url": server.URL})
	if !strings.Contains(out, server.URL) {
		t.Fatal(out)
	}
	snapshots, _ := filepath.Glob(filepath.Join(root, ".gektor", "browser", "*.yml"))
	found := strings.Contains(out, "Calculate")
	for _, path := range snapshots {
		b, _ := os.ReadFile(path)
		found = found || strings.Contains(string(b), "Calculate")
	}
	if !found {
		t.Fatal("DOM snapshot did not contain the fixture button", out)
	}

	var snapshot string
	for _, path := range snapshots {
		b, _ := os.ReadFile(path)
		snapshot += string(b)
	}
	ref := regexp.MustCompile(`button "Calculate" \[ref=([^\]]+)\]`).FindStringSubmatch(snapshot)
	if len(ref) != 2 {
		t.Fatal("missing button ref", snapshot)
	}
	out = call("browser_click", map[string]any{"element": "Calculate button", "target": ref[1]})
	snapshots, _ = filepath.Glob(filepath.Join(root, ".gektor", "browser", "*.yml"))
	found = strings.Contains(out, "42")
	for _, path := range snapshots {
		b, _ := os.ReadFile(path)
		found = found || strings.Contains(string(b), "42")
	}
	if !found {
		t.Fatal("clicked page did not show 42", out)
	}

	call("browser_console_messages", map[string]any{"level": "error"})
	// A viewport change alone is not verification: ask the live DOM for proof.
	call("browser_resize", map[string]any{"width": 375, "height": 812})
	measure := func(want bool) {
		t.Helper()
		out := call("browser_evaluate", map[string]any{"function": `() => ({overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth})`})
		pattern := `"overflow"\s*:\s*false`
		if want {
			pattern = `"overflow"\s*:\s*true`
		}
		if !regexp.MustCompile(pattern).MatchString(out) {
			t.Fatal("unexpected measured overflow", out)
		}
	}
	measure(false)
	out = call("browser_take_screenshot", map[string]any{"filename": "smoke.png", "type": "png", "scale": "css"})
	if !strings.Contains(out, "smoke.png") {
		t.Fatal(out)
	}
	// Introduce a narrow-layout regression in the actual page and ensure the
	// same measurement detects it; the check must not be a constant green light.
	call("browser_evaluate", map[string]any{"function": `() => { const block=document.createElement('div'); block.style.cssText='width:900px;height:10px'; block.textContent='overflow regression'; document.body.append(block); }`})
	measure(true)
	// Archived regression: HTTP 200 with a broken import-map prefix must be
	// visible as a browser error, not accepted as proof that the UI works.
	call("browser_navigate", map[string]any{"url": server.URL + "/broken"})
	out = call("browser_console_messages", map[string]any{"level": "error"})
	if !strings.Contains(out, "modules/missing.js") {
		t.Fatal("browser failed to expose the broken import map", out)
	}
	call("browser_close", map[string]any{})
	if attached == 0 {
		// This server may return a saved screenshot link rather than inline
		// image content. Exercise the supported read_file route in that case.
		var screenshot string
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && entry.Name() == "smoke.png" {
				screenshot = path
			}
			return nil
		})
		if screenshot == "" {
			t.Fatal("screenshot neither attached nor saved")
		}
		ws, err := workspace.New(root)
		if ws != nil {
			t.Cleanup(func() { ws.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, screenshot)
		args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(rel)})
		reader := ReadFile{WS: ws, Vision: true}
		if _, err := reader.Run(t.Context(), args); err != nil {
			t.Fatal(err)
		}
		paths, err := reader.Attach(args)
		if err != nil || len(paths) != 1 {
			t.Fatal(paths, err)
		}
		if err := llm.ValidateImage(paths[0]); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("DOM, real button click, result 42, console and screenshot succeeded")
}
