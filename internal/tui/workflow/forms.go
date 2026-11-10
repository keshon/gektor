package workflow

import (
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/tui/screen"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"

	"github.com/keshon/gektor/internal/workflow"
)

func (m *Model) setupForm() tea.Cmd {
	w := m
	if w.run != nil {
		return nil
	}
	m.dialogs.OpenForm(screen.Form{
		Title:      "Edit workflow",
		Hint:       "Choose what to change. AI proposals are reviewed before applying.",
		ApplyLabel: "Open",
		Fields:     []formField{{Key: "section", Label: "Section", Value: "Selected step arguments"}},
		Apply: func(v map[string]string) error {
			actions := map[string]string{"Selected step arguments": "edit", "AI change": "ai", "Name": "name", "Inputs": "inputs", "Outputs": "outputs", "Complete JSON": "recipe"}
			action, ok := actions[v["section"]]
			if !ok {
				return fmt.Errorf("choose a section")
			}
			m.queueAction(action, "")
			return nil
		},
		Choices: map[string][]string{"section": {"Selected step arguments", "AI change", "Name", "Inputs", "Outputs", "Complete JSON"}},
	})

	return nil
}

func (m *Model) addNodeForm() tea.Cmd {
	w := m
	names := append(w.registry.Names(), "Schedule trigger")
	m.dialogs.OpenForm(screen.Form{
		Title:      "Add node",
		Hint:       "Choose an operation or a Schedule trigger.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "type", Label: "Node type", Value: names[0]}},
		Apply: func(v map[string]string) error {
			if v["type"] == "Schedule trigger" {
				m.queueAction("schedule-add", "")
				return nil
			}
			if _, err := w.registry.Node(v["type"]); err != nil {
				return err
			}
			m.queueAction("add-operation", v["type"])
			return nil
		},
		Choices: map[string][]string{"type": names},
	})

	return nil
}

func (m *Model) behaviorForm() tea.Cmd {
	w := m
	for i, step := range w.editor.Definition.Steps {
		if step.ID != w.graph.Selected {
			continue
		}
		original, _ := json.Marshal(step)
		schema := schemaform.Parse(json.RawMessage(`{"properties":{"review":{"type":"boolean","enum":[true,false]},"depends_on":{"type":"array"},"when":{},"for_each":{},"merge":{"type":"string","enum":["all","any"]},"retry":{"type":"object"}}}`))
		m.dialogs.OpenForm(screen.Form{
			Title:      step.ID + " / behavior",
			Hint:       "Conditions and references use JSON. Retries require a retry-safe operation.",
			ApplyLabel: "Apply",
			Fields:     schema.Fields(original),
			Apply: func(v map[string]string) error {
				raw, err := schema.Values(original, v)
				if err != nil {
					return err
				}
				var changed workflow.Step
				if err := json.Unmarshal(raw, &changed); err != nil {
					return err
				}
				d := w.editor.Clone()
				d.Steps[i] = changed
				w.editor.Replace(d)
				w.graph = m.buildGraph(w.graph)
				return nil
			},
			Choices: schema.Choices(),
		})

		break
	}
	return nil
}

func (m *Model) proposalForm() tea.Cmd {
	m.dialogs.OpenForm(screen.Form{
		Title:      "AI workflow proposal",
		Hint:       "The model proposes recipe edits only. You review before applying.",
		ApplyLabel: "Propose",
		Fields:     []formField{{Key: "prompt", Label: "Describe the change", Multiline: true}},
		Apply: func(v map[string]string) error {
			if strings.TrimSpace(v["prompt"]) == "" {
				return fmt.Errorf("describe a change")
			}
			m.queueAction("ai-prompt", v["prompt"])
			return nil
		},
	})
	return nil
}

