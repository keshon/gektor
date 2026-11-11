package harness

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
)

// sessionObserver owns event translation and nudge-reply attribution for one run.
// Calls are synchronous. Previous observers run after the corresponding event.
// The agent is attached after construction, before any execution can publish.
type sessionObserver struct {
	emitter     *events.Emitter
	previous    roles.Env
	agent       *agent.Agent
	expectReply bool
}

// observe binds the existing low-level Agent hooks at this frontend adapter.
func (o *sessionObserver) observe(env roles.Env) roles.Env {
	env.ChildEvents = o
	env.OnToolFinished = o.toolFinished
	env.OnInput = o.input
	env.OnPrompt = o.prompt
	env.OnProgress = o.progress
	env.OnStep = o.step
	env.OnResult = o.result
	env.OnUsage = o.usage
	env.OnNudge = o.nudge
	env.OnLocal = o.local
	env.OnContext = o.context
	env.OnToolOutput = o.toolOutput
	env.OnDelta = o.delta
	env.OnReasoning = o.reasoning

	return env
}

// Handle routes child envelopes through the same ordered emitter as parent events.
func (o *sessionObserver) Handle(event events.Event) {
	o.emitter.Handle(event)
	if o.previous.ChildEvents != nil {
		o.previous.ChildEvents.Handle(event)
	}
}

func (o *sessionObserver) finding(scope checks.Scope, finding checks.Finding) {
	events.EmitFinding(o.emitter, string(scope), finding.Rule, finding.Path, finding.Line, finding.Summary)
}

func (o *sessionObserver) toolFinished(callID string) {
	events.EmitToolFinished(o.emitter, callID)
	if o.previous.OnToolFinished != nil {
		o.previous.OnToolFinished(callID)
	}
}

func (o *sessionObserver) input(input llm.Message) {
	o.emitter.Emit("user_input", map[string]any{"text": input.Content, "images": input.Images})
	if o.previous.OnInput != nil {
		o.previous.OnInput(input)
	}
}

func (o *sessionObserver) prompt(report llm.ContextReport) {
	o.emitter.Emit("prompt_budget", map[string]any{"report": report})
	if o.previous.OnPrompt != nil {
		o.previous.OnPrompt(report)
	}
}

func (o *sessionObserver) progress(p llm.Progress) {
	fields := map[string]any{
		"phase":       p.Phase,
		"subphase":    p.Subphase,
		"prompt":      p.PromptTokens,
		"completion":  p.CompletionTokens,
		"cached":      p.CachedTokens,
		"cache_known": p.CacheKnown,
		"processed":   p.ProcessedTokens,
		"estimated":   p.Estimated,
		"detail":      p.Detail,
		"request":     p.Request,
		"attempt":     p.Attempt,
		"request_id":  p.RequestID,
		"http_status": p.HTTPStatus,
		"duration_ms": p.DurationMS,
		"outcome":     p.Outcome,
	}
	o.emitter.Emit("progress", fields)
	if o.previous.OnProgress != nil {
		o.previous.OnProgress(p)
	}
}

func (o *sessionObserver) step(label string, step int, message llm.Message) {
	// A nudge attributes only the next text-only step. Tool calls remain work.
	harnessReply := o.expectReply && len(message.ToolCalls) == 0
	o.expectReply = false
	report := o.agent.Report()
	EmitStep(o.emitter, label, step, message, harnessReply, report.MaxSteps)

	if o.previous.OnStep != nil {
		o.previous.OnStep(label, step, message)
	}
}

func (o *sessionObserver) result(result agent.ToolResult) {
	events.EmitToolResultStatus(o.emitter, result.CallID, result.Output, result.Failed, result.Status)
	if o.previous.OnResult != nil {
		o.previous.OnResult(result)
	}
}

func (o *sessionObserver) usage(step int, usage llm.Usage) {
	EmitUsage(o.emitter, step, usage)
	if o.previous.OnUsage != nil {
		o.previous.OnUsage(step, usage)
	}
}

func (o *sessionObserver) nudge(kind, text string) {
	events.EmitNudge(o.emitter, kind, text)
	o.expectReply = true

	if o.previous.OnNudge != nil {
		o.previous.OnNudge(kind, text)
	}
}

func (o *sessionObserver) local(text string) {
	// Local warnings are delivered only through the session event sink.
	o.emitter.Emit("local_warning", map[string]any{"text": text})
}

func (o *sessionObserver) context(message llm.Message) {
	fields := map[string]any{
		"text":    message.Content,
		"role":    string(message.Role),
		"request": message.Request,
		"kind":    message.Harness,
	}
	o.emitter.Emit("context", fields)
	if o.previous.OnContext != nil {
		o.previous.OnContext(message)
	}
}

func (o *sessionObserver) toolOutput(callID, text string) {
	events.EmitToolOutput(o.emitter, callID, text)
	if o.previous.OnToolOutput != nil {
		o.previous.OnToolOutput(callID, text)
	}
}

func (o *sessionObserver) delta(text string) {
	events.EmitDelta(o.emitter, text)
	if o.previous.OnDelta != nil {
		o.previous.OnDelta(text)
	}
}

func (o *sessionObserver) reasoning(text string) {
	events.EmitReasoning(o.emitter, text)
	if o.previous.OnReasoning != nil {
		o.previous.OnReasoning(text)
	}
}
