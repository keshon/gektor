package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

type contextInspectionKey struct{}

type ContextTool struct {
	Name        string `json:"name"`
	RoleAllowed bool   `json:"role_allowed"`
}

type ContextSkill struct {
	ID        string `json:"id"`
	ArchiveID int    `json:"archive_id"`
}

// ContextInspection is captured before one model request. Tool batches share
// this immutable view, including when another call activates new tools.
type ContextInspection struct {
	Request   conversation.RequestManifest
	Prompts   []conversation.PromptRevision
	Tools     []ContextTool
	Skills    []ContextSkill
	Directive string
	Summary   string
}

// InspectionFrom returns request evidence, never a projection reconstructed
// from the saved archive. Callers must not modify the returned slices.
func InspectionFrom(ctx context.Context) (*ContextInspection, bool) {
	v, ok := ctx.Value(contextInspectionKey{}).(*ContextInspection)
	return v, ok && v != nil
}

func (a *Agent) recordInspection(req llm.ChatRequest) {
	v := &ContextInspection{}
	r := conversation.RequestManifest{Number: a.requests, ArchiveMessages: len(a.snapshot.History), Compacted: a.snapshot.ContextStart > 0, Included: []conversation.MessageRange{}}
	system := ""
	calls := map[string]llm.ToolCall{}
	seenSkills := map[string]bool{}
	for i, m := range req.Messages {
		projected := llm.ModelMessages(req.Messages[i : i+1])
		if len(projected) == 0 {
			continue
		}
		m = projected[0]
		if a.cfg.Directive != "" && m.Role == llm.RoleUser && m.Content == harnessPrefix+a.cfg.Directive {
			v.Directive = a.cfg.Directive
		}
		r.MessageCount++
		r.ImageCount += len(m.Images)
		if m.Role == llm.RoleSystem {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
		}
		id := i + 1
		if r.Compacted {
			if i == 2 {
				r.HasSummary, v.Summary = true, m.Content
				continue
			}
			if i >= 3 {
				id = a.snapshot.ContextStart + i - 2
			}
		}
		if n := len(r.Included); n > 0 && r.Included[n-1].End+1 == id {
			r.Included[n-1].End = id
		} else {
			r.Included = append(r.Included, conversation.MessageRange{Start: id, End: id})
		}
		if id <= len(a.snapshot.History) {
			original := a.snapshot.History[id-1]
			if m.Content != original.Content || !reflect.DeepEqual(m.ToolCalls, original.ToolCalls) || !reflect.DeepEqual(m.Images, original.Images) {
				r.Rewritten = append(r.Rewritten, id)
			}
		}
		for _, call := range m.ToolCalls {
			calls[call.ID] = call
		}
		if call, ok := calls[m.ToolCallID]; ok && call.Name == "load_skill" && m.Role == llm.RoleTool && m.ToolFailed != nil && !*m.ToolFailed {
			var input struct{ ID, Resource string }
			if json.Unmarshal(call.Arguments, &input) == nil && input.ID != "" && input.Resource == "" && !seenSkills[input.ID] {
				v.Skills = append(v.Skills, ContextSkill{input.ID, id})
				seenSkills[input.ID] = true
			}
		}
	}
	prompt := a.snapshot.RecordPrompt(system, a.cfg.SystemSections, a.requests)
	r.PromptRevision = prompt.ID
	for _, tool := range req.Tools {
		v.Tools = append(v.Tools, ContextTool{tool.Name, a.mayRun(tool.Name)})
	}
	slices.SortFunc(v.Tools, func(a, b ContextTool) int {
		return strings.Compare(a.Name, b.Name)
	})
	v.Request, v.Prompts = r, append([]conversation.PromptRevision(nil), a.snapshot.Prompts...)
	a.snapshot.LastRequest, a.inspection = &r, v
}
