// Package agent contains the model-agnostic, backend-agnostic agent loop:
// talk to an llm.Client, run whatever tools it asks for, and decide when to
// stop or change strategy. It has no idea koboldcpp or filesystem tools
// exist — those live in internal/llm and internal/tools.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/permission"
	"github.com/keshon/tars/internal/prompts"
)

// ToolResult carries execution status separately from model-facing prose.
type ToolResult struct {
	CallID string
	Output string
	Failed bool
}

// ToolMode tells the loop whether a tool may run concurrently with others
// in the same step, or must run alone. A type-level signal cannot know
// whether two write_file calls touch the same path, so rather than inspect
// arguments, every Exclusive tool just runs alone — occasionally
// serializing two writes to different files is cheaper than a write racing
// a read of the same path.
type ToolMode int

const (
	// Concurrent tools are safe to run alongside anything else in the
	// same batch: read-only operations.
	Concurrent ToolMode = iota
	// Exclusive tools mutate the workspace, or do something unanalyzable
	// (an arbitrary shell command). They separate concurrent read batches
	// and execute in model call order.
	Exclusive
)

func (m ToolMode) String() string {
	switch m {
	case Concurrent:
		return "Concurrent"
	case Exclusive:
		return "Exclusive"
	default:
		return "unknown"
	}
}

// Tool is anything the agent can call by name with JSON arguments.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON schema for the arguments object
	Mode() ToolMode
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry is a fixed set of tools, looked up by name.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

// Names returns this registry's tool names, sorted. What an agent may do
// is worth asserting on rather than assuming — see internal/roles, where
// a hand-assembled agent silently ended up with a smaller tool set than
// the one that ships.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Defs returns the tool definitions to hand to the LLM client.
func (r *Registry) Defs() []llm.ToolDef {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]llm.ToolDef, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		})
	}
	return defs
}

func (r *Registry) Run(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Run(ctx, args)
}

// ModeOf reports a tool's concurrency mode. An unknown name (shouldn't
// happen — the model can only call tools it was offered) is treated as
// Exclusive: the safe default when in doubt.
func (r *Registry) ModeOf(name string) ToolMode {
	if t, ok := r.tools[name]; ok {
		return t.Mode()
	}
	return Exclusive
}

// Has reports whether a tool is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// HasAny reports whether at least one of the named tools is registered.
func (r *Registry) HasAny(names ...string) bool {
	for _, name := range names {
		if r.Has(name) {
			return true
		}
	}
	return false
}

// Checklister is a tool carrying the model's checklist, which the loop
// reads and can close on the model's behalf. *tools.Todo is one. The
// interface exists so the loop finds the checklist by shape rather than
// by the name "todo".
type Checklister interface {
	Tool
	Progress() (done int, open []string)
}

// Checklist returns the registry's checklist tool, or nil.
func (r *Registry) Checklist() Checklister {
	if r == nil {
		return nil
	}
	for _, t := range r.tools {
		if cl, ok := t.(Checklister); ok {
			return cl
		}
	}
	return nil
}

// Questioner is a tool that puts a question to a human. The loop finds an
// unanswered one in history on resume, so it needs the question text back
// out of the call arguments.
type Questioner interface {
	Tool
	Question(args json.RawMessage) string
}

// Questioner returns the registry's human-question tool, or nil.
func (r *Registry) Questioner() Questioner {
	if r == nil {
		return nil
	}
	for _, t := range r.tools {
		if q, ok := t.(Questioner); ok {
			return q
		}
	}
	return nil
}

// IdempotentOf reports whether a tool declared (via an optional
// `Idempotent() bool`) that identical calls return identical results while
// nothing mutates — true for pure reads, never for time-varying results
// (check_url) or side effects (delegate_task). The loop short-circuits
// exact repeats so a weak model re-reading the same file is told so rather
// than served the same bytes again. Default false: a tool must opt in.
func (r *Registry) IdempotentOf(name string) bool {
	t, ok := r.tools[name]
	if !ok {
		return false
	}
	if it, ok := t.(interface{ Idempotent() bool }); ok {
		return it.Idempotent()
	}
	return false
}

// TodoProgress reports the checklist tool's state: done count and
// still-open item texts. Zeros when the registry has none, which the
// finish gate reads as "nothing pending". Nil-receiver safe: some loop
// paths run tool-less.
func (r *Registry) TodoProgress() (done int, open []string) {
	if cl := r.Checklist(); cl != nil {
		return cl.Progress()
	}
	return 0, nil
}

