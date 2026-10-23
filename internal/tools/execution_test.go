package tools

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

func writeLiteralExecutionArgs() {
	for i, arg := range os.Args {
		if arg == "--spawn-http-child" {
			child := exec.Command(os.Args[0], "--http-fixture")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Run(); err != nil {
				os.Exit(4)
			}
			return
		}
		if arg == "--http-fixture" {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				os.Exit(3)
			}
			fmt.Println("url: http://" + listener.Addr().String())
			_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "managed server works") }))
			return
		}
		if arg == "--literal-args" {
			dir, _ := os.Getwd()
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[i+1:], "dir": dir})
			return
		}
	}
	os.Exit(2)
}

func TestManagedNativeHTTPServerLifecycle(t *testing.T) {
	testManagedNativeHTTPServerLifecycle(t, []string{"--http-fixture"})
}

func testManagedNativeHTTPServerLifecycle(t *testing.T, arguments []string) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	procs := NewBackgroundProcesses()
	defer procs.StopAll()
	t.Setenv("GEKTOR_EXEC_HELPER", "1")
	args, _ := json.Marshal(map[string]any{"program": os.Args[0], "args": arguments})
	out, err := (StartProgram{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}).Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimPrefix(strings.Split(out, "\n")[0], "id: ")
	// A slow CI host may not have printed the URL when the settle timer fires.
	for i := 0; i < 50 && !strings.Contains(out, "url: http://"); i++ {
		time.Sleep(50 * time.Millisecond)
		query, _ := json.Marshal(map[string]any{"id": id, "cursor": 0})
		out, err = (CheckBackground{Procs: procs}).Run(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
	}
	match := regexp.MustCompile(`url: (http://[^\s]+)`).FindStringSubmatch(out)
	if len(match) != 2 {
		t.Fatal(out)
	}
	urlArgs, _ := json.Marshal(map[string]any{"url": match[1]})
	out, err = (CheckURL{}).Run(t.Context(), urlArgs)
	if err != nil || !strings.Contains(out, "managed server works") {
		t.Fatal(out, err)
	}
	if _, err = (StopBackground{Procs: procs}).Run(t.Context(), json.RawMessage(`{"id":"invented"}`)); err == nil || !strings.Contains(err.Error(), "do not invent") {
		t.Fatal(err)
	}
	stopArgs, _ := json.Marshal(map[string]any{"id": id})
	if _, err = (StopBackground{Procs: procs}).Run(t.Context(), stopArgs); err != nil {
		t.Fatal(err)
	}
	if out, err = (CheckURL{}).Run(t.Context(), urlArgs); err != nil || !strings.Contains(out, "\nerror:") {
		t.Fatal("server did not report a connection error after stop", out, err)
	}
}

func TestProgramToolsPreserveLiteralArguments(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.Mkdir(filepath.Join(root, "working dir"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(root, "helper with spaces.exe")
	if err := os.WriteFile(program, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEKTOR_EXEC_HELPER", "1")
	values := []string{"", "spaces here", `a"b`, `C:\path with spaces\`, "& echo injected", "; false", "$HOME", "%PATH%", "текст"}
	args := append([]string{"--literal-args"}, values...)
	input, _ := json.Marshal(map[string]any{"program": program, "args": args, "workdir": "working dir"})
	procs := NewBackgroundProcesses()
	defer procs.StopAll()
	for _, tool := range []agent.Tool{RunProgram{WS: ws}, StartProgram{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}} {
		t.Run(tool.Name(), func(t *testing.T) {
			out, err := tool.Run(t.Context(), input)
			if err != nil {
				t.Fatalf("%s: %v", tool.Name(), err)
			}
			if tool.Name() == "start_program" {
				id := strings.TrimPrefix(strings.Split(out, "\n")[0], "id: ")
				wait, _ := json.Marshal(map[string]any{"id": id, "timeout_seconds": 10})
				out, err = (WaitBackground{Procs: procs}).Run(t.Context(), wait)
				if err != nil {
					t.Fatal(err)
				}
			}
			start := strings.Index(out, `{"args":`)
			if start < 0 {
				t.Fatal(out)
			}
			var got struct {
				Args []string
				Dir  string
			}
			if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&got); err != nil {
				t.Fatal(err, out)
			}
			want, _ := json.Marshal(values)
			actual, _ := json.Marshal(got.Args)
			if string(actual) != string(want) || !strings.EqualFold(got.Dir, filepath.Join(root, "working dir")) {
				t.Fatalf("arguments or workdir changed: %s", out)
			}
		})
	}
}

func TestProcessToolsRejectInvalidInputs(t *testing.T) {
	cases := []struct {
		tool  agent.Tool
		input string
	}{
		{RunShell{}, `{}`},
		{RunShell{}, `{"command":" "}`},
		{RunShell{}, `{"command":"echo x","program":"node"}`},
		{RunShell{}, `{"command":"echo x","args":["x"]}`},
		{StartBackground{}, `{}`},
		{StartBackground{}, `{"program":"node"}`},
		{RunProgram{}, `{}`},
		{RunProgram{}, `{"program":" "}`},
		{RunProgram{}, `{"program":"node","command":"echo x"}`},
		{RunProgram{}, `{"program":"node","unknown":true}`},
		{StartProgram{}, `{}`},
		{StartProgram{}, `{"program":"node","command":"echo x"}`},
	}
	for _, tc := range cases {
		if out, err := tc.tool.Run(t.Context(), json.RawMessage(tc.input)); err == nil {
			t.Fatalf("%s accepted %s: %s", tc.tool.Name(), tc.input, out)
		}
	}
}

func TestProcessSchemasAreGrammarSafe(t *testing.T) {
	cases := []struct {
		tool     agent.Tool
		required string
	}{
		{RunShell{}, "command"},
		{StartBackground{}, "command"},
		{RunProgram{}, "program"},
		{StartProgram{}, "program"},
	}
	for _, tc := range cases {
		t.Run(tc.tool.Name(), func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal(tc.tool.Schema(), &schema); err != nil {
				t.Fatal(err)
			}
			for _, unsupported := range []string{"oneOf", "anyOf", "allOf"} {
				if _, ok := schema[unsupported]; ok {
					t.Fatalf("%s schema uses %s; llama.cpp collapses such tool arguments to {}", tc.tool.Name(), unsupported)
				}
			}
			required, _ := schema["required"].([]any)
			if len(required) != 1 || required[0] != tc.required {
				t.Fatalf("%s required = %v, want [%s]", tc.tool.Name(), required, tc.required)
			}
			properties, _ := schema["properties"].(map[string]any)
			if _, ok := properties[tc.required]; !ok {
				t.Fatalf("%s schema has no %s property", tc.tool.Name(), tc.required)
			}
			if tc.required == "program" {
				if _, ok := properties["args"]; !ok {
					t.Fatalf("%s schema has no args property", tc.tool.Name())
				}
			}
		})
	}
}

func TestBrowserAvailabilityMatchesRegisteredTools(t *testing.T) {
	if !strings.Contains(BrowserContext(nil), "unavailable") {
		t.Fatal(BrowserContext(nil))
	}
	list := []agent.Tool{&MCPTool{server: "browser", def: MCPToolDef{Name: "browser_navigate"}}}
	if len(BrowserToolNames(list)) != 1 || !strings.Contains(BrowserContext(list), "connected over MCP") {
		t.Fatal(BrowserContext(list))
	}
}
