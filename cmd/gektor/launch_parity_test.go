package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/agentstdio"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

type parityClient struct {
	requests []llm.ChatRequest
	streams  int
}

func (c *parityClient) Stream(ctx context.Context, request llm.ChatRequest, _ func(string), content func(string)) (llm.ChatResponse, error) {
	c.streams++
	response, err := c.Chat(ctx, request)
	if response.Message.Content != "" && content != nil {
		content(response.Message.Content)
	}
	return response, err
}

func (c *parityClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	if len(c.requests) == 1 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "write", Name: "write_file", Arguments: json.RawMessage(`{"path":"notes.txt","content":"key = AKIAIOSFODNN7EXAMPLE\n"}`)}}}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func TestLaunchConfigurationAndBehaviorMatchAcrossFrontends(t *testing.T) {
	for _, frontend := range []string{"cli", "rpc"} {
		for _, plan := range []bool{false, true} {
			name := frontend + "/act"
			if plan {
				name = frontend + "/plan"
			}
			t.Run(name, func(t *testing.T) {
				ws, err := workspace.New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer ws.Close()
				args := []string{"-workspace", ws.Root(), "-mode", "json", "-temperature", "0.73", "-system-prompt", "unique-launch-identity", "-post-history", "unique-launch-post", "-user-name", "Operator", "-context-limit", "65536", "-max-tokens", "256", "-max-steps", "6", "-yes"}
				if plan {
					args = append(args, "-plan")
				}
				if frontend == "rpc" {
					args = append(args, "-serve")
				}
				args = append(args, "-stream")
				if frontend != "rpc" {
					args = append(args, "write a note")
				}
				o, err := resolveLaunch(launchRequest{args: args})
				if err != nil {
					t.Fatal(err)
				}
				client := &parityClient{}
				procs := tools.NewBackgroundProcesses()
				defer procs.StopAll()
				cfg, err := launchRunConfiguration(o, roles.Env{Client: client, WS: ws, Procs: procs, ContextLimit: 65536, Model: "test-model", VisionKnown: true}, func(string, ...any) {})
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Env.Model != "test-model" || !cfg.Env.VisionKnown || len(cfg.Env.Rules) == 0 || cfg.Plan != plan {
					t.Fatal("prepared configuration lost run settings")
				}
				if cfg.Env.OnStep != nil || cfg.Env.OnDelta != nil || cfg.Env.OnToolOutput != nil || cfg.Env.Checks != nil {
					t.Fatal("prepared environment contains frontend observers")
				}
				cfg.StateFile = filepath.Join(t.TempDir(), "state.json")
				var wire bytes.Buffer
				switch frontend {
				case "cli":
					err = runAgentCLI(t.Context(), o, cfg, nil, events.New(&wire), io.Discard, func(string, ...any) {})
				case "rpc":
					server := rpcConfiguration(o, cfg)
					server.Directory = t.TempDir()
					err = agentstdio.Serve(t.Context(), server, strings.NewReader("{\"id\":1,\"method\":\"run\",\"params\":{\"task\":\"write a note\"}}\n"), &wire)
				}
				if err != nil {
					t.Fatal(err, wire.String())
				}
				if len(client.requests) == 0 {
					t.Fatal("model was never called")
				}
				if client.streams == 0 || !strings.Contains(wire.String(), `"event":"delta"`) {
					t.Fatal("streaming was disabled or escaped the event transport")
				}
				request := client.requests[0]
				if request.Temperature != 0.73 {
					t.Fatalf("temperature = %v", request.Temperature)
				}
				var prompt strings.Builder
				for _, message := range request.Messages {
					prompt.WriteString(message.Content)
				}
				for _, marker := range []string{"unique-launch-identity", "unique-launch-post"} {
					if !strings.Contains(prompt.String(), marker) {
						t.Fatalf("identity lost %s", marker)
					}
				}
				_, statErr := os.Stat(filepath.Join(ws.Root(), "notes.txt"))
				if plan {
					if !os.IsNotExist(statErr) || !strings.Contains(wire.String(), "read-only") {
						t.Fatal("plan mode allowed mutation", statErr, wire.String())
					}
				} else {
					if statErr != nil || !strings.Contains(wire.String(), `"event":"finding"`) {
						t.Fatal("write checks did not reach the frontend", statErr, wire.String())
					}
				}
				results, outcomes := 0, 0
				for _, line := range strings.Split(strings.TrimSpace(wire.String()), "\n") {
					var event map[string]any
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal("non-JSON protocol output", line, err)
					}
					if event["event"] == "tool_result" {
						results++
					}
					if event["event"] == "outcome" {
						outcomes++
					}
				}
				if results != 1 || outcomes != 1 {
					t.Fatalf("duplicate or missing events: results=%d outcomes=%d", results, outcomes)
				}
			})
		}
	}
}

