package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

// A small envelope works with strict remote schemas and local GBNF alike.
// The contained recipe is checked against the registry before it can be applied.
const workflowProposalSchema = `{"type":"object","properties":{"recipe_json":{"type":"string"},"note":{"type":"string"}},"required":["recipe_json","note"],"additionalProperties":false}`
const workflowProposalGrammar = `root ::= "{" ws "\"recipe_json\"" ws ":" ws string ws "," ws "\"note\"" ws ":" ws string ws "}"
string ::= "\"" ([^"\\\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
ws ::= [ \t\n\r]*`

type workflowProposal struct {
	before, source, note string
	err                  error
}

func (m *Model) propose(prompt string) tea.Cmd {
	if m.ctx.Err() != nil || m.busy {
		return nil
	}
	client := m.config.Client
	if client == nil {
		m.setNotice(pane.NoticeWarning, "Connect a model to request an AI proposal")
		return nil
	}
	s, err := m.service(nil)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return nil
	}
	schema := string(s.DefinitionSchema())
	contracts, err := json.Marshal(m.registry.Contracts())
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return nil
	}
	before := m.editor.Source()
	m.setNotice(pane.NoticeInfo, "AI drafting a proposal · Esc cancels")
	output := 8192
	if m.config.MaxTokens > 0 {
		output = min(output, m.config.MaxTokens)
	}
	request := llm.ChatRequest{
		MaxTokens:   output,
		Temperature: m.config.Temperature,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: prompts.WorkflowEdit + "\n\n" + workspace.WorkflowGuide},
			{Role: llm.RoleUser, Content: "Definition schema:\n" + schema + "\nOperation contracts:\n" + string(contracts) + "\nCurrent draft:\n" + before + "\nRequested change:\n" + prompt},
		},
		JSONSchema: workflowProposalSchema,
		Grammar:    workflowProposalGrammar,
	}
	limit := m.config.ContextLimit
	return m.startOperation(func(operationCtx context.Context) tea.Msg {
		ctx, cancel := context.WithTimeout(operationCtx, 2*time.Minute)
		defer cancel()

		if limit > 0 && llm.EstimatePromptTokens(request)+output > limit {
			return workflowProposal{before: before, err: fmt.Errorf("proposal exceeds the model context limit")}
		}
		response, requestErr := client.Chat(ctx, request)
		var envelope struct {
			Recipe string `json:"recipe_json"`
			Note   string `json:"note"`
		}
		if requestErr == nil {
			requestErr = json.Unmarshal([]byte(response.Message.Content), &envelope)
		}
		if requestErr == nil && (response.FinishReason == "length" || len(response.Message.ToolCalls) > 0) {
			requestErr = fmt.Errorf("model returned incomplete output or an unexpected tool call")
		}
		if len(envelope.Recipe) > 64<<10 || len(envelope.Note) > 4096 {
			requestErr = fmt.Errorf("proposal exceeds 64 KiB")
		}
		return workflowProposal{before: before, source: envelope.Recipe, note: envelope.Note, err: requestErr}
	})
}
func (m *Model) acceptProposal(p workflowProposal) {
	m.busy = false
	m.cancel = nil
	if p.err != nil {
		m.setNotice(pane.NoticeError, p.err.Error())
		return
	}
	d, err := Decode([]byte(p.source))
	if err == nil {
		s, e := m.service(nil)
		err = e
		if err == nil {
			err = workflow.ValidateJSON([]byte(p.source), s.DefinitionSchema())
		}
	}
	if err != nil {
		m.dialogs.PresentDialog(screen.Dialog{Title: "Invalid AI proposal", Lines: []string{err.Error(), p.source}})
		return
	}
	lines := []string{"Proposal only · no files saved or operations executed.", ""}
	if p.note != "" {
		lines = append(lines, p.note, "")
	}
	old := map[string]string{}
	for _, step := range m.editor.Definition.Steps {
		b, _ := json.MarshalIndent(step, "", "  ")
		old[step.ID] = string(b)
	}
	for _, step := range d.Steps {
		b, _ := json.MarshalIndent(step, "", "  ")
		next := string(b)
		prev, ok := old[step.ID]
		if !ok {
			lines = append(lines, "+ Add "+step.ID, next)
		} else if prev != next {
			lines = append(lines, "~ Change "+step.ID, "Before:", prev, "After:", next)
		}
		delete(old, step.ID)
	}
	for id := range old {
		lines = append(lines, "- Remove "+id)
	}
	lines = append(lines, "", "Complete proposed recipe (including inputs/outputs):", p.source)
	// Split multiline sections so the shared scrollbar measures actual rows.
	m.dialogs.PresentDialog(screen.Dialog{Title: "Review AI proposal", Lines: strings.Split(strings.Join(lines, "\n"), "\n"),
		Shortcuts: []popup.Shortcut{{Key: "Esc/Enter", Action: "discard"}, {Key: "S", Action: "apply to draft"}, {Key: "↑↓", Action: "scroll"}},

		Save: true,
	})
	m.proposalDialog = m.dialogs.Dialog
	m.proposalDraft = d
	m.proposalBefore = p.before

}

func (m *Model) applyProposal() error {
	d := m.proposalDraft

	if m.busy || m.run != nil || m.editor.Source() != m.proposalBefore {
		return fmt.Errorf("draft changed since the request; ask for a fresh proposal")
	}
	m.editor.Replace(d)
	m.graph = m.buildGraph(m.graph)
	m.setNotice(pane.NoticeSuccess, "Proposal applied to draft · Ctrl+S saves")
	return nil
}
