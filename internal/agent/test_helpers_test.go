package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// stubClient returns canned responses in sequence, one per Chat call.
type stubClient struct {
	responses   []llm.ChatResponse
	calls       int
	lastHistory []llm.Message
}

func (s *stubClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	s.lastHistory = req.Messages
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

type delegateStub struct {
	mutations int
	result    string
	err       error
}

func (d delegateStub) Name() string { return "delegate_task" }

func (d delegateStub) Description() string { return "delegate" }

func (d delegateStub) Mode() ToolMode { return Concurrent }

func (d delegateStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }

func (d delegateStub) Run(context.Context, json.RawMessage) (string, error) {
	return fmt.Sprintf("DELEGATE\nmutations: %d\npaths: changed.txt\n----\n%s", d.mutations, d.result), d.err
}

// echoToolStub is a no-op tool that always succeeds — stands in for
// list_files's "never errors, just returns the same thing" behavior.
type echoToolStub struct{ name string }

func (e echoToolStub) Name() string {
	if e.name == "" {
		return "echo"
	}
	return e.name
}

func (echoToolStub) Description() string { return "echo" }

func (echoToolStub) Mode() ToolMode { return Concurrent }

func (echoToolStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }

func (echoToolStub) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

// varyingArgsClient issues a different tool call (different args) on
// every step, then finishes — confirms distinct calls are never flagged
// as a repeat.
type varyingArgsClient struct {
	steps        int
	finalContent string
	calls        int
	lastHistory  []llm.Message
}

func (c *varyingArgsClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.lastHistory = req.Messages
	c.calls++
	if c.calls > c.steps {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: c.finalContent}}, nil
	}
	args, _ := json.Marshal(map[string]int{"n": c.calls})
	return llm.ChatResponse{Message: llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "c", Name: "echo", Arguments: args}},
	}}, nil
}

// usageClient returns escalating Usage.PromptTokens values, one per call,
// then a final no-tool-call answer once the list is exhausted.
type usageClient struct {
	usages       []int
	finalContent string
	calls        int
	lastHistory  []llm.Message
}

func (c *usageClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.lastHistory = req.Messages
	if c.calls >= len(c.usages) {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: c.finalContent}}, nil
	}
	usage := c.usages[c.calls]
	c.calls++
	args, _ := json.Marshal(map[string]int{"n": c.calls})
	return llm.ChatResponse{
		Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "echo", Arguments: args}}},
		Usage:   llm.Usage{PromptTokens: usage},
	}, nil
}

// slowToolStub sleeps for delay then succeeds — used to prove concurrent
// execution of multiple tool calls within one step.
type slowToolStub struct {
	name  string
	delay time.Duration
}

func (s slowToolStub) Name() string { return s.name }

func (s slowToolStub) Description() string { return "slow" }

func (s slowToolStub) Mode() ToolMode { return Concurrent }

func (s slowToolStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }

func (s slowToolStub) Run(_ context.Context, _ json.RawMessage) (string, error) {
	time.Sleep(s.delay)
	return "ok", nil
}

// twoCallClient issues exactly two tool calls in its first response, then
// finishes on the second call.
type twoCallClient struct {
	callA, callB llm.ToolCall
	finalContent string
	calls        int
	lastHistory  []llm.Message
	mu           sync.Mutex
}

func (c *twoCallClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastHistory = req.Messages
	c.calls++
	if c.calls == 1 {
		return llm.ChatResponse{Message: llm.Message{
			Role:      llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{c.callA, c.callB},
		}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: c.finalContent}}, nil
}

// askUserStub blocks until the test supplies an answer via the channel.
type askUserStub struct {
	questionCh chan string
	answer     string
}

func (a askUserStub) Name() string { return "ask_user" }

func (askUserStub) Description() string { return "ask" }

func (askUserStub) Mode() ToolMode { return Exclusive }

func (askUserStub) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (a askUserStub) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Question string `json:"question"`
	}
	_ = json.Unmarshal(args, &in)
	if a.questionCh != nil {
		a.questionCh <- in.Question
	}
	return a.answer, nil
}

// orderTrackingExclusiveStub is Exclusive and records the order it ran in
// relative to other instances sharing the same *[]string log — used to
// prove Exclusive calls never overlap each other.
type orderTrackingExclusiveStub struct {
	name  string
	delay time.Duration
	log   *[]string
	mu    *sync.Mutex
}

func (s orderTrackingExclusiveStub) Name() string { return s.name }

func (s orderTrackingExclusiveStub) Description() string { return "exclusive" }

func (s orderTrackingExclusiveStub) Mode() ToolMode { return Exclusive }

func (s orderTrackingExclusiveStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }

func (s orderTrackingExclusiveStub) Run(_ context.Context, _ json.RawMessage) (string, error) {
	s.mu.Lock()
	*s.log = append(*s.log, "start:"+s.name)
	s.mu.Unlock()
	time.Sleep(s.delay)
	s.mu.Lock()
	*s.log = append(*s.log, "end:"+s.name)
	s.mu.Unlock()
	return "ok", nil
}

// stubQuestioner stands in for tools.AskUser, which cannot be imported
// here: tools depends on this package.
type stubQuestioner struct{ Tool }

func (stubQuestioner) Name() string { return "stub_question" }

func (stubQuestioner) Question(args json.RawMessage) string {
	var in struct {
		Ask string `json:"ask"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	return in.Ask
}
