package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/permission"
)

// Uses the shipping role and actual backend with isolated application metadata.
// No SSH connection, personal configuration or credential access is needed.
func TestDiscoveryLive(t *testing.T) {
	url := os.Getenv("GEKTOR_DISCOVERY_LIVE_URL")
	if url == "" {
		t.Skip("set GEKTOR_DISCOVERY_LIVE_URL to exercise discovery with llama.cpp")
	}

	for _, tc := range []struct {
		name, task, requiredTool, answer string
	}{
		{"ssh", "Which SSH connections are registered? Give their names. Use the available SSH tools; do not read configuration files, run commands, connect to hosts or change anything.", "ssh_connections", "Koara-fixture"},
		{"tool-name", "Use discover_capabilities to find the exact tool image_view and tell me its capability ID. Also list capabilities with limit=2, then request the next page using the returned next_offset and limit=2. Do not activate packs or run commands.", "discover_capabilities", "native_images"},
		{"skills", "Find project guidance for readable Go review. Search skill metadata using those words, load the matching guide, then report its review marker. Do not run commands or change files.", "load_skill", "fixture-review-approved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			dir := filepath.Join(home, ".gektor")
			err := os.MkdirAll(dir, 0700)
			if err != nil {
				t.Fatal(err)
			}
			profiles := map[string]ssh.Profile{"Koara-fixture": {
				Host: "example.invalid", User: "tester", KnownHosts: filepath.Join(home, "known_hosts"), PasswordEnv: "DO_NOT_RESOLVE",
			}}
			data, err := json.Marshal(profiles)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(dir, "ssh.json"), data, 0600)
			if err != nil {
				t.Fatal(err)
			}

			e := testEnv(t)
			for i := range 40 {
				skillDir := filepath.Join(e.WS.Root(), "skills", fmt.Sprintf("topic-%02d", i))
				err = os.MkdirAll(skillDir, 0700)
				if err != nil {
					t.Fatal(err)
				}
				guide := "---\ndescription: Reference material for warehouse receipts and shipping labels, carrier codes and delivery timestamps.\n---\nUnrelated guide."
				err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(guide), 0600)
				if err != nil {
					t.Fatal(err)
				}
			}
			skillDir := filepath.Join(e.WS.Root(), "skills", "go-review")
			err = os.MkdirAll(skillDir, 0700)
			if err != nil {
				t.Fatal(err)
			}
			guide := "---\nname: go-review\ndescription: Review readable Go packages.\n---\nReview marker: fixture-review-approved."
			err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(guide), 0600)
			if err != nil {
				t.Fatal(err)
			}

			e.Client = chatcompletions.NewLlamaClient(url, "local")
			e.MaxSteps, e.MaxTokens, e.ContextLimit = 8, 2048, 32768
			e.WorkflowDir = filepath.Join(home, "workflows")
			e.Policy = permission.Default().WithExtra([]permission.Rule{
				{Tool: "run_*", Pattern: "*", Effect: permission.Deny},
				{Tool: "start_*", Pattern: "*", Effect: permission.Deny},
				{Tool: "ssh_exec", Pattern: "*", Effect: permission.Deny},
				{Tool: "ssh_upload", Pattern: "*", Effect: permission.Deny},
				{Tool: "ssh_download", Pattern: "*", Effect: permission.Deny},
			})
			var calls []llm.ToolCall
			failures := 0
			e.OnStep = func(_ string, _ int, msg llm.Message) { calls = append(calls, msg.ToolCalls...) }
			e.OnUsage = func(step int, usage llm.Usage) {
				t.Logf("request=%d prompt=%d cached=%d completion=%d", step+1, usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens)
			}
			e.OnResult = func(result agent.ToolResult) {
				t.Logf("result status=%s output=%s", result.Status, result.Output)
				if result.Failed {
					failures++
				}
			}
			a, err := Interactive(e, "", filepath.Join(home, "session", "state.json"), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
			defer cancel()
			answer, err := a.Run(ctx, tc.task)
			t.Logf("answer=%s", answer)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(answer, tc.answer) {
				t.Fatalf("missing %q", tc.answer)
			}

			found, continued := false, false
			for _, call := range calls {
				t.Logf("call %s %s", call.Name, call.Arguments)
				if call.Name == tc.requiredTool {
					found = true
				}
				if call.Name == "discover_capabilities" {
					var page struct {
						Offset int `json:"offset"`
					}
					err = json.Unmarshal(call.Arguments, &page)
					if err != nil {
						t.Fatal(err)
					}
					continued = continued || page.Offset == 2
				}
				if strings.HasPrefix(call.Name, "run_") || strings.HasPrefix(call.Name, "start_") || strings.HasPrefix(call.Name, "ssh_") && call.Name != "ssh_connections" {
					t.Fatalf("unexpected execution %s", call.Name)
				}
				if tc.name == "ssh" && (call.Name == "read_file" || call.Name == "list_files" || call.Name == "grep_files") {
					t.Fatalf("configuration detour %s", call.Name)
				}
			}
			if !found {
				t.Fatalf("did not use %s", tc.requiredTool)
			}
			if tc.name == "tool-name" && !continued {
				t.Fatal("did not follow discovery continuation")
			}
			if failures != 0 {
				t.Fatal("tool failures", a.Report())
			}
		})
	}
}

// Replays only model inference. No returned tool call is dispatched or executed.
func TestSSHArgumentsReplayLive(t *testing.T) {
	url := os.Getenv("GEKTOR_DISCOVERY_LIVE_URL")
	archive := os.Getenv("GEKTOR_ARGUMENTS_REPLAY")
	if url == "" || archive == "" {
		t.Skip("set GEKTOR_DISCOVERY_LIVE_URL and GEKTOR_ARGUMENTS_REPLAY")
	}
	snapshot, err := conversation.Load(archive)
	if err != nil {
		t.Fatal(err)
	}
	const end = 26 // cdfa1926: immediately after the first malformed retry.
	if len(snapshot.History) < end {
		t.Fatal("replay archive is too short")
	}
	var defs []llm.ToolDef
	for _, capability := range snapshot.Capabilities {
		if capability.ID == "native_ssh" {
			defs = append(defs, capability.Tools...)
		}
	}
	if len(defs) == 0 {
		t.Fatal("replay archive lacks the SSH catalog")
	}
	client := chatcompletions.NewLlamaClient(url, "local")
	output := os.Getenv("GEKTOR_ARGUMENTS_LIVE_OUT")
	if output != "" {
		file, openErr := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		client.Debug = file
	}
	req := llm.ChatRequest{Messages: llm.ModelMessages(snapshot.History[:end]), Tools: defs, MaxTokens: 2048}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	response, err := client.Stream(ctx, req, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("finish=%s prompt=%d cached=%d completion=%d", response.FinishReason, response.Usage.PromptTokens, response.Usage.CachedTokens, response.Usage.CompletionTokens)
	if response.FinishReason == "length" {
		t.Fatal("replay hit the output limit")
	}
	found := false
	for _, call := range response.Message.ToolCalls {
		if call.Name != "ssh_exec" {
			t.Fatal("replay did not use the direct SSH schema", call.Name)
		}
		var arguments ssh.ExecRequest
		err = json.Unmarshal(call.Arguments, &arguments)
		if err != nil {
			t.Fatal(err)
		}
		if arguments.Connection != "Koara" || strings.TrimSpace(arguments.Command) == "" {
			t.Fatal("replay did not repair required arguments")
		}
		found = true
	}
	if !found {
		t.Fatal("replay returned no SSH call")
	}
}
