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
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
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

	// OnEvent receives every event in order. Nil drops them.
	OnEvent func(Event)
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

// Run executes the task to completion and returns the final answer.
// Framing (run start/end) is the caller's job: in-process callers have
// call boundaries, and wire transports add their own. Session streams
// step events only.
func (s *Session) build(ctx context.Context) (*agent.Agent, *events.Emitter, roles.Env, error) {
	emitter := events.NewSink(s.cfg.OnEvent)

	askFn := s.asker()

	env := s.cfg.Env
	previousChild := env.OnChild
	env.OnChild = func(callID, kind string, fields map[string]any) {
		emitter.Emit("child", map[string]any{"call_id": callID, "kind": kind, "fields": fields})
		if previousChild != nil {
			previousChild(callID, kind, fields)
		}
	}
	env.Inbox = &s.inbox
	previousFinished := env.OnToolFinished
	env.OnToolFinished = func(callID string) {
		events.EmitToolFinished(emitter, callID)
		if previousFinished != nil {
			previousFinished(callID)
		}
	}
	previousInput := env.OnInput
	env.OnInput = func(input llm.Message) {
		emitter.Emit("user_input", map[string]any{"text": input.Content, "images": input.Images})
		if previousInput != nil {
			previousInput(input)
		}
	}
	// A local server may load different weights between turns. Probe before
	// building the agent so the display and enforced budget use the same facts.
	if env.RefreshModelFacts != nil {
		env = env.Refreshed(ctx)
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
		emitter.Emit("progress", map[string]any{"phase": p.Phase, "subphase": p.Subphase, "prompt": p.PromptTokens, "completion": p.CompletionTokens, "cached": p.CachedTokens, "cache_known": p.CacheKnown, "processed": p.ProcessedTokens, "estimated": p.Estimated, "detail": p.Detail, "request": p.Request, "attempt": p.Attempt, "request_id": p.RequestID, "http_status": p.HTTPStatus, "duration_ms": p.DurationMS, "outcome": p.Outcome})
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
		events.EmitToolResultStatus(emitter, result.CallID, result.Output, result.Failed, result.Status)
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
	// Each session owns its reporter and routes findings through its event sink.
	env.Checks = checks.NewReporter(env.Rules, func(scope checks.Scope, f checks.Finding) {
		events.EmitFinding(emitter, string(scope), f.Rule, f.Path, f.Line, f.Summary)
	})
	if s.cfg.WorkflowRecipe != "" {
		var err error
		a, err = roles.WorkflowAuthor(env, s.cfg.WorkflowRecipe, s.cfg.StateFile)
		if err != nil {
			return nil, emitter, env, err
		}
	} else if s.cfg.Plan {
		emitter.Emit("plan_mode", map[string]any{"read_only": true})
		a = roles.Planner(env, "plan", s.cfg.StateFile, askFn)
	} else {
		a = roles.Interactive(env, "", s.cfg.StateFile, askFn, s.cfg.Verify)
	}
	return a, emitter, env, nil
}

func (s *Session) Run(ctx context.Context) (string, error) {
	closeDiagnostics := s.diagnostics()
	defer closeDiagnostics()
	if strings.TrimSpace(s.cfg.Task) == "" && len(s.cfg.Images) == 0 {
		return "", fmt.Errorf("run: task is empty")
	}
	a, emitter, env, err := s.build(ctx)
	if err != nil {
		return "", err
	}
	defer s.finish(a, emitter, env)
	ans, err := a.Run(ctx, s.cfg.Task, s.cfg.Images...)
	if err != nil && s.cfg.WorkflowRecipe == "" {
		if resAns, resErr, ok := agent.BudgetContinue(ctx, a, err, s.cfg.StateFile, s.cfg.Answer); ok {
			ans, err = resAns, resErr
		}
	}
	return ans, err
}

// asker builds the ask_user implementation over the session suspender,
// sharing one question budget across Run and Resume calls. Resume runs
// used to refuse all questions; a follow-up that needs clarification
// should ask like any other turn.
func (s *Session) asker() func(context.Context, string) (string, error) {
	return s.askerBudget.Ask(s.cfg.Answer, s.cfg.AskPrompt)
}

// Resume recovers an interrupted run. A pending question is answered through
// ResumeAnswer or the operator gate before generation continues.
func (s *Session) Resume(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	return s.resume(ctx, history, note, images, true)
}

// Continue starts a new user turn from history, including a fork. Outstanding
// tool calls are closed without prompting; note and images reach the new turn.
func (s *Session) Continue(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	return s.resume(ctx, history, note, images, false)
}

func (s *Session) resume(ctx context.Context, history []llm.Message, note string, images []string, answerPendingQuestion bool) (string, error) {
	closeDiagnostics := s.diagnostics()
	defer closeDiagnostics()
	if s.cfg.WorkflowRecipe != "" {
		return "", fmt.Errorf("workflow draft sessions do not support resume")
	}
	a, emitter, env, err := s.build(ctx)
	if err != nil {
		return "", err
	}
	defer s.finish(a, emitter, env)
	var ans string
	if callID, question, paused := agent.PausedOnQuestion(a.Tools(), history); answerPendingQuestion && paused {
		reply := agent.SuspendReply{Answer: s.cfg.ResumeAnswer}
		var rerr error
		if s.cfg.ResumeAnswer == "" {
			if s.cfg.AskPrompt != nil {
				question = s.cfg.AskPrompt(question)
			}
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
	return ans, err
}

func (s *Session) finish(a *agent.Agent, e *events.Emitter, env roles.Env) {
	env.Sweep(a.MutatedPaths())
	s.lastReport = a.Report()
	EmitOutcome(e, s.lastReport, s.cfg.StateFile)
}

// Report returns the measured outcome of the most recent run or continuation.
func (s *Session) Report() agent.RunReport { return s.lastReport }

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
func (s *Session) PendingInput() (llm.Message, int)           { return s.inbox.Latest() }
func (s *Session) RecallInput() (llm.Message, bool)           { return s.inbox.RecallLatest() }

// SetSteering controls tool admission without restarting the session.
func (s *Session) SetSteering(enabled bool)              { s.inbox.SetSteering(enabled) }
func (s *Session) InputPreview() (llm.Message, int, int) { return s.inbox.Preview() }

// PendingInputs snapshots the queue without transferring attachment ownership.
func (s *Session) PendingInputs() []agent.QueuedInput     { return s.inbox.Inputs() }
func (s *Session) EditInput(id uint64, text string) error { return s.inbox.EditInput(id, text) }
func (s *Session) RemoveInput(id uint64) error            { return s.inbox.RemoveInput(id) }
