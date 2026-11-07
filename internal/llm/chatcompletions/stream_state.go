package chatcompletions

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
)

type deltaCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamChunk struct {
	PromptProgress *struct {
		Total     int  `json:"total"`
		Cache     *int `json:"cache"`
		Processed int  `json:"processed"`
	} `json:"prompt_progress"`
	Timings *struct {
		Prompt    int  `json:"prompt_n"`
		Cached    *int `json:"cache_n"`
		Predicted *int `json:"predicted_n"`
	} `json:"timings"`
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Delta struct {
			Role      string      `json:"role"`
			Content   string      `json:"content"`
			Reasoning string      `json:"reasoning_content"`
			ToolCalls []deltaCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
}

// streamAccumulator owns one response's deltas and usage estimates. It does not
// read the connection or execute tool calls.
type streamAccumulator struct {
	content, reasoning                           strings.Builder
	finish                                       string
	complete                                     bool
	calls                                        map[int]*deltaCall
	promptTokens, completionTokens, cachedTokens int
	progress                                     llm.Progress
	promptKnown, generationKnown                 bool
	generatedRunes                               int
}

func (s *streamAccumulator) consume(chunk streamChunk, onProgress func(llm.Progress), onReasoning, onContent func(string)) {
	if chunk.Usage != nil {
		s.promptTokens = chunk.Usage.PromptTokens
		s.completionTokens = chunk.Usage.CompletionTokens
		s.cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
	}
	if p := chunk.PromptProgress; p != nil && p.Total > 0 {
		s.progress.Phase, s.progress.PromptTokens = "prefill", p.Total
		s.progress.ProcessedTokens = p.Processed
		s.progress.CacheKnown = p.Cache != nil
		s.progress.CachedTokens = 0
		if p.Cache != nil {
			s.progress.CachedTokens = *p.Cache
		}
		s.promptKnown = true
	}
	previousRunes := s.generatedRunes
	for _, ch := range chunk.Choices {
		if ch.FinishReason != "" {
			s.finish = ch.FinishReason
			s.complete = true
		}
		if ch.Delta.Content != "" {
			s.progress.Phase = "generating"
			s.generatedRunes += utf8.RuneCountInString(ch.Delta.Content)
			s.content.WriteString(ch.Delta.Content)
			if onContent != nil {
				onContent(ch.Delta.Content)
			}
		}
		// Reasoning is reported as it arrives, separately from content, so a
		// reader watches deliberation finish before the answer begins.
		// It used to accumulate silently and appear only once the whole
		// reply was done.
		if ch.Delta.Reasoning != "" {
			s.progress.Phase = "reasoning"
			s.generatedRunes += utf8.RuneCountInString(ch.Delta.Reasoning)
			s.reasoning.WriteString(ch.Delta.Reasoning)
			if onReasoning != nil {
				onReasoning(ch.Delta.Reasoning)
			}
		}
		for _, dc := range ch.Delta.ToolCalls {
			s.progress.Phase = "tool_call"
			s.generatedRunes += utf8.RuneCountInString(dc.Function.Name) + utf8.RuneCountInString(dc.Function.Arguments)
			a := s.calls[dc.Index]
			if a == nil {
				cp := dc
				a = &cp
				s.calls[dc.Index] = a
			} else {
				a.Function.Arguments += dc.Function.Arguments
				if dc.ID != "" {
					a.ID = dc.ID
				}
				if dc.Function.Name != "" {
					a.Function.Name = dc.Function.Name
				}
			}
		}
	}
	if s.generatedRunes != previousRunes {
		s.progress.CompletionTokens = (s.generatedRunes + 3) / 4
		s.generationKnown = false
	}
	if chunk.Timings != nil {
		// During prefill prompt_n is only the work completed so far.
		// Keep prompt_progress.total authoritative; infer a total from
		// timings only after generation starts and no total was supplied.
		if cached := chunk.Timings.Cached; !s.promptKnown && s.generatedRunes > 0 && cached != nil && chunk.Timings.Prompt+*cached > 0 {
			s.progress.PromptTokens, s.promptKnown = chunk.Timings.Prompt+*cached, true
			s.progress.CachedTokens = *cached
		}
		if generated := chunk.Timings.Predicted; generated != nil {
			s.progress.CompletionTokens, s.generationKnown = *generated, true
		}
	}
	if chunk.Usage != nil && chunk.Usage.PromptTokens > 0 {
		s.progress.PromptTokens, s.promptKnown = chunk.Usage.PromptTokens, true
		s.progress.CompletionTokens = chunk.Usage.CompletionTokens
		s.progress.CachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		s.generationKnown = true
	}
	s.progress.Estimated = !s.promptKnown || !s.generationKnown
	if onProgress != nil {
		onProgress(s.progress)
	}
}

func (s *streamAccumulator) message() llm.Message {
	msg := llm.Message{Role: llm.RoleAssistant, Content: s.content.String(), Reasoning: s.reasoning.String()}
	for i := 0; i < len(s.calls); i++ {
		dc, ok := s.calls[i]
		if !ok {
			continue
		}
		msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
			ID:        dc.ID,
			Name:      dc.Function.Name,
			Arguments: llm.NormalizeArguments(json.RawMessage(dc.Function.Arguments)),
		})
	}
	return msg
}

func (s *streamAccumulator) usage(req llm.ChatRequest) llm.Usage {
	// A backend may omit the requested usage block. Fall back to an estimate
	// from the wire, marked Estimated (displays show "~").
	//
	// Count what goes on the wire and nothing else. Reasoning is decoded
	// for display and never encoded back, and tool-call arguments are
	// routinely larger than the text around them, so counting one and
	// skipping the other inflated the prompt on every step of a reasoning
	// run. Every budget downstream trusts this number: compaction fired
	// early and the response allowance shrank for no reason.
	estimated := false
	if s.promptTokens == 0 {
		if s.promptKnown {
			s.promptTokens, s.cachedTokens = s.progress.PromptTokens, s.progress.CachedTokens
		} else {
			estimated = true
			s.promptTokens = llm.EstimatePromptTokens(req)
		}
	}
	if s.completionTokens == 0 {
		if s.generationKnown {
			s.completionTokens = s.progress.CompletionTokens
		} else {
			estimated = true
			s.completionTokens = (s.generatedRunes + 3) / 4
		}
	}
	return llm.Usage{PromptTokens: s.promptTokens, CompletionTokens: s.completionTokens, CachedTokens: s.cachedTokens, Estimated: estimated}
}
