package llm

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// PromptSection identifies application-owned system text. Reports never expose it.
type PromptSection struct{ Name, Text string }

type ContextCost struct {
	Name            string `json:"name"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type ContextReport struct {
	Request          int           `json:"request,omitempty"`
	EstimatedTokens  int           `json:"estimated_tokens"`
	Method           string        `json:"method"`
	Sections         []ContextCost `json:"sections"`
	Tools            []ContextCost `json:"tools"`
	BackendTokens    int           `json:"backend_prompt_tokens,omitempty"`
	CachedTokens     int           `json:"cached_tokens,omitempty"`
	BackendEstimated bool          `json:"backend_estimated,omitempty"`
	Notes            []string      `json:"notes,omitempty"`
}

// AnalyzeContext uses the same estimate as the context guard, not a tokenizer.
// Provider chat templates and tokenization can differ; usage is reported separately.
func AnalyzeContext(req ChatRequest, sections []PromptSection) ContextReport {
	r := ContextReport{EstimatedTokens: EstimatePromptTokens(req), Method: "characters/4 plus message and image estimates; backend template/tokenization excluded"}
	system := ""
	var history []Message
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			system += msg.Content
		} else {
			history = append(history, msg)
		}
	}
	add := func(name, text string) {
		r.Sections = append(r.Sections, ContextCost{name, len(text), utf8.RuneCountInString(text) / 4})
	}
	for _, section := range sections {
		if section.Text == "" {
			r.Sections = append(r.Sections, ContextCost{Name: section.Name})
			continue
		}
		if strings.Contains(system, section.Text) {
			add(section.Name, section.Text)
			system = strings.Replace(system, section.Text, "", 1)
		}
	}
	add("other_system", system)
	if len(history) > 0 {
		bytes := 0
		for _, msg := range history {
			bytes += len(msg.Content) + len(msg.ToolCallID)
			for _, call := range msg.ToolCalls {
				bytes += len(call.Name) + len(call.ID) + len(call.Arguments)
			}
		}
		r.Sections = append(r.Sections, ContextCost{Name: "history_and_task", Bytes: bytes, EstimatedTokens: EstimatePromptTokens(ChatRequest{Messages: history})})
	}
	if len(req.Tools) > 0 {
		data, _ := json.Marshal(req.Tools)
		add("tool_schemas", string(data))
	}
	for _, tool := range req.Tools {
		data, _ := json.Marshal(tool)
		r.Tools = append(r.Tools, ContextCost{tool.Name, len(data), utf8.RuneCount(data) / 4})
	}
	sort.Slice(r.Tools, func(i, j int) bool {
		if r.Tools[i].EstimatedTokens == r.Tools[j].EstimatedTokens {
			return r.Tools[i].Name < r.Tools[j].Name
		}
		return r.Tools[i].EstimatedTokens > r.Tools[j].EstimatedTokens
	})
	used := 0
	for _, section := range r.Sections {
		used += section.EstimatedTokens
	}
	r.Sections = append(r.Sections, ContextCost{Name: "framing_and_rounding", EstimatedTokens: r.EstimatedTokens - used})
	return r
}
