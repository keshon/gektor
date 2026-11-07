package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/workspace"
	"strings"
	"time"
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

func (m *model) proposeWorkflow(prompt string) tea.Cmd {
	w := m.workflows
	if w.busy {
		return nil
	}
	client := m.baseEnv.Client
	if client == nil {
		w.setNotice(pane.NoticeWarning, "Connect a model to request an AI proposal")
		return nil
	}
	s, err := m.workflowService(nil)
	if err != nil {
		w.setNotice(pane.NoticeError, err.Error())
		return nil
	}
	schema := string(s.DefinitionSchema())
	contracts, _ := json.Marshal(w.registry.Contracts())
	before := w.editor.Source()
	w.busy = true
	w.setNotice(pane.NoticeInfo, "AI drafting a proposal · Esc cancels")
	ctx, cancel := context.WithTimeout(m.base, 2*time.Minute)
	w.cancel = cancel
	output := 8192
	if m.baseEnv.MaxTokens > 0 {
		output = min(output, m.baseEnv.MaxTokens)
	}
	request := llm.ChatRequest{MaxTokens: output, Temperature: m.baseEnv.Temperature, Messages: []llm.Message{{Role: llm.RoleSystem, Content: prompts.WorkflowEdit + "\n\n" + workspace.WorkflowGuide}, {Role: llm.RoleUser, Content: "Definition schema:\n" + schema + "\nOperation contracts:\n" + string(contracts) + "\nCurrent draft:\n" + before + "\nRequested change:\n" + prompt}}, JSONSchema: workflowProposalSchema, Grammar: workflowProposalGrammar}
	limit := m.baseEnv.ContextLimit
	return func() tea.Msg {
		defer cancel()
		if limit > 0 && llm.EstimatePromptTokens(request)+output > limit {
			return workflowProposal{before: before, err: fmt.Errorf("proposal exceeds the model context limit")}
		}
		response, err := client.Chat(ctx, request)
		var envelope struct {
			Recipe string `json:"recipe_json"`
			Note   string `json:"note"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(response.Message.Content), &envelope)
		}
		if err == nil && (response.FinishReason == "length" || len(response.Message.ToolCalls) > 0) {
			err = fmt.Errorf("model returned incomplete output or an unexpected tool call")
		}
		if len(envelope.Recipe) > 64<<10 || len(envelope.Note) > 4096 {
			err = fmt.Errorf("proposal exceeds 64 KiB")
		}
		return workflowProposal{before: before, source: envelope.Recipe, note: envelope.Note, err: err}
	}

}
func (m *model) acceptWorkflowProposal(p workflowProposal) {
	w := m.workflows
	w.busy = false
	w.cancel = nil
	if p.err != nil {
		w.setNotice(pane.NoticeError, p.err.Error())
		return
	}
	d, err := editor.Decode([]byte(p.source))
	if err == nil {
		s, e := m.workflowService(nil)
		err = e
		if err == nil {
			err = automation.ValidateJSON([]byte(p.source), s.DefinitionSchema())
		}
	}
	if err != nil {
		m.openDialog("Invalid AI proposal", []string{err.Error(), p.source})
		return
	}
	lines := []string{"Proposal only · no files saved or operations executed.", ""}
	if p.note != "" {
		lines = append(lines, p.note, "")
	}
	old := map[string]string{}
	for _, step := range w.editor.Definition.Steps {
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
	m.openDialog("Review AI proposal", strings.Split(strings.Join(lines, "\n"), "\n"))
	m.dialog.shortcuts = []popup.Shortcut{{Key: "Esc/Enter", Action: "discard"}, {Key: "S", Action: "apply to draft"}, {Key: "↑↓", Action: "scroll"}}
	m.dialog.save = func() error {
		if w.editor.Source() != p.before {
			return fmt.Errorf("draft changed since the request; ask for a fresh proposal")
		}
		w.editor.Replace(d)
		w.graph = m.workflowGraph(w.graph)
		w.setNotice(pane.NoticeSuccess, "Proposal applied to draft · Ctrl+S saves")
		return nil
	}
}
