package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/session"
)

func runAgentCLI(ctx context.Context, o resolvedLaunch, cfg run.Config, forkHistory []llm.Message, emitter *events.Emitter, noteW io.Writer, note func(string, ...any)) error {
	// ask_user blocks on stdin inside the tool. State is snapshotted by
	// the agent loop after the assistant message (with the unanswered tool
	// call) and before tool execution — so Ctrl+C while waiting leaves a
	// resumable state.json. Use -resume with -answer to continue.
	askFn := agent.NewAsker(0).Ask(func(ctx context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
		return cfg.Answer(ctx, req)
	}, func(question string) string {
		return fmt.Sprintf("\n[paused — state saved at %s]\n"+
			"[resume: gektor -resume %s -answer \"your answer\"]\n"+
			"\n[agent asks] %s", cfg.StateFile, cfg.StateFile, question)
	})

	env := cfg.Env.Refreshed(ctx)
	var err error
	var a *agent.Agent
	if o.workflowRecipe != "" {
		a, err = roles.WorkflowAuthor(env, o.workflowRecipe, cfg.StateFile)
		if err != nil {
			return err
		}
	} else if o.plan {
		a = roles.Planner(env, "plan", cfg.StateFile, askFn)
	} else {
		a = roles.Interactive(env, "", cfg.StateFile, askFn, cfg.Verify)
	}

	defer reportAgentRun(a, env, emitter, cfg.StateFile, o, note)

	var result string
	if o.resume != "" {
		history, err := session.ReadHistory(o.resume)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		note("resuming from %s (%d messages)", o.resume, len(history))

		// The history is off disk and no agent exists yet, so the question is
		// recognised by its argument shape rather than by a tool we could ask.
		if callID, question, paused := agent.PausedOnQuestion(nil, history); paused {
			ans := o.answer
			if ans == "" {
				var err error
				ans, err = askFn(ctx, question)
				if err != nil {
					return fmt.Errorf("reading answer: %w", err)
				}
			}
			result, err = a.ResumeWithAnswer(ctx, history, callID, ans)
		} else {
			result, err = a.Resume(ctx, history, prompts.Resume)
		}
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else if len(forkHistory) > 0 {
		var err error
		note := strings.TrimSpace(cfg.Task)
		if note == "" {
			note = prompts.Resume
		}
		result, err = a.Resume(ctx, forkHistory, note, cfg.Images...)
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else {
		var err error
		result, err = a.Run(ctx, cfg.Task, cfg.Images...)
		if err != nil && o.workflowRecipe == "" {
			// A run that hit MaxSteps with work still open gets one more
			// budget if the operator says yes. This used to exist only in
			// the API session, so the primary CLI silently never offered it.
			if ans, cerr, ok := agent.BudgetContinue(ctx, a, err, cfg.StateFile, cfg.Answer); ok {
				result, err = ans, cerr
			}
		}
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	}
	note("\n=== result ===")
	fmt.Fprintln(noteW, result)
	return nil
}
