// Package roles builds the agents this project runs.
//
// Role constructors share one environment, tool policy, and context-aware gate.
// Frontends provide observers and transport; capabilities are chosen here.
//
// This lives outside internal/agent on purpose: agent defines the Tool
// interface that tools implements, so agent cannot import tools without a
// cycle. roles is the layer that knows about both.
package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

const (
	// subagentSteps bounds one delegated unit independently of the parent's
	// limit, including when the parent has an unlimited budget.
	subagentSteps = 12
)

// Env is what every role needs regardless of what it does. Build one per
// run and pass it to each constructor.
type Env struct {
	Identity     string
	IdentityName string
	UserName     string
	Client       llm.Client
	WS           *workspace.Workspace
	Procs        *tools.BackgroundProcesses
	Outputs      *tools.OutputStore
	MaxTokens    int
	Temperature  float64
	ContextLimit int
	Policy       permission.Policy
	BackendKind  string

	// Vision reports that the backend can process pictures, so read_file
	// may attach an image instead of returning binary metadata. It comes
	// from llm.Server.SupportsVision, never from a guess.
	Vision bool

	// Model names the backend model for display (status, logs). Local
	// servers ignore it on the wire (they serve one model); it still
	// answers "what am I talking to". Empty renders as a dash.
	Model  string
	Stream bool

	MaxSteps int
	Inbox    *agent.Inbox
	OnInput  func(llm.Message)

	// MCPTools are discovered MCP tools appended to the full tool set
	// (Interactive, Subagent). Planner advertises them too, so that its
	// prompt matches Interactive's, but refuses to run them: a mode switch
	// must not change the prompt prefix.
	MCPTools []agent.Tool

	// OnDelta receives streamed answer text for live display. OnReasoning
	// receives streamed deliberation, which arrives first.
	OnDelta     func(chunk string)
	OnReasoning func(chunk string)

	// Gate, if set, prompts the operator for Ask-gated calls. Nil means
	// Ask degrades to Deny.
	Gate func(tool, resource string, args json.RawMessage) (permission.Effect, error)
	// GateContext receives the active run context. Preferred over Gate.
	GateContext func(context.Context, string, string, json.RawMessage) (permission.Effect, error)

	// OnStep, if set, receives every step of every agent built from this
	// Env, tagged with that agent's label ("explore", "review", a subtask
	// id). Callers used to wrap this by hand at four separate sites and
	// each wrapped it slightly differently.
	OnStep func(label string, step int, msg llm.Message)

	OnResult     func(agent.ToolResult)
	OnToolOutput func(callID, text string)

	// Rules are the deterministic checks this run applies to written
	// files. Language rules live in internal/rules; checks.Secrets covers
	// every language. Which set to use is the frontend's call.
	Rules []checks.Rule

	// Checks collects the run's findings: post-write scans from wrapped
	// file tools, plus the deep session-end sweep. Nil disables both, and
	// tools then run unwrapped, identical to uninstrumented.
	Checks *checks.Reporter

	// OnUsage, if set, receives backend token counts per step.
	// See agent.Config.OnUsage.
	OnUsage    func(step int, usage llm.Usage)
	OnProgress func(llm.Progress)

	// OnNudge receives loop-generated harness text (verify rounds,
	// refusals, leak notices, wrap-ups, stuck escalations) tagged by
	// kind. Nil disables reporting; the [harness] prefix applies
	// regardless, so provenance never depends on observers.
	OnNudge   func(kind, text string)
	OnContext func(llm.Message)
	OnLocal   func(string)

	// StateDir, when set, is where transcripts go; a role's stateFile
	// argument is resolved against it. Empty means the caller passes
	// whole paths, or none at all.
	StateDir string
}

func (e Env) onStep(label string) func(int, llm.Message) {
	if e.OnStep == nil {
		return nil
	}
	return func(step int, msg llm.Message) { e.OnStep(label, step, msg) }
}

// Sweep runs the deep session-end pass over the paths a run changed,
// catching writes that no tool reported as a post-write edit. Call it
// once per run, after the last turn, with every path the run mutated.
func (e Env) Sweep(paths []string) {
	if e.Checks != nil {
		e.Checks.Sweep(e.WS, paths)
	}
}

