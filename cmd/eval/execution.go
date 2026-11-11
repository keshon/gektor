package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/workspace"
)

func prepareProbeAttachments(p Probe, work, tempRoot string) ([]string, error) {
	var images []string
	for i, name := range p.Attachments {
		target := filepath.Join(tempRoot, fmt.Sprintf("attachment-%d%s", i+1, filepath.Ext(name)))
		if err := os.Rename(filepath.Join(work, filepath.FromSlash(name)), target); err != nil {
			return nil, fmt.Errorf("prepare attachment: %w", err)
		}
		images = append(images, target)
	}
	return images, nil
}

func observeProbeResults(env roles.Env, res *Result, calls *[]call, started time.Time) roles.Env {
	env.OnResult = func(result agent.ToolResult) {
		for i := len(*calls) - 1; i >= 0; i-- {
			if (*calls)[i].id == result.CallID {
				if (*calls)[i].tool == "delegate_task" {
					if metric, ok := parseDelegateMetrics(result.Output); ok {
						res.Delegates = append(res.Delegates, metric)
					}
				}
				break
			}
		}
		res.ToolSeconds += result.Duration.Seconds()
		recordToolResult(*calls, result)
		if res.FirstToolSeconds == 0 {
			res.FirstToolSeconds = time.Since(started).Seconds()
		}
		if result.Failed {
			if res.ToolFailures == nil {
				res.ToolFailures = map[string]int{}
			}
			res.ToolFailures[result.Status]++
		}
	}
	env.OnPrompt = func(report llm.ContextReport) {
		if report.Request == 1 {
			res.StartupContext = report
		}
	}
	return env
}

func checkProbeOutcome(ctx context.Context, p Probe, ws *workspace.Workspace, images []string, calls []call, answer string, res *Result) {
	checkStarted := time.Now()
	res.ArtifactFailures = checkArtifacts(ctx, p, ws)
	if len(images) > 0 {
		files, _ := generatedFixture(p.Generated)
		for i, path := range images {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != string(files[p.Attachments[i]]) {
				res.ArtifactFailures = append(res.ArtifactFailures, "original attachment changed")
			}
		}
	}
	artifactPassed := len(res.ArtifactFailures) == 0
	res.ArtifactPassed = &artifactPassed
	res.CheckSeconds = time.Since(checkStarted).Seconds()
	res.Failures = append(res.Failures, res.ArtifactFailures...)
	res.Failures = append(res.Failures, checkTrace(p, calls)...)
	if p.Generated != "" {
		if err := checkWorkflowTrace(calls, ws); err != nil {
			res.Failures = append(res.Failures, err.Error())
		}
	}
	res.Failures = append(res.Failures, checkAnswer(p, answer)...)
}

// roles.Interactive is what cmd/gektor builds too, so the probe
// scores the agent that ships. This used to be a hand-copied
// config here and it had already drifted: no ask_user, no
// delegate_task, while two probes are specifically about whether
// those get reached for when they shouldn't be.
//
// The responder says plainly that nobody is there rather than
// faking an answer — dropping the tool would change the tool set,
// and inventing an answer would make the probe score the answer.
// Whether ask_user was called at all stays visible in the trace,
// which is what probe 12 measures.
func executeProbeAgent(ctx context.Context, p Probe, env roles.Env, images []string, res *Result) string {
	replyIndex := 0
	stateFile := ""
	if res.Trace != "" {
		stateFile = res.Trace + ".state.json"
	}
	a, err := roles.Interactive(env, "", stateFile, func(context.Context, string) (string, error) {
		if replyIndex < len(p.Replies) {
			reply := p.Replies[replyIndex]
			replyIndex++
			return reply, nil
		}
		return "This is an automated evaluation run; no human is available. " +
			"State your assumption and proceed with the smallest reasonable action.", nil
	}, nil)
	if err != nil {
		res.RunError = firstLine(err.Error())
		return ""
	}
	runStarted := time.Now()
	var final string
	if len(p.Continuation) == 0 {
		final, err = a.Run(ctx, p.Task, images...)
	} else {
		// Recorded turns follow an already-started Act run. Retain its mode
		// notice here so resumption does not append a fresh directive after
		// the fixture and inadvertently frame completed work as a new task.
		history := []llm.Message{
			{Role: llm.RoleSystem},
			{Role: llm.RoleUser, Content: p.Task, Images: images},
			{Role: llm.RoleUser, Harness: "notice", Content: "[harness] " + prompts.ActMode},
		}
		history = append(history, p.Continuation...)
		final, err = a.Resume(ctx, history, "")
	}
	res.RunSeconds = time.Since(runStarted).Seconds()
	res.Warnings, res.Summary = a.Report().Warnings, a.Report().Summary
	if err != nil {
		res.RunError = firstLine(err.Error())
	}

	return final
}