// maxIdenticalAttempts is how many identical attempts at a call are
// allowed while it keeps failing and nothing mutates in between: the
// call executes twice and the third attempt is refused without running.
// Above two so a genuinely time-dependent retry has room — polling a
// server that is still starting is the honest case, and start_background
// is Concurrent so it does not clear this cache.
const maxIdenticalAttempts = 3

// rehydrateTodo restores the checklist from history into the fresh
// registry. The checklist tool takes the full list per call, so
// replaying the LAST such call's args restores exact state through the
// normal Run path. Best-effort: stale args fail validation and the
// tracker stays empty, exactly as before.
func (a *Agent) rehydrateTodo(ctx context.Context, history []llm.Message) {
	cl := a.cfg.Tools.Checklist()
	if cl == nil {
		return
	}
	for i := len(history) - 1; i >= 0; i-- {
		calls := history[i].ToolCalls
		for j := len(calls) - 1; j >= 0; j-- {
			if calls[j].Name != cl.Name() {
				continue
			}
			_, _ = a.cfg.Tools.Run(ctx, cl.Name(), calls[j].Arguments)
			return
		}
	}
}

// executeToolCalls runs one step's tool calls and accounts for them:
// repeat detection, refusal short-circuits, policy gates, concurrent vs
// exclusive scheduling, result collection, mutation bookkeeping, loop
// counters, and step funding. It returns whether the calls repeated the
// previous step and the grown history — it never finishes runs; the
// loop decides that from the returned repeat flag.
func (a *Agent) executeToolCalls(ctx context.Context, st *runState, step int, finishReason string, calls []llm.ToolCall, history []llm.Message) (bool, []llm.Message) {
	signature := callSignature(calls)
	repeat := signature == st.lastSignature
	st.lastSignature = signature
	// Acting resets the todo-bounce latch: only back-to-back
	// narration without intervening work counts toward it.
	st.todoBounces = 0

	// Tool calls within one step are scheduled by Tool.Mode(), not run
	// uniformly. Concurrent calls (reads, read-only checks) run together.
	// Exclusive ones — anything that mutates the workspace, run_shell's
	// unanalyzable command, delegate_task whose subagent writes to the same
	// workspace — run one at a time and never overlap the concurrent batch.
	// A model issuing write_file then patch_file on one file can rely on
	// that order, and two reads cannot race a write either way.
	results := make([]callResult, len(calls))

	// Exact repeats of idempotent (pure-read) calls don't re-execute:
	// nothing has changed, so the result would be byte-identical — and
	// re-delivering it teaches a weak model nothing while filling the
	// context with duplicates. The repeat comes back as an *error*
	// result on purpose: errors don't count as progress, so the stuck
	// detector keeps escalating if the model won't change course.
	//
	// write_file to a path already written this run is refused the same
	// way: soft nudges don't break rewrite loops when every call
	// returns success (progressed=true clears stuckSteps).
	skipped := make([]bool, len(calls))
	for i, call := range calls {
		// Truncated arguments (generation cut off mid-call) are stumps,
		// not calls: executing them fails arg parsing here and 500s
		// backends that parse server-side. Refuse with reissue-smaller
		// guidance instead. Malformed args with any other finish reason
		// keep the accurate "bad arguments" error below.
		if finishReason == "length" && len(strings.TrimSpace(string(call.Arguments))) > 0 && !json.Valid(call.Arguments) {
			skipped[i] = true
			results[i] = callResult{err: fmt.Errorf(
				"call arguments are truncated JSON (generation hit its output limit mid-call) — " +
					"reissue smaller: split the call into smaller pieces")}
			continue
		}
		// Whole-file writes only: the guard used to fire on patches too, telling
		// the model to patch with the patch tool it had just been blocked
		// from. It spent nine steps obeying that.
		if a.rewritesFile(call.Name) {
			if path := writePathFromArgs(call.Arguments); path != "" {
				if prev, seen := st.wrotePaths[path]; seen {
					skipped[i] = true
					results[i] = callResult{err: fmt.Errorf(
						"already wrote %s in step %d — it is on disk. To change it, use "+
							"patch_file or patch_lines. To add something else, write the "+
							"NEXT missing file from your files_hint / acceptance list, "+
							"then finish when done",
						path, prev)}
					continue
				}
			}
		}
		// A call that already failed, repeated identically with nothing
		// mutated since, fails identically again. Soft nudges do not stop
		// this: a live worker ran `go test <one _test.go file>` seven times
		// while the stuck counter climbed and escalated, ignoring every
		// nudge. A refusal is not ignorable.
		//
		// The tolerance exists because a few tools are legitimately
		// time-dependent — check_url against a server still starting.
		key := callKey(call.Name, call.Arguments)
		if n := st.failedCalls[key]; n >= maxIdenticalAttempts-1 {
			skipped[i] = true
			results[i] = callResult{err: fmt.Errorf(
				"this exact %s call has already failed %d times and nothing in the workspace "+
					"has changed since — it will fail the same way again. Read the error above "+
					"and fix the cause, or take a different approach; do not re-run it",
				call.Name, n)}
			continue
		}

		if !a.cfg.Tools.IdempotentOf(call.Name) {
			continue
		}
		sig := key
		if prev, seen := st.idempotentSeen[sig]; seen {
			skipped[i] = true
			results[i] = callResult{err: fmt.Errorf(
				"this exact %s call (same arguments) already ran in step %d and nothing has "+
					"changed since — its result is still valid, re-read it from the conversation. "+
					"Do not repeat the call; do something different (different path, different "+
					"arguments, or move on to acting on what you already know)",
				call.Name, prev)}
		}
	}

	runOne := func(i int) {
		call := calls[i]
		if err := ctx.Err(); err != nil {
			results[i] = callResult{err: err}
			return
		}
		resource := toolResource(call.Arguments)
		if err := permission.Authorize(ctx, a.cfg.Policy, a.cfg.BeforeToolCall, call.Name, resource, call.Arguments); err != nil {
			results[i] = callResult{err: err}
			if a.cfg.AfterToolCall != nil {
				a.cfg.AfterToolCall(call.Name, resource, "", err)
			}
			return
		}
		content, err := a.cfg.Tools.Run(ctx, call.Name, call.Arguments)
		results[i] = callResult{content: content, err: err}
		if a.cfg.AfterToolCall != nil {
			a.cfg.AfterToolCall(call.Name, resource, content, err)
		}
	}

	runBatch := func(indices []int) {
		var wg sync.WaitGroup
		for _, i := range indices {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				runOne(i)
			}(i)
		}
		wg.Wait()
	}
	// Consecutive reads may overlap; mutations are barriers in call order.
	var batch []int
	for i := range calls {
		if skipped[i] {
			continue
		}
		if a.cfg.Tools.ModeOf(calls[i].Name) == Concurrent {
			batch = append(batch, i)
		} else {
			runBatch(batch)
			batch = nil
			runOne(i)
		}
	}
	runBatch(batch)

	mutatingBefore := st.mutatingSucceeded
	progressed := false
	exclusiveSucceeded := false
	for i, call := range calls {
		content := results[i].content
		if results[i].err != nil {
			// A failing tool often returns its most useful text
			// alongside the error (compiler output, test failures) —
			// keep it. An error with no output keeps the old shape.
			if strings.TrimSpace(content) != "" {
				content += "\nerror: " + results[i].err.Error()
			} else {
				content = "error: " + results[i].err.Error()
			}
			if !skipped[i] {
				// Skipped calls are already refusals; counting them
				// would let the counter climb without the model ever
				// having re-attempted anything.
				if st.failedCalls == nil {
					st.failedCalls = make(map[string]int)
				}
				st.failedCalls[callKey(call.Name, call.Arguments)]++
			}
		} else {
			progressed = true
			delete(st.failedCalls, callKey(call.Name, call.Arguments))
			if a.cfg.Tools.ModeOf(call.Name) == Exclusive {
				exclusiveSucceeded = true
			}
			if a.cfg.Tools.IdempotentOf(call.Name) {
				if st.idempotentSeen == nil {
					st.idempotentSeen = make(map[string]int)
				}
				st.idempotentSeen[callKey(call.Name, call.Arguments)] = step
			}
			if containsStr(a.cfg.MutatingTools, call.Name) {
				if st.observeChanges == nil {
					st.mutatingSucceeded++
					for _, p := range mutatedPathsFromCall(call.Arguments) {
						if !containsStr(st.mutatedPaths, p) {
							st.mutatedPaths = append(st.mutatedPaths, p)
						}
					}
				}
				if a.rewritesFile(call.Name) {
					if path := writePathFromArgs(call.Arguments); path != "" {
						if st.wrotePaths == nil {
							st.wrotePaths = make(map[string]int)
						}
						st.wrotePaths[path] = step
					}
				}
			}
			if call.Name == "delegate_task" && st.observeChanges == nil {
				if m, paths := parseDelegateMutations(content); m > 0 {
					st.mutatingSucceeded += m
					for _, p := range paths {
						if !containsStr(st.mutatedPaths, p) {
							st.mutatedPaths = append(st.mutatedPaths, p)
						}
					}
				}
			}
		}
		content = TruncateMiddle(content, max(1, 48*1024/len(calls)))
		history = append(history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: call.ID,
			Content:    content,
		})
		if a.cfg.OnToolResult != nil {
			a.cfg.OnToolResult(call.ID, content)
		}
		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: content, Failed: results[i].err != nil})
		}
	}

	// Anything that mutated (an Exclusive call — write/patch/shell — or
	// a subagent reporting writes) invalidates the repeat cache: the
	// same read can now legitimately return something new, and a call
	// that failed before may now succeed.
	if exclusiveSucceeded || st.mutatingSucceeded > mutatingBefore {
		dropRepeatCaches(st)
		// A successful real check applies only to the state it examined.
		if a.cfg.Verify != nil {
			st.verifiedOnce = false
			a.report.Verification = ""
		}
	}

	// A step that changed the workspace is progress by definition, so it
	// can never be evidence of a loop. Writing four files in a row is the
	// normal shape of scaffolding — which is write_file four times, exactly
	// what the same-tool counter used to flag. Live failure, FPS mission:
	// three files were written correctly, tripped the nudge, and the worker
	// was told to "switch to a genuinely different tool" before writing the
	// third acceptance criterion.
	//
	// Exact repeats are handled by callSignature/idempotentSeen and
	// wrotePaths, so what is left here is the narrow case that nudge was
	// written for: grinding one read-only tool with slightly different
	// arguments and never converging. Fresh results restart the chain.
	mutated := st.mutatingSucceeded > mutatingBefore
	if a.observeWorkspace(st) {
		mutated = true
	}
	if mutated {
		st.exploratorySteps = 0
		st.lastSingleTool = ""
		st.lastSingleResult = 0
		st.consecutiveSameToolCount = 0
	} else {
		st.exploratorySteps++
		a.trackSingleToolLoop(calls, results, st)
	}
	if a.cfg.TodoFunding {
		fundTodoSteps(a.cfg.Tools, a.cfg.MaxSteps, st)
		if checkTodoClosing(a.cfg.Tools, st) {
			st.todoFunded += todoClosingSteps
			history = a.nudge(history, NudgeClosing, fmt.Sprintf(prompts.Closing, todoClosingSteps))
		}
		a.report.MaxSteps = a.cfg.MaxSteps + st.todoFunded
	}
	if progressed && !repeat {
		st.stuckSteps = 0
	} else {
		st.stuckSteps++
	}
	return repeat, history
}

