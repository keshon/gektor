package retrospective

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

type submitTool struct {
	archive    *archiveTool
	start, end int
	result     *Result
}

func (*submitTool) Name() string { return "submit_evaluation" }

func (*submitTool) Description() string {
	return "Submit the review. Findings need archive message IDs actually read in the selected scope. Optional memory (at most 1 observation) additionally needs an exact quote of at least 8 characters from a read, explicitly successful tool result. No reusable evidence means memory=[]; do not invent an entry."
}

func (*submitTool) Mode() agent.ToolMode { return agent.Exclusive }

func (*submitTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"},"findings":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"evidence":{"type":"array","items":{"type":"integer"}}},"required":["text","evidence"],"additionalProperties":false}},"memory":{"type":"array","maxItems":1,"items":{"type":"object","properties":{"when":{"type":"string"},"lesson":{"type":"string"},"evidence":{"type":"array","items":{"type":"integer"}},"quote":{"type":"string"}},"required":["when","lesson","evidence","quote"],"additionalProperties":false}}},"required":["summary","findings","memory"],"additionalProperties":false}`)
}

func (t *submitTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Summary  string    `json:"summary"`
		Findings []Finding `json:"findings"`
		Memory   []Memory  `json:"memory"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("expected one JSON object")
	}
	result := Result{Summary: input.Summary, Findings: input.Findings, Memory: input.Memory}
	if strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 4000 || len(result.Findings) > 8 || len(result.Memory) > 1 {
		return "", fmt.Errorf("keep summary under 4000 bytes, at most 8 findings and 1 memory")
	}
	validate := func(ids []int) error {
		if len(ids) == 0 {
			return fmt.Errorf("cite at least one message")
		}
		for _, id := range ids {
			if id <= t.start || id > t.end {
				return fmt.Errorf("message %d is outside the selected scope", id)
			}
			if _, ok := t.archive.seen[id]; !ok {
				return fmt.Errorf("read message %d before citing it", id)
			}
		}
		return nil
	}
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding.Text) == "" || len(finding.Text) > 2000 {
			return "", fmt.Errorf("finding must contain 1–2000 bytes")
		}
		if err := validate(finding.Evidence); err != nil {
			return "", err
		}
	}
	for _, memory := range result.Memory {
		if strings.TrimSpace(memory.When) == "" || strings.TrimSpace(memory.Lesson) == "" || len(memory.When) > 300 || len(memory.Lesson) > 1000 || len([]rune(strings.TrimSpace(memory.Quote))) < 8 || len(memory.Quote) > 600 {
			return "", fmt.Errorf("memory needs a concise condition, lesson and literal evidence quote")
		}
		if err := validate(memory.Evidence); err != nil {
			return "", err
		}
		confirmed := false
		for _, id := range memory.Evidence {
			m := t.archive.snapshot.History[id-1]
			if m.Role == llm.RoleTool && m.ToolFailed != nil && !*m.ToolFailed && strings.Contains(t.archive.seen[id], memory.Quote) && strings.Contains(m.Content, memory.Quote) {
				confirmed = true
			}
		}
		if !confirmed {
			return "", fmt.Errorf("memory quote needs a read, explicitly successful tool result; keep unsupported ideas in findings instead")
		}
	}
	t.result = &result
	return "Evaluation recorded. Finish briefly; do not continue the original task.", nil
}
