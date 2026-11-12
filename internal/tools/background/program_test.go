package background

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

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
	procs := New()
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
		out, err = (Check{Procs: procs}).Run(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
	}
	match := regexp.MustCompile(`url: (http://[^\s]+)`).FindStringSubmatch(out)
	if len(match) != 2 {
		t.Fatal(out)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(match[1])
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || !strings.Contains(string(data), "managed server works") {
		t.Fatal(string(data), readErr)
	}

	if _, err = (Stop{Procs: procs}).Run(t.Context(), json.RawMessage(`{"id":"invented"}`)); err == nil || !strings.Contains(err.Error(), "do not invent") {
		t.Fatal(err)
	}
	stopArgs, _ := json.Marshal(map[string]any{"id": id})
	if _, err = (Stop{Procs: procs}).Run(t.Context(), stopArgs); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(match[1])
	if err == nil {
		response.Body.Close()
		t.Fatal("server still accepts connections after stop")
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
	procs := New()
	defer procs.StopAll()
	tool := StartProgram{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}
	out, err := tool.Run(t.Context(), input)
	if err != nil {
		t.Fatalf("%s: %v", tool.Name(), err)
	}
	id := strings.TrimPrefix(strings.Split(out, "\n")[0], "id: ")
	wait, _ := json.Marshal(map[string]any{"id": id, "timeout_seconds": 10})
	out, err = (Wait{Procs: procs}).Run(t.Context(), wait)
	if err != nil {
		t.Fatal(err)
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
}

func TestProcessToolsRejectInvalidInputs(t *testing.T) {
	cases := []struct {
		tool  agent.Tool
		input string
	}{
		{StartShell{}, `{}`},
		{StartShell{}, `{"program":"node"}`},
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
		{StartShell{}, "command"},
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