func TestTUILaunchPreservesPreparedRunConfiguration(t *testing.T) {
	for _, fork := range []bool{false, true} {
		o := resolvedLaunch{}
		o.userName, o.systemPrompt, o.temperature = "Operator", "unique-tui-identity", 0.73
		o.tui, o.plan, o.answer = true, true, "saved answer"
		if fork {
			o.fork = "source.json"
		}
		client := &parityClient{}
		ws, err := workspace.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		cfg, err := launchRunConfiguration(o, roles.Env{Client: client, WS: ws, Model: "tui-model", ContextLimit: 65536, VisionKnown: true}, func(string, ...any) {})
		if err != nil {
			t.Fatal(err)
		}
		verified := false
		cfg.Task, cfg.StateFile, cfg.Images = "new instruction", "new-state.json", []string{"new.png"}
		cfg.Verify = func(context.Context) (string, error) { verified = true; return "checked", nil }
		history := []llm.Message{{Role: llm.RoleUser, Content: "old instruction"}}
		got := tuiConfiguration(launchRequest{}, o, cfg, history)
		if got.Env.Client != client || got.Env.WS != ws || got.Env.Model != cfg.Env.Model || got.Env.Identity != cfg.Env.Identity || got.Env.Temperature != cfg.Env.Temperature || got.Env.ContextLimit != cfg.Env.ContextLimit || !got.Env.VisionKnown || !got.Env.Stream || len(got.Env.Rules) != len(cfg.Env.Rules) {
			t.Fatal("TUI adapter lost prepared environment")
		}
		if got.Task != cfg.Task || got.StateFile != cfg.StateFile || got.Images[0] != cfg.Images[0] || got.Plan != cfg.Plan || got.ResumeAnswer != cfg.ResumeAnswer || got.Fork != fork || len(got.History) != 1 {
			t.Fatal("TUI adapter lost turn settings")
		}
		if _, err := got.Verify(t.Context()); err != nil || !verified {
			t.Fatal("TUI adapter lost verification callback", err)
		}
	}
}

type forkClient struct{ messages []llm.Message }

func (c *forkClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.messages = req.Messages
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func TestCLIForkOfPausedQuestionPreservesNewTurn(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	client := &forkClient{}
	image := filepath.Join(ws.Root(), "new.png")
	cfg := run.Config{
		Env:  roles.Env{Client: client, WS: ws, Vision: true},
		Task: "NEW FORK INSTRUCTION", Images: []string{image}, StateFile: filepath.Join(t.TempDir(), "state.json"),
		ResumeAnswer: "must not answer the old question",
		Answer: func(context.Context, agent.SuspendRequest) (agent.SuspendReply, error) {
			t.Fatal("fork prompted for old question")
			return agent.SuspendReply{}, nil
		},
	}
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "old task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "question", Name: "ask_user", Arguments: json.RawMessage(`{"question":"old question?"}`)}}},
	}
	if err := runAgentCLI(t.Context(), resolvedLaunch{}, cfg, history, nil, io.Discard, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	closed, continued := false, false
	for _, message := range client.messages {
		if message.Role == llm.RoleTool && message.ToolCallID == "question" {
			closed = true
			if strings.Contains(message.Content, cfg.ResumeAnswer) {
				t.Fatal("fork answered the old question")
			}
		}
		if message.Role == llm.RoleUser && message.Content == cfg.Task && len(message.Images) == 1 && message.Images[0] == image {
			continued = true
		}
	}
	if !closed || !continued {
		t.Fatalf("fork lost closed call or continuation: %+v", client.messages)
	}
}

func TestRPCRejectsUnsupportedLaunchArguments(t *testing.T) {
	for _, args := range [][]string{
		{"-serve", "-tui"}, {"-serve", "inline task"},
		{"-serve", "-resume", "state.json"}, {"-serve", "-fork", "state.json"},
		{"-serve", "-answer", "yes"}, {"-serve", "-image", "input.png"},
	} {
		if _, err := parseLaunchOptions(args); err == nil {
			t.Fatal("silently accepted unsupported RPC arguments", args)
		}
	}
}
