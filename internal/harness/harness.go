// Package harness is the programmatic way to run the agent: a Session that
// emits the same events -mode json prints, and answers gates through a
// Suspender instead of stdin. CLI, TUI and stdio RPC all build on it.
//
// Events retain their Go values in process; wire adapters serialize the same
// event fields at the JSONL boundary.
package harness

import (
	"context"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/roles"
)

// Event is a decoded JSONL event. Aliased so callers wiring a Config do
// not have to import the wire package for one type; events owns it.
type Event = events.Event

// Config wires one session. Everything the harness needs comes
// from Env (client, workspace, tools, policy, budgets); Session adds
// the task, the event sink, and the gate transport.
type Config struct {
	Env roles.Env

	ResumeAnswer   string
	WorkflowRecipe string
	AskPrompt      func(string) string
	Plan           bool
	Task           string
	StateFile      string
	Verify         func(ctx context.Context) (output string, err error)

	// Images attaches pictures (absolute paths) to the opening user
	// message for vision-capable backends. CLI -image and TUI @paths
	// fill this; follow-up turns pass their own via Resume.
	Images []string

	// Answer transports blocked gates. Nil means ClosedSuspender:
	// headless runs fail closed instead of hanging on stdin.
	Answer agent.Suspender

	// Events receives every event synchronously in order. Nil drops them.
	Events events.Sink
}

// Session runs one task. Create per run; it holds no global state.
type Session struct {
	cfg         Config
	askerBudget *agent.Asker
	inbox       agent.Inbox
	lastReport  agent.RunReport
}

// New validates the config without running anything.
func New(cfg Config) (*Session, error) {
	if cfg.Env.Client == nil {
		return nil, fmt.Errorf("run: Env.Client is nil")
	}

	if cfg.Env.WS == nil {
		return nil, fmt.Errorf("run: Env.WS is nil")
	}

	if cfg.WorkflowRecipe != "" && cfg.Plan {
		return nil, fmt.Errorf("workflow authoring cannot use plan mode")
	}

	if cfg.Answer == nil {
		cfg.Answer = agent.ClosedSuspender("run session without operator")
	}

	return &Session{cfg: cfg, askerBudget: agent.NewAsker(0)}, nil
}
