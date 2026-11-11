package agent

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// LoadState reads a history previously written via Config.StateFile.
func LoadState(path string) ([]llm.Message, error) {
	return conversation.ReadContext(path)
}

// SaveState writes a checkpoint atomically without running an agent.
func SaveState(path string, history []llm.Message) error {
	return conversation.WriteSnapshot(path, history)
}

// saveState always updates runtime history. Disk persistence is best-effort.
func (a *Agent) saveState(history []llm.Message) {
	a.captureHistory(history)
	a.saveSnapshot()
}

func (a *Agent) saveSnapshot() {
	if a.cfg.StateFile == "" {
		return
	}
	if err := a.persistState(); err != nil {
		warning := "session persistence failed: " + err.Error()
		if !containsStr(a.report.Warnings, warning) {
			a.report.Warnings = append(a.report.Warnings, warning)
			if a.cfg.OnLocal != nil {
				a.cfg.OnLocal(warning)
			}
		}
	}
}

func (a *Agent) persistState() error {
	a.snapshot.Workspace = a.cfg.Workspace
	if a.cfg.Catalog != nil {
		a.snapshot.Capabilities = a.cfg.Catalog.Save()
	}
	if a.cfg.Browser != nil {
		a.snapshot.Browser = a.cfg.Browser.Reference()
	}
	return a.snapshot.Save(a.cfg.StateFile)
}

func (a *Agent) restoreHistory(history []llm.Message) ([]llm.Message, error) {
	a.snapshot = conversation.New(history)
	if a.cfg.StateFile != "" {
		s, err := conversation.Load(a.cfg.StateFile)
		if err == nil && (sameHistory(history, s.History) || sameHistory(history, s.Context())) {
			a.snapshot = s
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	history = a.snapshot.Context()
	if a.snapshot.LastRequest != nil {
		a.requests = max(a.requests, a.snapshot.LastRequest.Number)
	}
	if a.snapshot.ContextStart > 0 {
		var err error
		history, err = retainArchivedInstructions(a.snapshot, history)
		if err != nil {
			return nil, err
		}
		a.snapshot.Summary = history[2].Content
		a.snapshot.SummaryRequest = history[2].Request
	}
	for _, msg := range history {
		a.requests = max(a.requests, msg.Request)
	}
	a.checkpointLen = len(history)
	return history, nil
}

func sameHistory(a, b []llm.Message) bool {
	if len(a) > 0 && len(b) > 0 && a[0].Role == llm.RoleSystem && b[0].Role == llm.RoleSystem {
		return reflect.DeepEqual(a[1:], b[1:])
	}
	return reflect.DeepEqual(a, b)
}

func (a *Agent) captureHistory(history []llm.Message) {
	if a.snapshot.Version == 0 {
		a.snapshot = conversation.New(history)
		a.checkpointLen = len(history)
		return
	}
	if len(history) > a.checkpointLen {
		a.snapshot.History = append(a.snapshot.History, history[a.checkpointLen:]...)
	}
	if len(history) > 0 && len(a.snapshot.History) > 0 {
		if old := a.snapshot.History[0]; old.Role == llm.RoleSystem && old.Content != history[0].Content && len(a.snapshot.Prompts) == 0 {
			a.snapshot.RecordPrompt(old.Content, nil, 0)
		}
		a.snapshot.History[0] = history[0]
	}
	for i, msg := range history {
		if msg.Request == 0 {
			continue
		}
		index := i
		if a.snapshot.ContextStart > 0 && i >= 3 {
			index = a.snapshot.ContextStart + i - 3
		}
		if a.snapshot.ContextStart > 0 && i == 2 {
			a.snapshot.SummaryRequest = msg.Request
			continue
		}
		if index < len(a.snapshot.History) {
			a.snapshot.History[index].Harness = msg.Harness
			a.snapshot.History[index].Request = msg.Request
		}
	}
	a.checkpointLen = len(history)
}

func (a *Agent) compactContext(previous, next []llm.Message) {
	a.captureHistory(previous)
	a.snapshot.Compact(next)
	a.checkpointLen = len(next)
}

// Never replay a tool whose completion is unknown: it may already have changed
// the workspace. Close only the interrupted trailing group with explicit results.
func (a *Agent) closePendingCalls(history []llm.Message, answerID, answer string) []llm.Message {
	start := len(history) - 1
	for start >= 0 && history[start].Role == llm.RoleTool {
		start--
	}
	if start < 0 || history[start].Role != llm.RoleAssistant {
		return history
	}
	answered := map[string]bool{}
	var rejected []string
	for _, msg := range history[start+1:] {
		answered[msg.ToolCallID] = true
	}
	for _, call := range history[start].ToolCalls {
		if call.InputError != "" {
			rejected = append(rejected, fmt.Sprintf("Call %s (%s) was rejected before execution: %s. Arguments were supplied and retained as text. Reissue valid complete arguments. Original input excerpt:\n%s", call.ID, call.Name, call.InputError, TruncateMiddle(call.RawArguments, 1800)))
			continue
		}
		if answered[call.ID] {
			continue
		}
		failed := true
		result := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, ToolFailed: &failed, Content: "error: execution interrupted; completion is unknown. Inspect the workspace before retrying this action."}
		if call.ID == answerID {
			result.Content = answer
			failed = false
		}
		history = append(history, result)
		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: result.Content, Failed: failed})
		}
	}
	for _, diagnostic := range rejected {
		history = a.nudge(history, "tool_input", diagnostic)
	}
	return history
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (a *Agent) resumeHistory(history []llm.Message) []llm.Message {
	history = append([]llm.Message(nil), history...)
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		history[0].Content = a.cfg.System
	}
	return history
}

// rehydrateTodo restores the checklist from history into the fresh
// registry. The checklist tool takes the full list per call, so
// replaying the LAST such call's args restores exact state through the
// normal Run path. Best-effort: stale args fail validation and the
// tracker stays empty, exactly as before.
func (a *Agent) rehydrateTodo(ctx context.Context, history []llm.Message) {
	cl := a.cfg.Tools.Checklist()
	if cl == nil {
		return
	}
	for i := len(history) - 1; i >= 0; i-- {
		calls := history[i].ToolCalls
		for j := len(calls) - 1; j >= 0; j-- {
			if calls[j].Name != cl.Name() {
				continue
			}
			_, _ = a.cfg.Tools.Run(ctx, cl.Name(), calls[j].Arguments)
			return
		}
	}
}