func (m *Model) sourceForm() tea.Cmd {
	w := m
	m.dialogs.OpenForm(screen.Form{
		Title:      "Recipe JSON",
		Hint:       "Inputs, outputs, conditions, iteration and retry are preserved here.",
		ApplyLabel: "Apply",
		Fields:     []formField{{Key: "source", Label: "Recipe", Value: w.editor.Source(), Multiline: true}},
		Apply: func(v map[string]string) error {
			d, err := Decode([]byte(v["source"]))
			if err != nil {
				return err
			}
			w.editor.Replace(d)
			w.graph = m.buildGraph(w.graph)
			return nil
		},
	})
	return nil
}

func (m *Model) connectForm() tea.Cmd {
	w := m
	names := []string{}
	for _, s := range w.editor.Definition.Steps {
		names = append(names, s.ID)
	}
	if len(names) < 2 {
		w.setNotice(pane.NoticeWarning, "Add at least two nodes first")
		return nil
	}
	destination := names[0]
	if destination == w.graph.Selected {
		destination = names[1]
	}
	m.dialogs.OpenForm(screen.Form{
		Title:      "Connect nodes",
		Hint:       "Choose the source and destination, then map their data.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "from", Label: "Source node", Value: w.graph.Selected}, {Key: "to", Label: "Destination node", Value: destination}},
		Apply: func(v map[string]string) error {
			if v["from"] == v["to"] {
				return fmt.Errorf("choose different nodes")
			}
			payload, _ := json.Marshal([]string{v["from"], v["to"]})
			m.queueAction("link-target", string(payload))
			return nil
		},
		Choices: map[string][]string{"from": names, "to": names},
	})

	return nil
}

func (m *Model) deleteNodeForm() tea.Cmd {
	w := m
	if schedule := m.selectedSchedule(); schedule != nil {
		m.deleteSchedule(*schedule)
		return nil
	}
	id := w.graph.Selected
	m.dialogs.OpenForm(screen.Form{
		Title:      "Delete node",
		Hint:       "References to this node must be repaired before validation succeeds.",
		ApplyLabel: "Delete",
		Fields:     []formField{{Key: "id", Label: "Type node ID to delete: " + id}},
		Apply: func(v map[string]string) error {
			if id == "" || v["id"] != id {
				return fmt.Errorf("type %s", id)
			}
			d := w.editor.Clone()
			for i, s := range d.Steps {
				if s.ID == id {
					d.Steps = append(d.Steps[:i], d.Steps[i+1:]...)
					break
				}
			}
			w.editor.Replace(d)
			w.graph.Selected = ""
			w.graph = m.buildGraph(w.graph)
			return nil
		},
		Destructive: true,
	})

	return nil
}

func (m *Model) reviewForm() tea.Cmd {
	w := m
	if w.run == nil {
		w.setNotice(pane.NoticeWarning, "Open a run first")
		return nil
	}
	pending := []string{}
	for _, s := range w.run.Steps {
		if s.Status == "waiting_review" {
			pending = append(pending, s.ID)
		}
	}
	if len(pending) == 0 {
		w.setNotice(pane.NoticeInfo, "No pending review")
		return nil
	}
	resolved := ""
	for _, step := range w.run.Steps {
		if step.ID == pending[0] {
			resolved = prettyToolArguments(string(step.Args))
			break
		}
	}
	m.dialogs.OpenForm(screen.Form{
		Title:      "Review operation",
		Hint:       "Approval executes this resolved operation. Completed operations are not repeated.",
		ApplyLabel: "Continue",
		Fields:     []formField{{Key: "step", Label: "Waiting node", Value: pending[0], ReadOnly: true}, {Key: "resolved", Label: "Resolved arguments", Value: resolved, Multiline: true, ReadOnly: true}, {Key: "decision", Label: "Decision", Value: "Reject"}},
		Apply: func(v map[string]string) error {
			m.queueAction("review-"+strings.ToLower(v["decision"]), v["step"])
			return nil
		},
		Choices: map[string][]string{"decision": {"Reject", "Approve"}},
	})

	return nil
}
