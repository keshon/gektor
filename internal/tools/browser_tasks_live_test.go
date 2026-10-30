package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

// A deterministic model driver checks what actually reaches the model through
// the agent loop. Real Chrome supplies every observation and persisted effect;
// these are integration regressions, not a local-model success-rate benchmark.
func TestBrowserTaskObservationsLive(t *testing.T) {
	if os.Getenv("GEKTOR_BROWSER_SMOKE") != "1" {
		t.Skip("set GEKTOR_BROWSER_SMOKE=1 for browser task fixtures")
	}
	t.Setenv("USERPROFILE", browserHostHome)
	t.Setenv("HOME", browserUnixHome)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var mu sync.Mutex
	stored := "Ada"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/article":
			fmt.Fprint(w, `<title>Field bulletin</title><nav>Unrelated navigation</nav><article style="display:none">Hidden template: departure 09:00.</article><article><h1>Launch update</h1><p>`+strings.Repeat("Background detail. ", 220)+`</p><p>Departure is 18:40. Boarding code is ORBIT-73.</p></article>`)
		case "/save":
			mu.Lock()
			if r.URL.Query().Get("drop") != "1" {
				stored = r.URL.Query().Get("name")
			}
			mu.Unlock()
			fmt.Fprint(w, "Saved successfully") // deliberately also returned on dropped writes
		case "/settings", "/broken-settings":
			mu.Lock()
			name := stored
			mu.Unlock()
			drop := "0"
			if r.URL.Path == "/broken-settings" {
				drop = "1"
			}
			fmt.Fprintf(w, `<title>Settings</title><main><label>Name<input aria-label="Name" id="name" value="%s"></label><button onclick="fetch('/save?drop=%s&name='+encodeURIComponent(document.getElementById('name').value)).then(r=>r.text()).then(t=>document.getElementById('status').textContent=t)">Save</button><output id="status"></output><p>Stored name: %s</p></main>`, html.EscapeString(name), drop, html.EscapeString(name))
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "account", Value: "fixture", Path: "/", MaxAge: 3600, HttpOnly: true})
			fmt.Fprint(w, "<main>Signed in as Fixture User</main>")
		case "/account":
			if c, err := r.Cookie("account"); err == nil && c.Value == "fixture" {
				fmt.Fprint(w, "<main>Signed in as Fixture User</main>")
			} else {
				fmt.Fprint(w, "<main><h1>Sign in required</h1></main>")
			}
		default:
			fmt.Fprint(w, `<title>Chart fixture</title><canvas id="chart" width="200" height="100"></canvas><script>let c=document.getElementById('chart').getContext('2d');c.fillStyle='blue';c.fillRect(0,0,100,80)</script>`)
		}
	}))
	defer srv.Close()
	root := t.TempDir()
	b := &Browser{Channel: "chrome", Dir: root, Headless: true, connect: func(ctx context.Context, note func(string)) ([]agent.Tool, *MCPClient, error) {
		if err := PrepareBrowser(ctx, "chrome", root, note); err != nil {
			return nil, nil, err
		}
		return StartBrowser(ctx, "chrome", root)
	}}
	defer b.Close()
	finish := func(text string) llm.ChatResponse {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: text}}
	}
	call := func(name string, input any) llm.ChatResponse {
		data, _ := json.Marshal(input)
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("call-%d", time.Now().UnixNano()), Name: name, Arguments: data}}}, FinishReason: "tool_calls"}
	}
	last := func(req llm.ChatRequest) llm.Message {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == llm.RoleTool {
				return req.Messages[i]
			}
		}
		return llm.Message{}
	}
	run := func(t *testing.T, reg *agent.Registry, client browserTestClient, task string) string {
		t.Helper()
		a := agent.New(agent.Config{Client: client, Tools: reg, System: "Use browser observations and verify outcomes.", MaxSteps: 15, StateFile: filepath.Join(t.TempDir(), "state.json"), ToolContext: func(ctx context.Context) context.Context { return VisionContext(ctx, true) }})
		out, err := a.Run(ctx, task)
		if err != nil {
			t.Fatal(out, err)
		}
		return out
	}
	t.Run("article is read from paged rendered text", func(t *testing.T) {
		reg, _ := browserRegistry(b)
		step := 0
		var combined strings.Builder
		client := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
			step++
			switch step {
			case 1:
				return call("enable_browser", map[string]any{})
			case 2:
				t.Logf("activated browser tools=%d; schema estimate=%d tokens; instructions=%d bytes", len(b.tools), llm.EstimatePromptTokens(llm.ChatRequest{Tools: agent.NewRegistry(b.tools...).Defs()}), len(browserInstructions))
				return call("mcp__browser__browser_navigate", map[string]any{"url": srv.URL + "/article"})
			case 3:
				return call("browser_observe", map[string]any{"mode": "text"})
			}
			var page struct {
				Text, URL, Title, Scope string
				Next                    int `json:"next_offset"`
			}
			if err := json.Unmarshal([]byte(last(req).Content), &page); err != nil {
				t.Fatal(last(req).Content, err)
			}
			if page.URL != srv.URL+"/article" || page.Title != "Field bulletin" || page.Scope != "article" {
				t.Fatal(page)
			}
			combined.WriteString(page.Text)
			if page.Next > 0 {
				return call("browser_observe", map[string]any{"mode": "text", "offset": page.Next})
			}
			if !strings.Contains(combined.String(), "18:40") || !strings.Contains(combined.String(), "ORBIT-73") || strings.Contains(combined.String(), "Unrelated navigation") || strings.Contains(combined.String(), "Hidden template") {
				t.Fatal(combined.String())
			}
			return finish("Departure 18:40; boarding code ORBIT-73.")
		})
		if out := run(t, reg, client, "Read the bulletin and report departure time and boarding code"); !strings.Contains(out, "ORBIT-73") {
			t.Fatal(out)
		}
	})
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("save persistence broken=%t", broken), func(t *testing.T) {
			reg, _ := browserRegistry(b)
			step := 0
			name, path := "Grace", "/settings"
			if broken {
				name, path = "Linus", "/broken-settings"
			}
			client := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
				step++
				switch step {
				case 1:
					return call("enable_browser", map[string]any{})
				case 2, 7:
					return call("mcp__browser__browser_navigate", map[string]any{"url": srv.URL + path})
				case 3, 5:
					return call("browser_observe", map[string]any{"mode": "controls"})
				case 4:
					ref := regexp.MustCompile(`textbox "Name" \[ref=([^\]]+)\]`).FindStringSubmatch(last(req).Content)
					if len(ref) != 2 {
						t.Fatal("model did not receive input reference", last(req).Content)
					}
					return call("mcp__browser__browser_type", map[string]any{"element": "Name", "target": ref[1], "text": name})
				case 6:
					ref := regexp.MustCompile(`button "Save" \[ref=([^\]]+)\]`).FindStringSubmatch(last(req).Content)
					if len(ref) != 2 {
						t.Fatal("model did not receive fresh button reference", last(req).Content)
					}
					return call("mcp__browser__browser_click", map[string]any{"element": "Save", "target": ref[1]})
				case 8:
					return call("browser_observe", map[string]any{"expect_text": "Stored name: " + name})
				}
				result := last(req)
				failed := result.ToolFailed != nil && *result.ToolFailed
				if failed != broken {
					t.Fatal("incorrect outcome verification", result)
				}
				if broken {
					return finish("Save did not persist; verification failed.")
				}
				return finish("Verified persisted name: Grace.")
			})
			out := run(t, reg, client, "Change the stored name and verify it survives reopening settings")
			if broken && !strings.Contains(out, "failed") || !broken && !strings.Contains(out, "Verified") {
				t.Fatal(out)
			}
			mu.Lock()
			got := stored
			mu.Unlock()
			if got != "Grace" {
				t.Fatal("unexpected actual server state", got)
			}
		})
	}
	t.Run("login handoff refreshes page before continuing", func(t *testing.T) {
		reg, _ := browserRegistry(b)
		asked := 0
		if err := reg.Add(AskUser{AskFn: func(question string) (string, error) {
			asked++
			if strings.Contains(strings.ToLower(question), "password") {
				t.Fatal("requested credentials in chat")
			}
			// Simulate the operator completing login outside the model's turn.
			args, _ := json.Marshal(map[string]string{"url": srv.URL + "/login"})
			_, err := reg.Run(ctx, "mcp__browser__browser_navigate", args)
			return "Done", err
		}}); err != nil {
			t.Fatal(err)
		}
		step := 0
		client := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
			step++
			switch step {
			case 1:
				return call("enable_browser", map[string]any{})
			case 2:
				return call("mcp__browser__browser_navigate", map[string]any{"url": srv.URL + "/account"})
			case 3:
				return call("ask_user", map[string]any{"question": "Finish the fixture login in the browser and confirm completion."})
			case 4:
				if asked != 1 || last(req).Content != "Done" {
					t.Fatal(last(req))
				}
				return call("browser_observe", map[string]any{"mode": "controls", "expect_text": "Signed in as Fixture User"})
			}
			if !strings.Contains(last(req).Content, "Signed in as Fixture User") || last(req).ToolFailed == nil || *last(req).ToolFailed {
				t.Fatal(last(req))
			}
			return finish("Observed signed-in account after handoff.")
		})
		run(t, reg, client, "Open my account, pause for login, then verify the signed-in page")
	})
	t.Run("visual observation reaches model as pixels", func(t *testing.T) {
		reg, _ := browserRegistry(b)
		step := 0
		client := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
			step++
			switch step {
			case 1:
				return call("enable_browser", map[string]any{})
			case 2:
				return call("mcp__browser__browser_navigate", map[string]any{"url": srv.URL + "/chart"})
			case 3:
				return call("browser_observe", map[string]any{"mode": "visual"})
			}
			images := 0
			for _, msg := range req.Messages {
				for _, path := range msg.Images {
					images++
					if err := llm.ValidateImage(path); err != nil {
						t.Fatal(err)
					}
				}
			}
			if images != 1 {
				t.Fatal("model did not receive screenshot pixels", images, last(req))
			}
			return finish("Screenshot received for visual inspection.")
		})
		run(t, reg, client, "Inspect the chart visually")
	})
}
