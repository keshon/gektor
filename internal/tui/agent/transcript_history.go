package agent

import (
	"strings"
	"time"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// renderHistory rebuilds the transcript from a loaded snapshot so a
// resumed or compacted session reads like it was never left. System
// prompts stay out; loop nudges retain their [harness] provenance as
// system notices. Tool cards re-attach their results by
// call ID, same pairing the live path uses. No markers: the caller
// prepends its own (resumed/compacted), this renders conversation.
func renderHistory(history []llm.Message) []block {
	blocks, _ := renderHistoryAt(history)
	return blocks
}

// renderHistoryAt also maps source message indices to transcript block indices.
// The final boundary marks the end of the rendered history.
func renderHistoryAt(history []llm.Message) ([]block, []int) {
	var out []block
	boundary := make([]int, len(history)+1)

	for i, msg := range history {
		boundary[i] = len(out)

		switch msg.Role {
		case llm.RoleSystem:
			continue
		case llm.RoleUser:
			if strings.HasPrefix(msg.Content, "[harness] ") {
				out = append(out, contextBlock(msg))
				continue
			}

			if strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0 {
				continue
			}

			out = append(out, userBlock(displayInputContent(msg.Content)+imageSuffix(msg.Images)))
		case llm.RoleAssistant:
			out = append(out, assistantBlocks(msg.Content, msg.Reasoning)...)

			for _, call := range msg.ToolCalls {
				out = append(out, savedToolCard(call))
			}
		case llm.RoleTool:
			attached := attachSavedToolResult(out, msg)
			if !attached {
				out = append(out, resultBlock(msg.Content))
			}
		}
	}

	// Saved histories do not store message times; do not invent them on reopen.
	for i := range out {
		out[i].at = time.Time{}
	}

	boundary[len(history)] = len(out)

	return out, boundary
}

// historyMatchBlock locates visible text within a matched source message.
func historyMatchBlock(blocks []block, boundary []int, message int) int {
	message = min(max(message, 0), len(boundary)-1)
	prefix := boundary[message]
	if message >= len(boundary)-1 {
		return prefix
	}

	for i := prefix; i < boundary[message+1]; i++ {
		if blocks[i].role == roleUser || blocks[i].role == roleAnswer {
			return i
		}
	}

	return prefix
}

func (m *Model) appendSavedRun(snapshot conversation.Snapshot) {
	if len(snapshot.Runs) == 0 {
		return
	}

	last := snapshot.Runs[len(snapshot.Runs)-1]
	text := "Last run: " + last.Outcome
	if last.Error != "" {
		text += "\n" + last.Error
	}

	if last.Verification != "" {
		text += "\nVerification: " + last.Verification
	}

	if last.Outcome != "completed" {
		text += "\nReview the saved work, then send a follow-up to continue. Existing changes remain."
	}

	b := markerBlock(text)
	if last.Error != "" {
		b = errorBlock(text)
	}

	m.transcript.blocks = append(m.transcript.blocks, b)
}

func savedToolCard(call llm.ToolCall) block {
	args := string(call.Arguments)
	if call.InputError != "" {
		args = call.RawArguments
	}

	card := toolCardBlock(call.ID, call.Name+" "+args)
	if call.InputError != "" {
		card.open = false
		card.failed = true
		card.result = "not executed: " + call.InputError + "; original arguments preserved"
	}

	return card
}

func attachSavedToolResult(blocks []block, message llm.Message) bool {
	if message.ToolCallID == "" {
		return false
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		card := &blocks[i]
		if card.role != roleTool || !card.open || card.callID != message.ToolCallID {
			continue
		}

		card.result = message.Content
		card.toolStatus = message.ToolStatus
		card.open = false
		card.failed = strings.HasPrefix(message.Content, "error:") || strings.Contains(message.Content, "\nerror:")
		if message.ToolFailed != nil {
			card.failed = *message.ToolFailed
		}

		return true
	}

	return false
}
