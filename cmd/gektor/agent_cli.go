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
	observer := &cliObserver{
		outputMode: o.outputMode,
		logMax:     o.logMax,
		emitter:    emitter,
		output:     output,
		note:       note,
	}
	cfg.Events = observer
	if o.outputMode == "json" {
		return
	}

	cfg.Env.OnStep = observer.step
	cfg.Env.OnDelta = observer.delta
	cfg.Env.OnToolOutput = observer.toolOutput
	cfg.Env.OnNudge = observer.nudge
	cfg.Env.OnPrompt = observer.prompt
}

// cliObserver presents session events and plain-terminal streaming output.
type cliObserver struct {
	outputMode string
	logMax     int
	emitter    *events.Emitter
	output     io.Writer
	note       func(string, ...any)
}

func (o *cliObserver) Handle(event events.Event) {
	if o.outputMode == "json" {
		o.emitter.Handle(event)
		return
	}

	switch event.Name {
	case "finding":
		fmt.Fprintf(o.output, "check %v %v:%v %v\n", event.Fields["rule"], event.Fields["path"], event.Fields["line"], event.Fields["summary"])
	case "local_warning":
		o.note("%v", event.Fields["text"])
	}
}

func (o *cliObserver) step(label string, step int, message llm.Message) {
	printStep(label, step, message, o.logMax)
}

func (o *cliObserver) delta(text string) {
	fmt.Fprint(o.output, text)
}

func (o *cliObserver) toolOutput(_ string, text string) {
	fmt.Fprint(o.output, text)
}

func (o *cliObserver) nudge(kind, text string) {
	fmt.Printf("nudge [%s] %s\n", kind, text)
}

func (o *cliObserver) prompt(report llm.ContextReport) {
	if report.Request != 1 || report.BackendTokens != 0 {
		return
	}

	o.note("startup context: ~%d tokens (%d tool schemas); estimates exclude backend template/tokenization", report.EstimatedTokens, len(report.Tools))
	for _, section := range report.Sections {
		if section.EstimatedTokens > 0 {
			o.note("  %s: ~%d", section.Name, section.EstimatedTokens)
		}
	}
}
