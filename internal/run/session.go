// Package run is the programmatic way to run the agent: a Session that
// emits the same events -mode json prints, and answers gates through a
// Suspender instead of stdin. The TUI and the stdio JSON-RPC server both
// build on it; neither touches the loop directly.
//
// Schema parity is structural, not promised: Session reuses the events
// package to serialize, and parses the JSONL back, so -mode json output
// and Session events cannot drift apart without a test failing.
package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
)

// Event is a decoded JSONL event. Aliased so callers wiring a Config do
// not have to import the wire package for one type; events owns it.
type Event = events.Event

// Config wires a direct-mode run. Everything the harness needs comes
// from Env (client, workspace, tools, policy, budgets); Session adds
// the task, the event sink, and the gate transport.
type Config struct {
	Env roles.Env

	ResumeAnswer string
	Plan         bool
	Task         string
	StateFile    string
	Verify       func(ctx context.Context) (output string, err error)

	// Images attaches pictures (absolute paths) to the opening user
	// message for vision-capable backends. CLI -image and TUI @paths
	// fill this; follow-up turns pass their own via Resume.
	Images []string

	// Answer transports blocked gates. Nil means ClosedSuspender:
	// headless runs fail closed instead of hanging on stdin.
	Answer agent.Suspender

	// OnEvent receives every event in order. Nil drops them.
	OnEvent func(Event)
}

// Session runs one task. Create per run; it holds no global state.
type Session struct {
	cfg         Config
	askerBudget *agent.Asker
	inbox       agent.Inbox
}

// New validates the config without running anything.
func New(cfg Config) (*Session, error) {
	if strings.TrimSpace(cfg.Task) == "" {
		return nil, fmt.Errorf("run: task is empty")
	}
	if cfg.Env.Client == nil {
		return nil, fmt.Errorf("run: Env.Client is nil")
	}
	if cfg.Env.WS == nil {
		return nil, fmt.Errorf("run: Env.WS is nil")
	}
	if cfg.Answer == nil {
		cfg.Answer = agent.ClosedSuspender("run session without operator")
	}
	return &Session{cfg: cfg, askerBudget: agent.NewAsker(0)}, nil
}

