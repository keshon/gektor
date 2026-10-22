// Package agent contains the model-agnostic, backend-agnostic agent loop:
// talk to an llm.Client, run whatever tools it asks for, and decide when to
// stop or change strategy. It has no idea koboldcpp or filesystem tools
// exist — those live in internal/llm and internal/tools.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

// ToolResult carries execution status separately from model-facing prose.
type ToolResult struct {
	CallID string
	Output string
	Failed bool
	Status string
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

// Attacher is implemented by a tool whose successful result includes
// pictures the model should see. The loop asks for absolute paths after
// Run succeeds and appends them to a user turn, because the OpenAI wire
// format has no image parts in a role:tool message. A tool without this
// interface is exactly as before.
type Attacher interface {
	Attach(args json.RawMessage) ([]string, error)
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
	if r == nil {
		return nil
	}
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
	return r.RunStreaming(ctx, name, args, nil)
}

func (r *Registry) ValidateCall(name string, args json.RawMessage) error {
	if r == nil {
		return fmt.Errorf("unknown tool %q", name)
	}
	t, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	return validateArguments(args, t.Schema())
}

// RunStreaming observes display-only output when the tool supports it.
// The returned result remains authoritative for model history.
func (r *Registry) RunStreaming(ctx context.Context, name string, args json.RawMessage, onOutput func(string)) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if err := validateArguments(args, t.Schema()); err != nil {
		return "", err
	}
	if streaming, ok := t.(interface {
		RunStreaming(context.Context, json.RawMessage, func(string)) (string, error)
	}); ok && onOutput != nil {
		return streaming.RunStreaming(ctx, args, onOutput)
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

// AttacherOf returns the tool's picture-returning half, or nil. Like
// other optional capabilities, this is opt-in.
func (r *Registry) AttacherOf(name string) Attacher {
	t, ok := r.tools[name]
	if !ok {
		return nil
	}
	a, _ := t.(Attacher)
	return a
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
// reads for reporting and restores on resume. *tools.Todo is one. The
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

// TodoProgress reports the checklist tool's state: done count and
// still-open item texts. Nil-receiver safe for tool-less runs.
func (r *Registry) TodoProgress() (done int, open []string) {
	if cl := r.Checklist(); cl != nil {
		return cl.Progress()
	}
	return 0, nil
}

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

type callResult struct {
	content string
	err     error
}

// Keep refused calls in the transcript without letting malformed RawMessage
// bytes break persistence or the next backend request. Execution checks the
// original arguments; the paired error result explains this placeholder.
func recordableMessage(msg llm.Message) llm.Message {
	for i, call := range msg.ToolCalls {
		if !json.Valid(call.Arguments) || call.InputError != "" {
			msg.ToolCalls = append([]llm.ToolCall(nil), msg.ToolCalls...)
			for j := i; j < len(msg.ToolCalls); j++ {
				if !json.Valid(msg.ToolCalls[j].Arguments) || msg.ToolCalls[j].InputError != "" {
					msg.ToolCalls[j].RawArguments = string(msg.ToolCalls[j].Arguments)
					if msg.ToolCalls[j].InputError == "" {
						msg.ToolCalls[j].InputError = "invalid_json"
					}
					msg.ToolCalls[j].Arguments = nil
				}
			}
			break
		}
	}
	return msg
}

// maxToolImagesPerStep caps the pictures one assistant step may pull into
// the conversation. A model that reads a directory of screenshots would
// otherwise pay the vision-encoder cost for every one of them in a single
// turn, and the estimate used by the context meter deliberately
// undercounts that cost. Four is enough for a screenshot and its
// reference; more is usually the model losing track of what it asked for.
const maxToolImagesPerStep = 4

// executeToolCalls overlaps adjacent reads and serializes every mutation in
// model call order.
//
// Repeat detection compares calls and results: refreshed data is progress.
func (a *Agent) executeToolCalls(ctx context.Context, st *runState, step int, finishReason string, calls []llm.ToolCall, history []llm.Message) []llm.Message {
	results := make([]callResult, len(calls))
	if a.cfg.OnProgress != nil {
		a.cfg.OnProgress(llm.Progress{Phase: "tools", Detail: fmt.Sprintf("executing %d tool calls", len(calls))})
	}

	skipped := make([]bool, len(calls))
	var newInstructions []string
	for i, call := range calls {
		// Truncated arguments (generation cut off mid-call) are stumps, not
		// calls: executing them fails arg parsing here and 500s backends that
		// parse server-side. Refuse with reissue-smaller guidance instead.
		if !json.Valid(call.Arguments) || call.InputError != "" {
			skipped[i] = true
			kind := "invalid"
			if finishReason == "length" {
				kind = "truncated"
			}
			results[i].err = fmt.Errorf("%s JSON arguments; call %s (%s) was NOT executed. Arguments were supplied but could not be parsed, not omitted. finish_reason=%q. Reissue complete valid arguments%s. Original input excerpt (%d bytes):\n%s", kind, call.ID, call.Name, finishReason, map[bool]string{true: " in smaller calls because the output limit was reached", false: ""}[finishReason == "length"], len(call.Arguments), TruncateMiddle(string(call.Arguments), 1800))
			continue
		}
		// A read-only role is offered the full tool set so a mode switch
		// cannot rewrite the prompt prefix, so refusal has to be explicit:
		// nothing runs, nothing changes, and the model is told what to do
		// instead.
		if err := a.cfg.Tools.ValidateCall(call.Name, call.Arguments); err != nil {
			skipped[i] = true
			results[i].err = err
			continue
		}
		if !a.mayRun(call.Name) {
			skipped[i] = true
			results[i].err = fmt.Errorf(
				"this run is read-only: %s is unavailable and nothing was changed - "+
					"state the change as a step in the plan and let the operator apply it", call.Name)
			continue
		}

		if a.cfg.Instructions != nil && !skipped[i] {
			text, err := a.cfg.Instructions(call.Arguments)
			if err != nil {
				skipped[i] = true
				results[i].err = fmt.Errorf("load scoped instructions: %w", err)
				continue
			}
			loaded := false
			for _, msg := range history {
				if strings.Contains(msg.Content, text) {
					loaded = true
					break
				}
			}
			if text != "" && !loaded {
				if !containsStr(newInstructions, text) {
					newInstructions = append(newInstructions, text)
				}
				if a.cfg.Tools.ModeOf(call.Name) == Exclusive {
					skipped[i] = true
					results[i].err = fmt.Errorf("new scoped instructions were discovered; nothing was executed. Read the following harness instructions and reconsider this call before retrying")
				}
			}
		}
	}

	executed := make([]bool, len(calls))
	runOne := func(i int) {
		call := calls[i]
		if err := ctx.Err(); err != nil {
			results[i].err = err
			return
		}
		var resources []string
		if a.cfg.Resources != nil {
			var err error
			resources, err = a.cfg.Resources(call.Name, call.Arguments)
			if err != nil {
				results[i].err = err
				return
			}
		}
		if len(resources) == 0 {
			resources = []string{toolResource(call.Arguments)}
		}
		for _, resource := range resources {
			if err := permission.Authorize(ctx, a.cfg.Policy, a.cfg.BeforeToolCall, call.Name, resource, call.Arguments); err != nil {
				results[i].err = err
				return
			}
		}
		executed[i] = true
		var onOutput func(string)
		if a.cfg.OnToolOutput != nil {
			onOutput = func(text string) { a.cfg.OnToolOutput(call.ID, text) }
		}
		results[i].content, results[i].err = a.cfg.Tools.RunStreaming(ctx, call.Name, call.Arguments, onOutput)
	}
	runBatch := func(indices []int) {
		var wg sync.WaitGroup
		for _, i := range indices {
			wg.Add(1)
			go func(i int) { defer wg.Done(); runOne(i) }(i)
		}
		wg.Wait()
	}
	var batch []int
	for i, call := range calls {
		if skipped[i] {
			continue
		}
		if a.cfg.Tools.ModeOf(call.Name) == Concurrent {
			batch = append(batch, i)
		} else {
			runBatch(batch)
			batch = nil
			runOne(i)
			// A failed command may still change files. Treat it as a mutation barrier.
			if executed[i] {
				st.noteMutation()
			}
		}
	}
	runBatch(batch)

	mutatingBefore := st.mutatingSucceeded
	var pictures []string
	var signature strings.Builder
	var diagnostics []string
	for i, call := range calls {
		if results[i].err == nil {
			if at := a.cfg.Tools.AttacherOf(call.Name); at != nil {
				paths, err := at.Attach(call.Arguments)
				switch {
				case err != nil:
					results[i].err = fmt.Errorf("attach picture: %w", err)
				case len(paths) > 0 && len(pictures)+len(paths) > maxToolImagesPerStep:
					results[i].err = fmt.Errorf("at most %d pictures can be attached in one step; this picture was not attached — re-read it in a later step", maxToolImagesPerStep)
				default:
					pictures = append(pictures, paths...)
				}

			}
		}
		content := results[i].content
		if results[i].err != nil {
			if strings.TrimSpace(content) != "" {
				content += "\n"
			}
			content += "error: " + results[i].err.Error()
		} else {
			if st.observeChanges == nil {
				// Embedders may omit the workspace observer.
				if containsStr(a.cfg.MutatingTools, call.Name) {
					st.mutatingSucceeded++
					st.addPaths(mutatedPathsFromCall(call.Arguments))
				}
				if call.Name == "delegate_task" {
					mutations, paths := parseDelegateMutations(content)
					st.mutatingSucceeded += mutations
					st.addPaths(paths)
				}
			}
		}
		if a.cfg.LimitOutput != nil {
			content = a.cfg.LimitOutput(ctx, call.Name, content, max(1, 48*1024/len(calls)))
		} else {
			content = TruncateMiddle(content, max(1, 48*1024/len(calls)))
		}
		failed := results[i].err != nil
		fmt.Fprintf(&signature, "%s\x00%s\x00", callKey(call.Name, call.Arguments), content)
		status := "success"
		if failed {
			status = "execution_error"
			if !executed[i] {
				status = "rejected"
			}
			if errors.Is(results[i].err, context.Canceled) || errors.Is(results[i].err, context.DeadlineExceeded) {
				status = "cancelled"
			}
			if strings.HasPrefix(results[i].err.Error(), "invalid_arguments:") {
				status = "invalid_arguments"
			}
		}
		if !json.Valid(call.Arguments) || call.InputError != "" {
			status = "invalid_json"
			if finishReason == "length" {
				status = "output_limit"
			}
			if call.InputError == "incomplete_stream" {
				status = "incomplete_stream"
			}
			diagnostics = append(diagnostics, content)
		} else {
			history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: content, ToolFailed: &failed, ToolStatus: status})
		}

		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: content, Failed: failed, Status: status})
		}
	}
	for _, diagnostic := range diagnostics {
		history = a.nudge(history, "tool_input", diagnostic)
	}
	for _, instructions := range newInstructions {
		history = a.nudge(history, "instructions", instructions)
	}
	st.trackSignature(signature.String())
	if len(pictures) > 0 {
		// The tool result is already in history; the pictures ride the
		// next user turn because role:tool has no image parts on the
		// OpenAI wire format. The note says where they came from, so a
		// model does not mistake harness output for fresh user input.
		history = append(history, llm.Message{
			Role:    llm.RoleUser,
			Content: pictureNote(pictures),
			Images:  pictures,
		})
	}

	// A successful mutation is progress, even when calls repeat.
	if st.mutatingSucceeded > mutatingBefore {
		st.noteMutation()
	}
	return history
}

// pictureNote labels attached pictures as tool output. The model must not
// read them as a new instruction from the operator; the paths let it
// connect each picture to the read_file result it requested.
func pictureNote(paths []string) string {
	var b strings.Builder
	b.WriteString("Pictures returned by tools (tool output, not a new user request):\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	b.WriteString("Use what you see to continue the task.")
	return b.String()
}

func (st *runState) addPaths(paths []string) {
	for _, p := range paths {
		if !containsStr(st.mutatedPaths, p) {
			st.mutatedPaths = append(st.mutatedPaths, p)
		}
	}
}

// A single end-of-run comparison also catches shell, MCP and failed-tool effects.
func (a *Agent) observeWorkspace(st *runState) {
	if st.observeChanges == nil {
		return
	}
	paths, err := st.observeChanges()
	if err != nil {
		st.observationErr = err
		return
	}
	st.addPaths(paths)
	st.mutatingSucceeded = len(paths)
}
