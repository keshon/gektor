package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

func TestLiveExtensionExecutableHelper(t *testing.T) {
	service := ""
	for i, arg := range os.Args {
		if arg == "--fixture-adapter" && i+1 < len(os.Args) {
			service = os.Args[i+1]
		}
	}
	if service == "" {
		return
	}
	var request struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	file, err := os.OpenFile(service+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintln(file, string(request.Arguments))
	_ = file.Close()
	if _, err := os.Stat(service); err != nil {
		fmt.Fprint(os.Stdout, `{"error":"fixture service unavailable"}`)
		os.Exit(0)
	}
	var in struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(request.Arguments, &in)
	value := map[string]string{"alpha": "alpha-714", "beta": "beta-928", "gamma": "gamma-563"}[in.Key]
	if value == "" || request.Tool != "lookup" {
		fmt.Fprint(os.Stdout, `{"error":"invalid lookup"}`)
		os.Exit(0)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"output": value})
	os.Exit(0)
}

// Opt-in integration: the real local model uses the production role, registry,
// process adapter, skill libraries and persistence across six user turns.
func TestLiveExtensionsAcrossTurns(t *testing.T) {
	if os.Getenv("GEKTOR_LIVE_EXTENSIONS") != "1" {
		t.Skip("set GEKTOR_LIVE_EXTENSIONS=1 for a running llama.cpp server")
	}
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	runs := 1
	if raw := os.Getenv("GEKTOR_LIVE_RUNS"); raw != "" {
		var err error
		runs, err = strconv.Atoi(raw)
		if err != nil || runs < 1 || runs > 10 {
			t.Fatal("GEKTOR_LIVE_RUNS must be 1–10")
		}
	}
	output := os.Getenv("GEKTOR_LIVE_OUTPUT")
	if output == "" {
		output = t.TempDir()
	}
	var outputErr error
	output, outputErr = filepath.Abs(output)
	if outputErr != nil {
		t.Fatal(outputErr)
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	client := llm.NewLlamaClient(backend, "local")
	probeCtx, probeCancel := context.WithTimeout(t.Context(), 10*time.Second)
	model, err := client.ModelName(probeCtx)
	probeCancel()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s backend=%s runs=%d output=%s", model, backend, runs, output)
	for repeat := 1; repeat <= runs; repeat++ {
		t.Run(fmt.Sprintf("run-%02d", repeat), func(t *testing.T) {
			dir := filepath.Join(output, fmt.Sprintf("run-%02d", repeat))
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatalf("use a fresh live output directory: %v", err)
			}
			root, library := filepath.Join(dir, "workspace"), filepath.Join(dir, "library")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			writeFixtureSkill(t, library)
			ws, err := workspace.NewWithSkillRoots(root, []string{library})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			service := filepath.Join(dir, "service.available")
			config := tools.CapabilityConfig{Version: 1, Packs: []tools.CapabilityPack{{ID: "fixture", Version: "1", Description: "Look up fixture values", Instructions: "Use lookup for fixture keys. Never guess values.", Type: "executable", Command: []string{os.Args[0], "-test.run=^TestLiveExtensionExecutableHelper$", "--", "--fixture-adapter", service}, ReadOnly: true, Concurrent: true, ResourceField: "key", Tools: []tools.ExecutableToolDef{{Name: "lookup", Description: "Retrieve a fixture value by key", InputSchema: extensionLookup{}.Schema()}}}}}
			configFile := filepath.Join(dir, "extensions.json")
			data, _ := json.MarshalIndent(config, "", "  ")
			if err := os.WriteFile(configFile, data, 0600); err != nil {
				t.Fatal(err)
			}
			config, err = tools.LoadCapabilityConfig(configFile)
			if err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(dir, "state.json")
			phases := []struct {
				key, want                  string
				unavailable, plan, compact bool
			}{
				{key: "alpha", want: "alpha-714"},
				{key: "beta", want: "beta-928"},
				{key: "gamma", want: "gamma-563", compact: true},
				{key: "alpha", unavailable: true},
				{key: "beta", want: "beta-928"},
				{key: "gamma", want: "gamma-563", plan: true},
			}
			for index, phase := range phases {
				if phase.unavailable {
					if err := os.Remove(service); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(service, []byte("available"), 0600); err != nil {
					t.Fatal(err)
				}
				if phase.compact {
					before, after, _, err := agent.CompactFile(state, 1)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("compaction: %d → %d messages", before, after)
				}
				trace, err := os.Create(filepath.Join(dir, fmt.Sprintf("phase-%02d.jsonl", index+1)))
				if err != nil {
					t.Fatal(err)
				}
				encoder := json.NewEncoder(trace)
				failures, lookups, skillReads, successfulLookups := 0, 0, 0, 0
				callNames := map[string]string{}
				sources, closeSources := tools.BuildCapabilities(config, ws.Root())
				e := Env{WS: ws, Procs: tools.NewBackgroundProcesses(), Client: client, BackendKind: "llama", Capabilities: sources, MaxSteps: 10, MaxTokens: 4096, ContextLimit: 32768, Temperature: 0.2,
					Policy: permission.Default().WithExtra([]permission.Rule{{Tool: "run_*", Pattern: "*", Effect: permission.Deny}, {Tool: "start_*", Pattern: "*", Effect: permission.Deny}, {Tool: "delegate_task", Pattern: "*", Effect: permission.Deny}}),
					OnStep: func(_ string, _ int, m llm.Message) {
						_ = encoder.Encode(map[string]any{"event": "assistant", "message": m})
						for _, call := range m.ToolCalls {
							callNames[call.ID] = call.Name
							if call.Name == "cap__fixture__lookup" {
								lookups++
							}
							if call.Name == "load_skill" {
								skillReads++
							}
						}
					},
					OnResult: func(result agent.ToolResult) {
						_ = encoder.Encode(result)
						if !result.Failed && callNames[result.CallID] == "cap__fixture__lookup" {
							successfulLookups++
						}
						if result.Failed {
							failures++
						}
					},
				}
				var a *agent.Agent
				if phase.plan {
					a = Planner(e, "live", state, nil)
				} else {
					a = Interactive(e, "live", state, nil, nil)
				}
				prompt := fmt.Sprintf("Use the fixture skill and available capability to retrieve key %s. Inspect the actual tool result. If lookup is unavailable, answer UNAVAILABLE and do not guess. Do not modify files. Keep the final answer to one line.", phase.key)
				ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
				start := time.Now()
				var answer string
				var runErr error
				if index == 0 {
					answer, runErr = a.Run(ctx, prompt)
				} else {
					saved, err := session.Load(state)
					if err != nil {
						cancel()
						closeSources()
						trace.Close()
						t.Fatal(err)
					}
					answer, runErr = a.Resume(ctx, saved.Context(), prompt)
				}
				cancel()
				closeSources()
				trace.Close()
				passed := runErr == nil && lookups > 0
				if index == 0 {
					passed = passed && skillReads > 0
				}
				if phase.unavailable {
					passed = passed && failures > 0 && strings.Contains(strings.ToUpper(answer), "UNAVAILABLE") && !strings.Contains(answer, "alpha-714")
				} else {
					passed = passed && successfulLookups > 0 && strings.Contains(answer, phase.want) && strings.Contains(answer, "RESULT:")
				}
				result := map[string]any{"phase": index + 1, "passed": passed, "answer": answer, "steps": a.Report().Steps, "failures": failures, "lookups": lookups, "successful_lookups": successfulLookups, "skill_reads": skillReads, "elapsed_seconds": time.Since(start).Seconds(), "model": model, "backend": backend, "plan": phase.plan, "after_compaction": phase.compact}
				if runErr != nil {
					result["error"] = runErr.Error()
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("phase-%02d.json", index+1)), data, 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("phase=%d pass=%v steps=%d failures=%d answer=%q err=%v", index+1, passed, a.Report().Steps, failures, answer, runErr)
				if !passed {
					t.Errorf("phase %d failed; see %s", index+1, dir)
				}
			}
		})
	}
}
