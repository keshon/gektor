// Package conversation owns durable conversation history and its compacted model view.
// A single snapshot stores each original message once; context is a retained tail
// plus a summary.
package conversation

import "github.com/keshon/gektor/internal/llm"

// Snapshot keeps the archive independent of the model's working context.
// ContextStart is an index into History; zero means the entire conversation.
type Snapshot struct {
	Version        int              `json:"version"`
	Workspace      string           `json:"workspace,omitempty"`
	History        []llm.Message    `json:"history"`
	ContextStart   int              `json:"context_start,omitempty"`
	Summary        string           `json:"summary,omitempty"`
	SummaryRequest int              `json:"summary_request,omitempty"`
	Runs           []Run            `json:"runs,omitempty"`
	Capabilities   []Capability     `json:"capabilities,omitempty"`
	Browser        *BrowserProfile  `json:"browser,omitempty"`
	Prompts        []PromptRevision `json:"prompt_revisions,omitempty"`
	LastRequest    *RequestManifest `json:"last_request,omitempty"`
}

// BrowserProfile references browser-managed data outside the conversation.
// It never contains cookies, passwords, or an arbitrary filesystem path.
type BrowserProfile struct {
	Channel string `json:"channel"`
	Profile string `json:"profile"`
}

// Capability preserves the catalog or an explicit unloaded selection on resume.
// Definitions are compared to the current adapter before any tool executes.
type Capability struct {
	Unloaded     bool          `json:"unloaded,omitempty"`
	ID           string        `json:"id"`
	Version      string        `json:"version"`
	Fingerprint  string        `json:"fingerprint"`
	Tools        []llm.ToolDef `json:"tools"`
	Instructions string        `json:"instructions,omitempty"`
}

// Run identifies one execution in the full archive, independent of compaction.
// Retries can overlap the preceding run so the original task stays in scope.
type Run struct {
	Start        int    `json:"start"`
	End          int    `json:"end"`
	Outcome      string `json:"outcome"`
	Steps        int    `json:"steps"`
	Verification string `json:"verification,omitempty"`
	Error        string `json:"error,omitempty"`
}

func New(history []llm.Message) Snapshot {
	return Snapshot{Version: 1, History: append([]llm.Message(nil), history...)}
}

func (s Snapshot) Context() []llm.Message {
	if s.ContextStart == 0 {
		return append([]llm.Message(nil), s.History...)
	}

	out := append([]llm.Message(nil), s.History[:2]...)
	out = append(out, llm.Message{
		Role:    llm.RoleUser,
		Content: s.Summary,
		Harness: "compact",
		Request: s.SummaryRequest,
	})

	return append(out, s.History[s.ContextStart:]...)
}

// Compact records the projection produced by the agent's compactor. Its tail
// is a suffix of the current context and therefore of the archive.
func (s *Snapshot) Compact(context []llm.Message) {
	s.ContextStart = len(s.History) - len(context[3:])
	s.Summary = context[2].Content
	s.SummaryRequest = context[2].Request
}
