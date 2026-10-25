package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecutableProtocolHelper(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "--gektor-executable-helper" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	data, _ := io.ReadAll(os.Stdin)
	switch mode {
	case "echo":
		var request struct {
			Tool      string            `json:"tool"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			os.Exit(2)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"output": request})
	case "error":
		fmt.Fprint(os.Stdout, `{"output":"partial evidence","error":"cannot finish"}`)
	case "malformed":
		fmt.Fprint(os.Stdout, `{"output":"one"} {"output":"two"}`)
	case "null":
		fmt.Fprint(os.Stdout, `{"output":null}`)
	case "oversize":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 2<<20))
	case "slow":
		time.Sleep(30 * time.Second)
	case "exit":
		fmt.Fprint(os.Stderr, "adapter failure")
		os.Exit(3)
	}
	os.Exit(0)
}

func TestExecutableProtocolAndFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"echo", "null", "error", "malformed", "oversize", "slow", "exit"} {
		t.Run(mode, func(t *testing.T) {
			tool := &ExecutableTool{Dir: t.TempDir(), Pack: CapabilityPack{ID: "fixture", Command: []string{os.Args[0], "-test.run=^TestExecutableProtocolHelper$", "--", "--gektor-executable-helper", mode}, TimeoutSeconds: 5}, Def: ExecutableToolDef{Name: "read"}}
			if mode == "slow" {
				tool.Pack.TimeoutSeconds = 1
			}
			start := time.Now()
			output, err := tool.Run(t.Context(), json.RawMessage(`{"value":"space ; $(not-a-command) quoted"}`))
			if mode == "echo" {
				if err != nil || !strings.Contains(output, "$(not-a-command)") || !strings.Contains(output, `"tool":"read"`) {
					t.Fatal(output, err)
				}
			} else if mode == "null" {
				if err != nil || output != "null" {
					t.Fatal(output, err)
				}
			} else if err == nil {
				t.Fatal("failure reported as success", output)
			}
			if mode == "error" && output != "partial evidence" {
				t.Fatal("error lost evidence", output, err)
			}
			if mode == "slow" && time.Since(start) > 10*time.Second {
				t.Fatal("timeout did not bound process")
			}
		})
	}
}
