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

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	toolfiles "github.com/keshon/gektor/internal/tools/files"
	"github.com/keshon/gektor/internal/workspace"
)

func permissionGateContext(autoDeny bool, suspend agent.Suspender, ws *workspace.Workspace) func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
	var approvals permission.Approvals
	return func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
		if approvals.Allows(tool, resource) {
			return permission.Allow, nil
		}
		if autoDeny {
			return permission.Deny, nil
		}
		target := permission.Target(tool, resource)
		prompt := fmt.Sprintf("\n[permission] %s on %q\n[y] Allow once / [a] Allow for this session on %q / [n] Deny", tool, resource, target)
		if preview := toolfiles.Preview(ws, tool, args); preview != "" {
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
		eff, derr := permission.Decide(rep.Answer)
		if derr != nil {
			return permission.Deny, fmt.Errorf("blocked by operator (%s on %s)", tool, resource)
		}

		err = ctx.Err()
		if err != nil {
			return permission.Deny, err
		}
		if permission.SessionAnswer(rep.Answer) {
			approvals.Remember(tool, resource)
		}
		return eff, nil
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

// nudgePrinter shows loop-generated harness notices in print mode;
// json mode carries them as nudge events via the session emitter.
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
