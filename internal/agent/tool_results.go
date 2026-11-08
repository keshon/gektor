package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

func (a *Agent) recordToolBatch(ctx context.Context, st *runState, batch *toolBatch, finishReason string, history []llm.Message, newInstructions []string) []llm.Message {
	calls, results := batch.calls, batch.results
	executed, withheld, yielded := batch.executed, batch.withheld, batch.yielded
	mutatingBefore := st.mutatingSucceeded
	var pictures []string
	var signature strings.Builder
	var diagnostics []string
	for i, call := range calls {
		if results[i].err == nil && !withheld[i] {
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
		} else if !withheld[i] {
			if st.observeChanges == nil {
				// Embedders may omit the workspace observer.
				if containsStr(a.cfg.MutatingTools, call.Name) {
					st.mutatingSucceeded++
					st.addPaths(mutatedPathsFromCall(call.Arguments))
				}
			}
		}
		// A failed delegate may have changed files before it stopped.
		if executed[i] && !withheld[i] && st.observeChanges == nil && call.Name == "delegate_task" {
			mutations, paths := parseDelegateMutations(content)
			st.mutatingSucceeded += mutations
			st.addPaths(paths)
		}
		if withheld[i] {
			content = "Not executed because new user input arrived. Reconsider this action using the updated instructions."
		}
		if a.cfg.LimitOutput != nil {
			content = a.cfg.LimitOutput(ctx, call.Name, content, max(1, 48*1024/len(calls)))
		} else {
			content = TruncateMiddle(content, max(1, 48*1024/len(calls)))
		}
		failed := results[i].err != nil
		if !withheld[i] && (a.cfg.Tools.ModeOf(call.Name) == Concurrent || failed) {
			a.noteObservation(st, call.Name, call.Arguments, content)
		}
		fmt.Fprintf(&signature, "%s\x00%s\x00", callKey(call.Name, call.Arguments), content)
		status := toolResultStatus(call, results[i], finishReason, executed[i], withheld[i])
		if executed[i] && !withheld[i] {
			a.report.ToolsExecuted++
		}
		a.report.RecentTools = append(a.report.RecentTools, ToolEvidence{CallID: call.ID, Name: call.Name, Status: status})
		if len(a.report.RecentTools) > 24 {
			a.report.RecentTools = a.report.RecentTools[len(a.report.RecentTools)-24:]
		}
		if !json.Valid(call.Arguments) || call.InputError != "" {
			diagnostics = append(diagnostics, content)
		} else {
			history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: content, ToolFailed: &failed, ToolStatus: status})
		}

		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: content, Failed: failed, Status: status, Duration: results[i].duration})
		}
	}
	for _, diagnostic := range diagnostics {
		history = a.nudge(history, "tool_input", diagnostic)
	}
	for _, instructions := range newInstructions {
		history = a.nudge(history, "instructions", instructions)
	}
	if !yielded {
		st.trackSignature(signature.String())
	}
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

func toolResultStatus(call llm.ToolCall, result callResult, finishReason string, executed, withheld bool) string {
	failed := result.err != nil
	status := "success"
	if withheld {
		status = "skipped_for_input"
	}
	if failed {
		status = "execution_error"
		if !executed {
			status = "rejected"
		}
		if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
			status = "cancelled"
		}
		if strings.HasPrefix(result.err.Error(), "invalid_arguments:") {
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
	}

	return status
}
