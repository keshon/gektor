package main

import (
	"fmt"
	"strings"
)

// Measurements come only from the harness header, never the model answer.
type delegateMetrics struct {
	Termination      string   `json:"termination"`
	Steps            int      `json:"steps"`
	MaxSteps         int      `json:"max_steps"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	CachedTokens     int      `json:"cached_tokens"`
	Estimated        bool     `json:"estimated"`
	ToolResults      []string `json:"tool_results,omitempty"`
}

func parseDelegateMetrics(output string) (delegateMetrics, bool) {
	var m delegateMetrics
	if !strings.HasPrefix(output, "DELEGATE\n") {
		return m, false
	}
	header, _, _ := strings.Cut(output, "\n----\n")
	for _, line := range strings.Split(header, "\n") {
		switch {
		case strings.HasPrefix(line, "termination: "):
			m.Termination = strings.TrimPrefix(line, "termination: ")
		case strings.HasPrefix(line, "steps: "):
			fmt.Sscanf(line, "steps: %d/%d", &m.Steps, &m.MaxSteps)
		case strings.HasPrefix(line, "elapsed_ms: "):
			fmt.Sscanf(line, "elapsed_ms: %d", &m.ElapsedMS)
		case strings.HasPrefix(line, "tokens: "):
			fmt.Sscanf(line, "tokens: prompt=%d completion=%d cached=%d estimated=%t", &m.PromptTokens, &m.CompletionTokens, &m.CachedTokens, &m.Estimated)
		case strings.HasPrefix(line, "tool_result: "):
			m.ToolResults = append(m.ToolResults, strings.TrimPrefix(line, "tool_result: "))
		}
	}
	return m, m.Termination != ""
}
