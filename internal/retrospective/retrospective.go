// Package retrospective reviews archived executions without resuming the task.
// Review and lesson creation are experimental.
package retrospective

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/workspace"
)

type Finding struct {
	Text     string `json:"text"`
	Evidence []int  `json:"evidence"`
}

// Memory is an observation with a literal historical quote, not a verified
// universal instruction. A successful tool result cannot prove generality.
type Memory struct {
	When     string `json:"when"`
	Lesson   string `json:"lesson"`
	Evidence []int  `json:"evidence"`
	Quote    string `json:"quote"`
}

type Result struct {
	Summary      string    `json:"summary"`
	Findings     []Finding `json:"findings"`
	Memory       []Memory  `json:"memory"`
	Scope        string    `json:"scope"`
	Start        int       `json:"start"`
	End          int       `json:"end"`
	Read         int       `json:"messages_read"`
	Partial      int       `json:"partially_read_messages"`
	SourceSHA256 string    `json:"source_sha256"`
	Saved        string    `json:"saved,omitempty"`
	Warning      string    `json:"warning,omitempty"`
}

type Config struct {
	Client                  llm.Client
	Workspace               *workspace.Workspace
	StateFile, Scope, Focus string
	ContextLimit, MaxTokens int
	OnProgress              func(Progress)
}

// Progress reports observed work, not an estimate of time remaining.
type Progress struct {
	Stage          string
	Scope, Request string
	Step, Steps    int
	Read, Total    int
}

// Evaluate uses a fresh, bounded agent with archive-reading tools only. It
// cannot run commands, edit source, delegate, or change the original context.
func Evaluate(ctx context.Context, c Config) (result Result, runErr error) {
	progress := Progress{Stage: "Loading saved archive", Steps: 12}
	report := func(stage string) {
		progress.Stage = stage
		if c.OnProgress != nil {
			c.OnProgress(progress)
		}
	}
	report(progress.Stage)
	if c.Client == nil || c.Workspace == nil {
		return result, fmt.Errorf("evaluation needs a model and workspace")
	}

	data, err := os.ReadFile(c.StateFile)
	if err != nil {
		return result, err
	}

	snapshot, err := conversation.Decode(data)
	if err != nil {
		return result, err
	}

	start, end, label, err := scope(snapshot, c.Scope)
	if err != nil {
		return result, err
	}

	archive := &archiveTool{snapshot: snapshot, seen: map[int]string{}}
	submit := &submitTool{
		archive: archive,
		start:   start,
		end:     end,
	}
	progress.Total = end - start
	progress.Scope = label
	progress.Request = agent.TruncateMiddle(conversation.DisplayInput(snapshot.History[start].Content), 240)
	report("Reading selected archive")
	failurePath := filepath.Join(filepath.Dir(c.StateFile), "evaluation-failure.json")
	lastValidation := ""
	calls := map[string]string{}
	completed := 0
	defer func() {
		if runErr == nil {
			if err := os.Remove(failurePath); err != nil && !os.IsNotExist(err) {
				runErr = fmt.Errorf("review saved, but could not clear previous failure: %w", err)
			}

			return
		}

		failureProgress := progress
		failureProgress.Step = completed
		failure := &Failure{
			Progress:       failureProgress,
			Cause:          conversation.FailureText(runErr),
			LastValidation: lastValidation,
			Path:           failurePath,
		}

		switch {
		case errors.Is(runErr, agent.ErrMaxSteps):
			failure.Recovery = "Review budget exhausted. Try /learn last with a specific focus, or select one run instead of session."
		case lastValidation != "":
			failure.Recovery = "The reviewer could not submit valid evidence. Inspect the validation error below; retry with a narrower focus. No lesson was saved."
		default:
			failure.Recovery = "No lesson was saved. Resolve the error before starting a fresh review."
		}

		if err := writeJSON(failurePath, failure); err != nil {
			failure.Path = ""
			failure.Recovery += " Diagnostics could not be saved: " + err.Error()
		}

		failure.err = runErr
		runErr = failure
	}()

	maxTokens := c.MaxTokens
	if maxTokens <= 0 || maxTokens > 4096 {
		maxTokens = 4096
	}

	// Construct the reviewer locally so task tools, identity, workspace
	// instructions, inbox, and transcript persistence cannot be inherited.
	a := agent.New(agent.Config{
		System:       prompts.Evaluation,
		MaxSteps:     12,
		Tools:        agent.NewRegistry(archive, submit),
		Client:       c.Client,
		MaxTokens:    maxTokens,
		ContextLimit: c.ContextLimit,
		OnProgress: func(_ llm.Progress) {
			progress.Step = completed + 1
			if progress.Step > progress.Steps {
				progress.Step = progress.Steps
				report("Waiting for final model response (step limit reached)")
				return
			}

			report("Waiting for model response")
		},
		OnStep: func(step int, msg llm.Message) {
			for _, call := range msg.ToolCalls {
				calls[call.ID] = call.Name
			}

			completed = step + 1
			progress.Step = completed
			stage := "Checking model response"
			if len(msg.ToolCalls) > 0 {
				switch msg.ToolCalls[0].Name {
				case "read_history":
					stage = "Reading archive excerpts"
				case "submit_evaluation":
					stage = "Validating evaluation"
				}
			}

			report(stage)
		},
		OnResult: func(result agent.ToolResult) {
			if result.Failed && calls[result.CallID] == "submit_evaluation" {
				lastValidation = agent.TruncateMiddle(result.Output, 1000)
			}

			progress.Read = 0

			for id := range archive.seen {
				if id > start && id <= end {
					progress.Read++
				}
			}

			report(progress.Stage)
		},
	})
	request := fmt.Sprintf("Review %s, archive messages %d–%d (one-based, inclusive). Focus: %s\n\n%s", label, start+1, end, c.Focus, overview(snapshot, start, end))
	if _, err := a.Run(ctx, request); err != nil {
		return result, fmt.Errorf("evaluation: %w", err)
	}

	if submit.result == nil {
		return result, fmt.Errorf("model finished without submitting a supported evaluation")
	}

	result = *submit.result
	result.Scope, result.Start, result.End = label, start+1, end

	for id := range archive.seen {
		if id > start && id <= end {
			result.Read++
			if archive.covered[id] < len([]rune(messageText(snapshot.History[id-1]))) {
				result.Partial++
			}
		}
	}

	result.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	current, err := os.ReadFile(c.StateFile)
	if err != nil || sha256.Sum256(current) != sha256.Sum256(data) {
		return Result{}, fmt.Errorf("session changed during evaluation; run /eval again")
	}

	path := filepath.Join(filepath.Dir(c.StateFile), "evaluation.json")
	report("Saving evaluation")
	if err := writeJSON(path, result); err != nil {
		return Result{}, err
	}

	return result, nil
}