func (e Env) ResolveGate() permission.Gate {
	if e.GateContext != nil {
		return e.GateContext
	}
	if e.Gate == nil {
		return nil
	}
	return func(_ context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		return e.Gate(tool, resource, args)
	}
}

func (e Env) policy() permission.Policy {
	if len(e.Policy.Rules) == 0 {
		return permission.Default()
	}
	return e.Policy
}

// Retrospective gets only archive access and structured submission. It deliberately
// omits task identity, workspace instructions, inbox and persistence:
// historical instructions must not become the reviewer's operating instructions.
func Retrospective(e Env, archive, submit agent.Tool) *agent.Agent {
	maxTokens := e.MaxTokens
	if maxTokens <= 0 || maxTokens > 4096 {
		maxTokens = 4096
	}
	return agent.New(agent.Config{
		Client: e.Client, System: prompts.Evaluation, MaxSteps: 12,
		MaxTokens: maxTokens, ContextLimit: e.ContextLimit,
		Tools:  agent.NewRegistry(archive, submit),
		OnStep: e.onStep("evaluation"), OnProgress: e.OnProgress, OnResult: e.OnResult,
	})
}

// Planner is the read-only conversation. It deliberately shares
// Interactive's system prompt and tool set: a narrower registry would change
// the prompt prefix, and a backend that caches prompts would re-read the
// whole conversation every time the operator switches modes. Capability is
// removed where it is used - the loop refuses every call outside
// tools.ReadOnly - rather than by asking the model to hold back, so nothing
// can write in this role whatever the model decides to try.
func Planner(e Env, label, stateFile string, ask func(question string) (string, error)) *agent.Agent {
	cfg := e.conversationConfig(label, stateFile, ask, nil)
	cfg.ReadOnly = tools.ReadOnly(e.WS, e.Procs, e.Vision).Names()
	cfg.Directive = prompts.PlanMode
	cfg.Supersedes = prompts.ActMode
	return agent.New(cfg)
}

// Shared run invariants; each role chooses only its capabilities and budget.
func (e Env) agentConfig(system, label, stateFile string) agent.Config {
	store := e.Outputs
	if store == nil {
		store = tools.NewOutputStore(e.statePath(stateFile))
	}
	return agent.Config{
		BeginChanges:  e.WS.ChangeObserver,
		Client:        e.Client,
		Inbox:         e.Inbox,
		OnInput:       e.OnInput,
		System:        system + "\n\n" + prompts.Environment(e.WS.Root(), e.Vision) + tools.BrowserContext(e.MCPTools) + e.WS.Instructions() + "\n\n" + e.Identity,
		MaxTokens:     e.MaxTokens,
		Temperature:   e.Temperature,
		ContextLimit:  e.ContextLimit,
		StateFile:     e.statePath(stateFile),
		OnStep:        e.onStep(label),
		OnResult:      e.OnResult,
		OnToolOutput:  e.OnToolOutput,
		OnUsage:       e.OnUsage,
		OnProgress:    e.OnProgress,
		OnNudge:       e.OnNudge,
		OnContext:     e.OnContext,
		ToolContext:   func(ctx context.Context) context.Context { return tools.VisionContext(store.Context(ctx), e.Vision) },
		LimitOutput:   tools.LimitOutput,
		Notifications: e.Procs.Notifications,
		Summarize:     true,
		OnLocal:       e.OnLocal,
		Instructions: func(args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
				From string `json:"from"`
				To   string `json:"to"`
			}
			if json.Unmarshal(args, &in) != nil || e.WS == nil {
				return "", nil
			}
			var parts []string
			for _, path := range []string{in.Path, in.From, in.To} {
				if path == "" {
					continue
				}
				text, err := e.WS.ScopedInstructions(path)
				if err != nil {
					return "", err
				}
				if text != "" && !slices.Contains(parts, text) {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, "\n"), nil
		},
		Policy: e.policy(),
		Resources: func(name string, args json.RawMessage) ([]string, error) {
			return tools.PathResources(e.WS, name, args)
		},
		BeforeToolCall: e.ResolveGate(),
	}
}