func (a *Agent) observeWorkspace(st *runState) bool {
	if st.observeChanges == nil {
		return false
	}
	paths, err := st.observeChanges()
	if err != nil {
		st.observationErr = err
		if a.cfg.OnNudge != nil {
			a.cfg.OnNudge("observation-error", err.Error())
		}
		return false
	}
	if len(paths) == 0 {
		return false
	}
	st.mutatingSucceeded++
	for _, p := range paths {
		if !containsStr(st.mutatedPaths, p) {
			st.mutatedPaths = append(st.mutatedPaths, p)
		}
	}
	dropRepeatCaches(st)
	if a.cfg.Verify != nil {
		st.verifiedOnce = false
		a.report.Verification = ""
	}
	return true
}

// callResult is one executed tool call's outcome: either content or err
// is set. It lives beside the execution that produces it (tools.go)
// rather than the loop that consumes it.
type callResult struct {
	content string
	err     error
}

// callSignature identifies a set of tool calls by name+arguments, order
// independent, so the loop can tell "the model issued the exact same
// call(s) again" apart from "the model made progress" - a tool call that
// succeeds without error is not the same thing as the model moving
// forward if it's the same call as last time.
func callSignature(calls []llm.ToolCall) string {
	parts := make([]string, len(calls))
	for i, c := range calls {
		parts[i] = callKey(c.Name, c.Arguments)
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// callKey identifies one tool call for repeat detection, canonicalizing
// arguments so spelling differences don't read as different calls. A model
// re-reading one file emits {"path":"x"} then {"path": "x",
// "metadata_only": false}; on raw bytes those are two keys and the repeat
// guard never fires. A round-trip through a map sorts keys and drops
// whitespace.
//
// Arguments that aren't a JSON object fall back to raw bytes: better a key
// too specific than one that collapses two different calls into one.
func callKey(name string, args json.RawMessage) string {
	var obj map[string]any
	if err := json.Unmarshal(args, &obj); err != nil {
		return name + ":" + string(args)
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		return name + ":" + string(args)
	}
	return name + ":" + string(canonical)
}
