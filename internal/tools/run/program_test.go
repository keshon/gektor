package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

func TestExecutionReportsExitAndLaunchFacts(t *testing.T) {
	ws, _ := testWS(t)
	t.Setenv("GEKTOR_EXEC_HELPER", "1")
	for _, tc := range []struct {
		name, program string
		args          []string
		want          string
		failed        bool
	}{
		{"quiet success", os.Args[0], []string{"--quiet-success"}, "exit_code: 0\n", false},
		{"nonzero exit", os.Args[0], []string{"--exit-result"}, "exit_code: 3\n", true},
		{"missing executable", filepath.Join(ws.Root(), "missing-executable"), nil, "process did not start", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"program": tc.program, "args": tc.args})
			out, err := (Program{WS: ws}).Run(t.Context(), args)
			if (err != nil) != tc.failed || !strings.Contains(out+fmt.Sprint(err), tc.want) {
				t.Fatalf("out=%q err=%v", out, err)
			}
			if tc.name == "nonzero exit" {
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != 3 || !strings.Contains(out, "stdout marker") || !strings.Contains(out, "stderr marker") {
					t.Fatalf("lost exit status or output: %q %v", out, err)
				}
			}
			if tc.name == "missing executable" && strings.Contains(out, "exit_code:") {
				t.Fatal("invented exit status", out)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	args, _ := json.Marshal(map[string]any{"program": os.Args[0], "args": []string{"--quiet-success"}})
	if out, err := (Program{WS: ws}).Run(ctx, args); !errors.Is(err, context.Canceled) || strings.Contains(out, "exit_code:") {
		t.Fatalf("cancellation lost: %q %v", out, err)
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
	tool := Program{WS: ws}
	out, err := tool.Run(t.Context(), input)
	if err != nil {
		t.Fatalf("%s: %v", tool.Name(), err)
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
		{Shell{}, `{}`},
		{Shell{}, `{"command":" "}`},
		{Shell{}, `{"command":"echo x","program":"node"}`},
		{Shell{}, `{"command":"echo x","args":["x"]}`},
		{Program{}, `{}`},
		{Program{}, `{"program":" "}`},
		{Program{}, `{"program":"node","command":"echo x"}`},
		{Program{}, `{"program":"node","unknown":true}`},
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
		{Shell{}, "command"},
		{Program{}, "program"},
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