func (e Env) statePath(name string) string {
	switch {
	case name == "":
		return ""
	case e.StateDir == "":
		return name
	default:
		return filepath.Join(e.StateDir, name)
	}
}

// Subagent is what delegate_task spawns. It gets the full tool set minus
// ask_user and delegate_task: no subagent can spawn subagents, and none
// can block the process waiting on a human who is watching the parent.
// The inherited System text names both withheld tools, so the scope
// correction is composed here, where the restricted registry is chosen.
// Observer hooks (steps, results, usage, nudges) are deliberately muted
// here, not inherited: the parent session wraps them into parent-evidence
// events, and a subagent's blow-by-blow would land in the parent
// transcript as orphan blocks and overwrite the parent's usage meter —
// unactionable noise the parent model can't even map, since it never sees
// the subagent's history. The parent learns everything it can use from
// the DELEGATE result envelope. Enforcement (gate) and file-grounded
// findings still flow: those constrain or describe the work itself.
func Subagent(e Env, role string) *agent.Agent {
	cfg := e.agentConfig(prompts.WithRole(role)+"\n\n"+prompts.SubagentScope+"\n\n"+prompts.PlatformLine(), "", "")
	cfg.Tools = tools.Base(e.WS, e.Procs, e.Checks.PerEdit, e.Rules, e.Vision, e.MCPTools...)
	cfg.MaxSteps = subagentSteps
	cfg.System += fmt.Sprintf("\n\nYour budget is %d model steps, including your final answer. Keep the subtask focused and report unfinished work honestly.", subagentSteps)
	cfg.Inbox, cfg.OnInput = nil, nil
	cfg.OnStep, cfg.OnResult, cfg.OnUsage, cfg.OnNudge = nil, nil, nil, nil
	cfg.OnToolOutput = nil
	cfg.OnProgress = nil
	cfg.OnContext, cfg.OnLocal, cfg.Notifications = nil, nil, nil
	return agent.New(cfg)
}

// Interactive is the top-level agent a person talks to: the full tool set
// plus ask_user and delegate_task.
//
// ask performs the blocking question. Passing nil removes ask_user from
// the tool set entirely, which changes what the agent can do — an
// unattended caller should usually pass a responder that says plainly no
// human is available rather than dropping the tool, so that the agent
// under test has the same tools as the one that ships and reaching for
// ask_user stays visible in the trace.
//
// verify, if set, runs the final build/test command without another model turn.
func Interactive(e Env, label, stateFile string,
	ask func(question string) (string, error),
	verify func(ctx context.Context) (string, error)) *agent.Agent {
	cfg := e.conversationConfig(label, stateFile, ask, verify)
	cfg.Directive = prompts.ActMode
	cfg.Supersedes = prompts.PlanMode
	return agent.New(cfg)
}

// conversationConfig is everything Interactive and Planner share, which is
// everything except the stance. One builder keeps their prompt prefix
// identical by construction: the tests assert it, because a divergence there
// is invisible until the operator pays for a full re-read.
func (e Env) conversationConfig(label, stateFile string,
	ask func(question string) (string, error),
	verify func(ctx context.Context) (string, error)) agent.Config {
	if e.Outputs == nil {
		e.Outputs = tools.NewOutputStore(e.statePath(stateFile))
	}
	extra := []agent.Tool{&tools.Delegate{Spawn: func(role string) *agent.Agent { return Subagent(e, role) }}}
	extra = append(extra, e.MCPTools...)
	extra = append(extra, tools.ReadHistory{Path: e.statePath(stateFile)})
	if ask != nil {
		extra = append(extra, tools.AskUser{AskFn: ask})
	}
	cfg := e.agentConfig(prompts.System+"\n\n"+prompts.PlatformLine(), label, stateFile)
	cfg.Tools = tools.Base(e.WS, e.Procs, e.Checks.PerEdit, e.Rules, e.Vision, extra...)
	cfg.MaxSteps = e.MaxSteps
	cfg.Verify = verify
	cfg.Stream, cfg.OnDelta, cfg.OnReasoning = e.Stream, e.OnDelta, e.OnReasoning
	return cfg
}