// Run executes the task to completion and returns the final answer.
// Framing (run start/end) is the caller's job: in-process callers have
// call boundaries, and wire transports add their own. Session streams
// step events only.
func (s *Session) build(ctx context.Context) (*agent.Agent, *events.Emitter, roles.Env) {
	emitter := events.New(events.EventWriter(s.cfg.OnEvent))

	askFn := s.asker(ctx)

	env := s.cfg.Env
	env.Inbox = &s.inbox
	previousInput := env.OnInput
	env.OnInput = func(input llm.Message) {
		emitter.Emit("user_input", map[string]any{"text": input.Content, "images": input.Images})
		if previousInput != nil {
			previousInput(input)
		}
	}
	// A local server may load different weights between turns. Probe before
	// building the agent so the display and enforced budget use the same facts.
	if server, ok := env.Client.(*llm.Server); ok && env.BackendKind != "openai" {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if limit, err := server.MaxContextLength(probeCtx); err == nil && limit > 0 {
			env.ContextLimit = limit
		}
		if name, err := server.ModelName(probeCtx); err == nil && strings.TrimSpace(name) != "" {
			env.Model = name
		}
		if vision, err := server.SupportsVision(probeCtx); err == nil {
			env.Vision = vision
		}
		cancel()
		emitter.Emit("backend", map[string]any{"model": env.Model, "context_limit": env.ContextLimit})
	}
	prevOnStep := env.OnStep
	prevTypedResult := env.OnResult
	prevOnUsage := env.OnUsage
	prevOnProgress := env.OnProgress
	prevOnPrompt := env.OnPrompt
	env.OnPrompt = func(report llm.ContextReport) {
		emitter.Emit("prompt_budget", map[string]any{"report": report})
		if prevOnPrompt != nil {
			prevOnPrompt(report)
		}
	}
	env.OnProgress = func(p llm.Progress) {
		emitter.Emit("progress", map[string]any{"phase": p.Phase, "prompt": p.PromptTokens, "completion": p.CompletionTokens, "cached": p.CachedTokens, "processed": p.ProcessedTokens, "estimated": p.Estimated, "detail": p.Detail})
		if prevOnProgress != nil {
			prevOnProgress(p)
		}
	}
	// expectReply marks the next text-only step as harness-caused: a
	// nudge was just delivered, so the following bare answer reacts to
	// the harness, not the operator. One-shot and text-only: tool calls
	// are work, whatever prompted them.
	expectReply := false
	var a *agent.Agent
	env.OnStep = func(label string, step int, msg llm.Message) {
		hr := expectReply && len(msg.ToolCalls) == 0
		expectReply = false
		EmitStep(emitter, label, step, msg, hr, a.Report().MaxSteps)
		if prevOnStep != nil {
			prevOnStep(label, step, msg)
		}
	}
	env.OnResult = func(result agent.ToolResult) {
		events.EmitToolResult(emitter, result.CallID, result.Output, result.Failed)
		if prevTypedResult != nil {
			prevTypedResult(result)
		}
	}
	env.OnUsage = func(step int, usage llm.Usage) {
		EmitUsage(emitter, step, usage)
		if prevOnUsage != nil {
			prevOnUsage(step, usage)
		}
	}
	prevOnNudge := env.OnNudge
	env.OnNudge = func(kind, text string) {

		events.EmitNudge(emitter, kind, text)
		expectReply = true
		if prevOnNudge != nil {
			prevOnNudge(kind, text)
		}
	}
	previousOutput := env.OnToolOutput
	previousContext := env.OnContext
	env.OnLocal = func(text string) { emitter.Emit("local_warning", map[string]any{"text": text}) }
	env.OnContext = func(m llm.Message) {
		emitter.Emit("context", map[string]any{"text": m.Content, "role": string(m.Role), "request": m.Request, "kind": m.Harness})
		if previousContext != nil {
			previousContext(m)
		}
	}
	env.OnToolOutput = func(callID, text string) {
		events.EmitToolOutput(emitter, callID, text)
		if previousOutput != nil {
			previousOutput(callID, text)
		}
	}
	prevOnDelta := env.OnDelta
	env.OnDelta = func(chunk string) {
		events.EmitDelta(emitter, chunk)
		if prevOnDelta != nil {
			prevOnDelta(chunk)
		}
	}
	prevOnReasoning := env.OnReasoning
	env.OnReasoning = func(chunk string) {
		events.EmitReasoning(emitter, chunk)
		if prevOnReasoning != nil {
			prevOnReasoning(chunk)
		}
	}
	// Findings go to the wire. The caller's own reporter is replaced
	// rather than chained: the TUI, the only caller, stubs hooks and
	// reads the event stream instead. The rule set is the caller's, so a
	// session over a non-Go project still reports secrets and nothing
	// that cannot parse.
	env.Checks = checks.NewReporter(env.Rules, func(scope checks.Scope, f checks.Finding) {
		events.EmitFinding(emitter, string(scope), f.Rule, f.Path, f.Line, f.Summary)
	})
	if s.cfg.Plan {
		a = roles.Planner(env, "plan", s.cfg.StateFile, askFn)
	} else {
		a = roles.Interactive(env, "", s.cfg.StateFile, askFn, s.cfg.Verify)
	}
	return a, emitter, env
}

func (s *Session) Run(ctx context.Context) (string, error) {
	a, emitter, env := s.build(ctx)
	ans, err := a.Run(ctx, s.cfg.Task, s.cfg.Images...)
	if err != nil {
		if resAns, resErr, ok := agent.BudgetContinue(ctx, a, err, s.cfg.StateFile, s.cfg.Answer); ok {
			ans, err = resAns, resErr
		}
	}
	env.Sweep(a.MutatedPaths())
	s.report(a, emitter)
	return ans, err
}

// asker builds the ask_user implementation over the session suspender,
// sharing one question budget across Run and Resume calls. Resume runs
// used to refuse all questions; a follow-up that needs clarification
// should ask like any other turn.
func (s *Session) asker(ctx context.Context) func(string) (string, error) {
	return s.askerBudget.Ask(func(_ context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
		return s.cfg.Answer(ctx, req)
	}, nil)
}

