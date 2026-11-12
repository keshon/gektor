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
	mutatingBefore := st.mutatingSucceeded
	var pictures []string
	var signature strings.Builder
	var diagnostics []string
	for i := range batch.entries {
		entry := &batch.entries[i]
		call := entry.call
		if entry.err == nil && !entry.withheld {
			if at := a.cfg.Tools.AttacherOf(call.Name); at != nil {
				paths, err := at.Attach(call.Arguments)
				switch {
				case err != nil:
					entry.err = fmt.Errorf("attach picture: %w", err)
				case len(paths) > 0 && len(pictures)+len(paths) > maxToolImagesPerStep:
					entry.err = fmt.Errorf("at most %d pictures can be attached in one step; this picture was not attached — re-read it in a later step", maxToolImagesPerStep)
				default:
					pictures = append(pictures, paths...)
				}
			}
		}

		content := entry.content
		if entry.err != nil {
			if strings.TrimSpace(content) != "" {
				content += "\n"
			}
			content += "error: " + entry.err.Error()
		} else if !entry.withheld {
			if !st.observing {
				// Embedders may omit the workspace observer.
				if containsStr(a.cfg.MutatingTools, call.Name) {
					st.mutatingSucceeded++
					st.addPaths(mutatedPathsFromCall(call.Arguments))
				}
			}
		}

		// A failed delegate may have changed files before it stopped.
		if entry.executed && !entry.withheld && !st.observing && call.Name == "delegate_task" {
			mutations, paths := parseDelegateMutations(content)
			st.mutatingSucceeded += mutations
			st.addPaths(paths)
		}
		if entry.withheld {
			content = "Not executed because new user input arrived. Reconsider this action using the updated instructions."
		}
		if a.cfg.Outputs != nil {
			content = a.cfg.Outputs.Limit(content, max(1, 48*1024/len(batch.entries)))
		} else {
			content = TruncateMiddle(content, max(1, 48*1024/len(batch.entries)))
		}

		failed := entry.err != nil
		if !entry.withheld && (a.cfg.Tools.ModeOf(call.Name) == Concurrent || failed) {
			a.noteObservation(st, call.Name, call.Arguments, content)
		}
		fmt.Fprintf(&signature, "%s\x00%s\x00", callKey(call.Name, call.Arguments), content)
		status := toolResultStatus(*entry, finishReason)
		if entry.executed && !entry.withheld {
			a.report.ToolsExecuted++
		}
		a.report.RecentTools = append(a.report.RecentTools, ToolEvidence{CallID: call.ID, Name: call.Name, Status: status})
		if len(a.report.RecentTools) > 24 {
			a.report.RecentTools = a.report.RecentTools[len(a.report.RecentTools)-24:]
		}

		if !json.Valid(call.Arguments) || call.InputError != "" {
			diagnostics = append(diagnostics, content)
		} else {
			message := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: content, ToolFailed: &failed, ToolStatus: status}
			if entry.executed && !failed && a.cfg.Catalog != nil && (call.Name == "enable_capability" || call.Name == "enable_browser") {
				for _, state := range a.cfg.Catalog.Save() {
					if content == CapabilityCatalog(state) {
						message.Harness = "capability_catalog:" + state.Fingerprint
					}
				}
			}
			history = append(history, message)
		}

		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: content, Failed: failed, Status: status, Duration: entry.duration})
		}
	}

	for _, diagnostic := range diagnostics {
		history = a.nudge(history, "tool_input", diagnostic)
	}
	for _, instructions := range newInstructions {
		history = a.nudge(history, "instructions", instructions)
	}
	if !batch.yielded {
		st.trackSignature(signature.String())
	}
	if len(pictures) > 0 {
		// The tool result is already in history; the pictures ride the
		// next user turn because role:tool has no image parts on the
		// OpenAI wire format. The note says where they came from, so a
		// model does not mistake harness output for fresh user input.
		history = append(history, llm.Message{
			Role:    llm.RoleUser,
			Harness: "tool_images",
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

func toolResultStatus(entry toolExecution, finishReason string) string {
	call := entry.call
	failed := entry.err != nil
	status := "success"
	if entry.withheld {
		status = "skipped_for_input"
	}
	if failed {
		status = "execution_error"
		if !entry.executed {
			status = "rejected"
		}
		if errors.Is(entry.err, context.Canceled) || errors.Is(entry.err, context.DeadlineExceeded) {
			status = "cancelled"
		}
		if strings.HasPrefix(entry.err.Error(), "invalid_arguments:") {
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
