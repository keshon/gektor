package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

func runAgentCLI(ctx context.Context, o resolvedLaunch, cfg harness.Config, forkHistory []llm.Message, emitter *events.Emitter, noteW io.Writer, note func(string, ...any)) error {
	cfg.AskPrompt = func(question string) string {
		return fmt.Sprintf("\n[paused — state saved at %s]\n[resume: gektor -resume %s -answer \"%s\"]\n\n[agent asks] %s", cfg.StateFile, cfg.StateFile, "your answer", question)
	}
	observeAgentCLI(&cfg, o, emitter, noteW, note)
	sess, err := harness.New(cfg)
	if err != nil {
		return err
	}
	defer func() { reportAgentRun(sess.Report(), cfg.StateFile, o, note) }()
	var result string
	if o.resume != "" {
		history, err := conversation.ReadHistory(o.resume)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		note("resuming from %s (%d messages)", o.resume, len(history))
		result, err = sess.Resume(ctx, history, prompts.Resume)
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else if len(forkHistory) > 0 {
		continuation := strings.TrimSpace(cfg.Task)
		if continuation == "" {
			continuation = prompts.Resume
		}
		result, err = sess.Continue(ctx, forkHistory, continuation, cfg.Images...)
	} else {
		result, err = sess.Run(ctx)
	}
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}
	note("\n=== result ===")
	_, err = fmt.Fprintln(noteW, result)
	return err
}

func observeAgentCLI(cfg *harness.Config, o resolvedLaunch, emitter *events.Emitter, output io.Writer, note func(string, ...any)) {
	cfg.OnEvent = func(ev harness.Event) {
		if o.outputMode == "json" {
			emitter.Emit(ev.Name, ev.Fields)
			return
		}
		switch ev.Name {
		case "finding":
			fmt.Fprintf(output, "check %v %v:%v %v\n", ev.Fields["rule"], ev.Fields["path"], ev.Fields["line"], ev.Fields["summary"])
		case "local_warning":
			note("%v", ev.Fields["text"])
		}
	}
	if o.outputMode == "json" {
		return
	}
	cfg.Env.OnStep = func(label string, step int, msg llm.Message) { printStep(label, step, msg, o.logMax) }
	cfg.Env.OnDelta = func(chunk string) { fmt.Fprint(output, chunk) }
	cfg.Env.OnToolOutput = func(_ string, text string) { fmt.Fprint(output, text) }
	cfg.Env.OnNudge = nudgePrinter(o.outputMode)
	cfg.Env.OnPrompt = func(report llm.ContextReport) {
		if report.Request == 1 && report.BackendTokens == 0 {
			note("startup context: ~%d tokens (%d tool schemas); estimates exclude backend template/tokenization", report.EstimatedTokens, len(report.Tools))
			for _, section := range report.Sections {
				if section.EstimatedTokens > 0 {
					note("  %s: ~%d", section.Name, section.EstimatedTokens)
				}
			}
		}
	}
}
