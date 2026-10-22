package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

func writeLiteralExecutionArgs() {
	for i, arg := range os.Args {
		if arg == "--literal-args" {
			dir, _ := os.Getwd()
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[i+1:], "dir": dir})
			return
		}
	}
	os.Exit(2)
}

func TestProcessToolsPreserveLiteralArguments(t *testing.T) {
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
	for _, tool := range []agent.Tool{RunShell{WS: ws}, StartBackground{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}} {
		t.Run(tool.Name(), func(t *testing.T) {
			out, err := tool.Run(t.Context(), input)
			if err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			if tool.Name() == "start_background" {
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

func TestProcessToolsRejectConflictingInputs(t *testing.T) {
	for _, tool := range []agent.Tool{RunShell{}, StartBackground{}} {
		for _, input := range []string{`{}`, `{"command":"echo x","program":"node"}`, `{"command":"echo x","args":[]}`, `{"program":" "}`, `{"program":"node","unknown":true}`} {
			if out, err := tool.Run(t.Context(), json.RawMessage(input)); err == nil {
				t.Fatalf("%s accepted %s: %s", tool.Name(), input, out)
			}
		}
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
