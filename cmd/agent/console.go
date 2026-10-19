package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/checks"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/permission"
	"github.com/keshon/tars/internal/run"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/workspace"
)

func permissionGateContext(autoDeny bool, suspend agent.Suspender, ws *workspace.Workspace) func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
	var mu sync.Mutex
	always := map[[2]string]bool{}
	return func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		key := [2]string{tool, resource}
		mu.Lock()
		allowed := always[key]
		mu.Unlock()
		if allowed {
			return permission.Allow, nil
		}
		if autoDeny {
			return permission.Deny, nil
		}
		prompt := fmt.Sprintf("\n[permission] %s on %q - allow? [y]es once / [a]lways / [n]o", tool, resource)
		if preview := tools.PreviewArgs(ws, tool, args); preview != "" {
			prompt += "\n" + preview
		}
		rep, err := suspend(ctx, agent.SuspendRequest{
			Kind:     agent.SuspendPermission,
			Tool:     tool,
			Resource: resource,
			Prompt:   prompt,
		})
		if err != nil {
			return permission.Deny, err
		}
		if eff, derr := permission.Decide(rep.Answer); derr != nil {
			return permission.Deny, fmt.Errorf("blocked by operator (%s on %s)", tool, resource)
		} else {
			if strings.EqualFold(strings.TrimSpace(rep.Answer), "a") || strings.EqualFold(strings.TrimSpace(rep.Answer), "always") {
				mu.Lock()
				always[key] = true
				mu.Unlock()
			}
			return eff, nil
		}
	}
}

// stdioSuspender answers gates from the terminal: the current behavior,
// centralized so future transports replace one constructor instead of
// three stdin blocks. Prompts go to noteW (stderr in -mode json), never
// to event stdout. Each suspension emits awaiting_input/input_answered
// events - the vocabulary a future TUI or RPC client already speaks.
func stdioSuspender(noteW io.Writer, emitter *events.Emitter) agent.Suspender {
	reader := bufio.NewReader(os.Stdin)
	type input struct {
		line string
		err  error
	}
	replies := make(chan input, 1)
	var once sync.Once
	flight := make(chan struct{}, 1)
	return func(ctx context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
		select {
		case flight <- struct{}{}:
		case <-ctx.Done():
			return agent.SuspendReply{}, ctx.Err()
		}
		defer func() { <-flight }()
		once.Do(func() {
			go func() {
				for {
					line, err := reader.ReadString('\n')
					replies <- input{line, err}
					if err != nil {
						close(replies)
						return
					}
				}
			}()
		})
		if emitter != nil {
			emitter.Emit("awaiting_input", map[string]any{"kind": string(req.Kind), "id": req.ID})
		}
		fmt.Fprintf(noteW, "%s\n> ", req.Prompt)
		select {
		case <-ctx.Done():
			return agent.SuspendReply{}, ctx.Err()
		case rep, ok := <-replies:
			if !ok {
				return agent.SuspendReply{}, fmt.Errorf("input transport closed")
			}
			if rep.err != nil && strings.TrimSpace(rep.line) == "" {
				return agent.SuspendReply{}, fmt.Errorf("reading answer: %w", rep.err)
			}
			if emitter != nil {
				emitter.Emit("input_answered", map[string]any{"kind": string(req.Kind), "id": req.ID})
			}
			return agent.SuspendReply{Answer: strings.TrimSpace(rep.line)}, nil
		}
	}
}

func stepPrinter(mode string, emitter *events.Emitter, logMax int) func(string, int, llm.Message) {
	if mode == "json" && emitter != nil {
		return func(label string, step int, msg llm.Message) {
			run.EmitStep(emitter, label, step, msg, false, 0)
		}
	}
	return func(label string, step int, msg llm.Message) { printStep(label, step, msg, logMax) }
}

// usagePrinter mirrors stepPrinter for token counts: JSON events in
// json mode, nothing in print mode (the debug log already records them).
func usagePrinter(mode string, emitter *events.Emitter) func(int, llm.Usage) {
	if mode == "json" && emitter != nil {
		return func(step int, usage llm.Usage) { run.EmitUsage(emitter, step, usage) }
	}
	return nil
}

// findingSink shows deterministic check findings: JSON finding events in
// json mode, one plain line otherwise. It used to return nil for json,
// which disabled scanning outright: a nil sink means no reporter, and no
// reporter means the file tools are never wrapped.
func findingSink(emitter *events.Emitter, mode string) func(checks.Scope, checks.Finding) {
	if mode == "json" && emitter != nil {
		return func(scope checks.Scope, f checks.Finding) {
			events.EmitFinding(emitter, string(scope), f.Rule, f.Path, f.Line, f.Summary)
		}
	}
	return func(_ checks.Scope, f checks.Finding) {
		fmt.Printf("check %s %s:%d %s\n", f.Rule, f.Path, f.Line, f.Summary)
	}
}

// nudgePrinter shows loop-generated harness notices in print mode;
// json mode carries them as nudge events via the session emitter.
func nudgePrinter(mode string) func(kind, text string) {
	if mode == "json" {
		return nil
	}
	return func(kind, text string) {
		fmt.Printf("nudge [%s] %s\n", kind, text)
	}
}

func printStep(label string, step int, msg llm.Message, logMax int) {
	prefix := fmt.Sprintf("[step %d]", step)
	if label != "" {
		prefix = fmt.Sprintf("[%s step %d]", label, step)
	}
	if msg.Content != "" {
		fmt.Printf("%s %s\n", prefix, agent.TruncateMiddle(msg.Content, logMax))
	}
	for _, tc := range msg.ToolCalls {
		fmt.Printf("%s -> %s(%s)\n", prefix, tc.Name,
			agent.TruncateMiddle(string(tc.Arguments), logMax))
	}
}