// Resume continues from saved history with a note, mirroring Agent.Resume.
func (s *Session) Resume(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	a, emitter, env := s.build(ctx)
	var ans string
	var err error
	if callID, question, paused := agent.PausedOnQuestion(a.Tools(), history); paused {
		reply := agent.SuspendReply{Answer: s.cfg.ResumeAnswer}
		var rerr error
		if s.cfg.ResumeAnswer == "" {
			reply, rerr = s.cfg.Answer(ctx, agent.SuspendRequest{Kind: agent.SuspendAsk, Prompt: question})
		}
		if rerr != nil {
			return "", rerr
		}
		ans, err = a.ResumeWithAnswer(ctx, history, callID, reply.Answer)
	} else {
		ans, err = a.Resume(ctx, history, note, images...)
	}
	if err != nil {
		if resAns, resErr, ok := agent.BudgetContinue(ctx, a, err, s.cfg.StateFile, s.cfg.Answer); ok {
			ans, err = resAns, resErr
		}
	}
	env.Sweep(a.MutatedPaths())
	s.report(a, emitter)
	return ans, err
}

func (s *Session) report(a *agent.Agent, e *events.Emitter) {
	if s.cfg.OnEvent == nil {
		return
	}
	EmitOutcome(e, a.Report(), s.cfg.StateFile)
}

// EmitOutcome reports measured effects and check output in every frontend.
func EmitOutcome(e *events.Emitter, r agent.RunReport, stateFile string) {
	if e == nil {
		return
	}
	e.Emit("outcome", map[string]any{"outcome": r.Outcome, "files": r.MutatedPaths, "verification": r.Verification, "warnings": r.Warnings, "resume": stateFile, "open_todos": r.OpenTodos, "summary": r.Summary})
}

// EmitStep writes one step for the CLI and session driver. harnessReply
// marks a text-only answer to a harness nudge (first reply is enough); the
// field is omitted when false so old consumers see no change. maxSteps
// carries the enforced step budget; zero omits it, for callers with no run
// behind the event.
//
// It lives here rather than beside its siblings in events because it
// takes an llm.Message, and events is a leaf by an enforced invariant.
func EmitStep(emitter *events.Emitter, label string, step int, msg llm.Message, harnessReply bool, maxSteps int) {
	if emitter == nil {
		return
	}
	ev := map[string]any{"step": step, "label": label}
	if harnessReply {
		ev["harness_reply"] = true
	}
	if maxSteps > 0 {
		ev["max_steps"] = maxSteps
	}
	if msg.Content != "" {
		ev["text"] = msg.Content
	}
	// Keep final deliberation complete so folding and reopening the live
	// block never replaces streamed text with a truncated version.
	if msg.Reasoning != "" {
		ev["reasoning"] = msg.Reasoning
	}
	calls := make([]any, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		args := string(tc.Arguments)
		if tc.InputError != "" {
			args = tc.RawArguments
		}
		calls = append(calls, map[string]any{
			// The id pairs results to calls (duplicate ids across
			// steps match the most recent open card).
			"id": tc.ID, "name": tc.Name, "args": args, "input_error": tc.InputError,
		})
	}
	if len(calls) > 0 {
		ev["tool_calls"] = calls
	}
	emitter.Emit("step", ev)
}

// EmitUsage writes backend token counts. Observers that meter context
// (TUIs, JSON streams) accumulate the latest prompt count against the
// known window; the loop itself budgets off the same numbers.
func EmitUsage(emitter *events.Emitter, step int, usage llm.Usage) {
	if emitter == nil {
		return
	}
	emitter.Emit("usage", map[string]any{
		"step":       step,
		"prompt":     usage.PromptTokens,
		"completion": usage.CompletionTokens,
		"cached":     usage.CachedTokens,
		"estimated":  usage.Estimated,
	})
}

// Submit queues steering for the next complete model/tool boundary.
// ErrInputClosed means the caller should start a follow-up turn instead.
func (s *Session) Submit(text string, images ...string) error { return s.inbox.Send(text, images...) }
func (s *Session) PendingInputCount() int                     { return s.inbox.Len() }
