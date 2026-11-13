package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
)

func (m *Model) setupForm() tea.Cmd {
	if m.run != nil {
		return nil
	}
	m.openForm(screen.Form{
		Title:      "Edit workflow",
		Hint:       "Choose what to change. AI proposals are reviewed before applying.",
		ApplyLabel: "Open",
		Fields:     []formField{{Key: "section", Label: "Section", Value: "Selected step arguments"}},
		Choices:    map[string][]string{"section": {"Selected step arguments", "AI change", "Name", "Inputs", "Outputs", "Complete JSON"}},
	}, func(v map[string]string) error {
		actions := map[string]string{"Selected step arguments": "edit", "AI change": "ai", "Name": "name", "Inputs": "inputs", "Outputs": "outputs", "Complete JSON": "recipe"}
		action, ok := actions[v["section"]]
		if !ok {
			return fmt.Errorf("choose a section")
		}
		m.queueAction(action, "")
		return nil
	})

	return nil
}

func (m *Model) addNodeForm() tea.Cmd {
	names := append(m.registry.Names(), "Schedule trigger")
	m.openForm(screen.Form{
		Title:      "Add node",
		Hint:       "Choose an operation or a Schedule trigger.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "type", Label: "Node type", Value: names[0]}},
		Choices:    map[string][]string{"type": names},
	}, func(v map[string]string) error {
		if v["type"] == "Schedule trigger" {
			m.queueAction("schedule-add", "")
			return nil
		}
		_, nodeErr := m.registry.Node(v["type"])
		if nodeErr != nil {
			return nodeErr
		}
		m.queueAction("add-operation", v["type"])
		return nil
	})

	return nil
}

func (m *Model) behaviorForm() tea.Cmd {
	for _, step := range m.editor.Definition.Steps {
		if step.ID != m.graph.Selected {
			continue
		}
		original, _ := json.Marshal(step)
		schema := schemaform.Parse(json.RawMessage(`{"properties":{"review":{"type":"boolean","enum":[true,false]},"depends_on":{"type":"array"},"when":{},"for_each":{},"concurrency":{"type":"integer","minimum":1,"maximum":32},"on_error":{"type":"string","enum":["stop","collect"]},"merge":{"type":"string","enum":["all","any"]},"retry":{"type":"object"}}}`))
		draft := m.editor.Source()
		id := step.ID
		m.openForm(screen.Form{
			Title:      step.ID + " / behavior",
			Hint:       "Map concurrency requires a concurrent operation. Collect retains operation failures; permission and storage failures stop the run.",
			ApplyLabel: "Apply",
			Fields:     schema.Fields(original),
			Choices:    schema.Choices(),
		}, func(v map[string]string) error {
			if m.busy || m.run != nil || m.editor.Source() != draft {
				return fmt.Errorf("draft changed; reopen node behavior")
			}

			raw, err := schema.Values(original, v)
			if err != nil {
				return err
			}

			var changed workflow.Step
			decodeErr := json.Unmarshal(raw, &changed)
			if decodeErr != nil {
				return decodeErr
			}

			d := m.editor.Clone()
			for i, step := range d.Steps {
				if step.ID == id {
					d.Steps[i] = changed
					break
				}
			}

			m.editor.Replace(d)
			m.graph = m.buildGraph(m.graph)
			return nil
		})

		break
	}
	return nil
}

func (m *Model) proposalForm() tea.Cmd {
	m.openForm(screen.Form{
		Title:      "AI workflow proposal",
		Hint:       "The model proposes recipe edits only. You review before applying.",
		ApplyLabel: "Propose",
		Fields:     []formField{{Key: "prompt", Label: "Describe the change", Multiline: true}},
	}, func(v map[string]string) error {
		if strings.TrimSpace(v["prompt"]) == "" {
			return fmt.Errorf("describe a change")
		}
		m.queueAction("ai-prompt", v["prompt"])
		return nil
	})
	return nil
}

func (m *Model) sourceForm() tea.Cmd {
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Recipe JSON",
		Hint:       "Inputs, outputs, conditions, iteration and retry are preserved here.",
		ApplyLabel: "Apply",
		Fields:     []formField{{Key: "source", Label: "Recipe", Value: m.editor.Source(), Multiline: true}},
	}, func(v map[string]string) error {
		if m.busy || m.run != nil || m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen recipe JSON")
		}

		d, err := Decode([]byte(v["source"]))
		if err != nil {
			return err
		}
		m.editor.Replace(d)
		m.graph = m.buildGraph(m.graph)
		return nil
	})
	return nil
}

func (m *Model) connectForm() tea.Cmd {
	names := []string{}
	for _, s := range m.editor.Definition.Steps {
		names = append(names, s.ID)
	}
	if len(names) < 2 {
		m.setNotice(pane.NoticeWarning, "Add at least two nodes first")
		return nil
	}
	destination := names[0]
	if destination == m.graph.Selected {
		destination = names[1]
	}
	m.openForm(screen.Form{
		Title:      "Connect nodes",
		Hint:       "Choose the source and destination, then map their data.",
		ApplyLabel: "Next",
		Fields: []formField{
			{Key: "from", Label: "Source node", Value: m.graph.Selected},
			{Key: "to", Label: "Destination node", Value: destination},
		},
		Choices: map[string][]string{"from": names, "to": names},
	}, func(v map[string]string) error {
		if v["from"] == v["to"] {
			return fmt.Errorf("choose different nodes")
		}
		payload, _ := json.Marshal([]string{v["from"], v["to"]})
		m.queueAction("link-target", string(payload))
		return nil
	})

	return nil
}

func (m *Model) deleteNodeForm() tea.Cmd {
	if schedule := m.selectedSchedule(); schedule != nil {
		m.deleteSchedule(*schedule)
		return nil
	}
	id := m.graph.Selected
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:       "Delete node",
		Hint:        "References to this node must be repaired before validation succeeds.",
		ApplyLabel:  "Delete",
		Fields:      []formField{{Key: "id", Label: "Type node ID to delete: " + id}},
		Destructive: true,
	}, func(v map[string]string) error {
		if m.busy || m.run != nil || m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen node deletion")
		}

		if id == "" || v["id"] != id {
			return fmt.Errorf("type %s", id)
		}

		d := m.editor.Clone()
		for i, s := range d.Steps {
			if s.ID == id {
				d.Steps = append(d.Steps[:i], d.Steps[i+1:]...)
				break
			}
		}

		m.editor.Replace(d)
		m.graph.Selected = ""
		m.graph = m.buildGraph(m.graph)
		return nil
	})

	return nil
}

func (m *Model) reviewForm() tea.Cmd {
	if m.run == nil {
		m.setNotice(pane.NoticeWarning, "Open a run first")
		return nil
	}
	pending := []string{}
	for _, s := range m.run.Steps {
		if s.Status == "waiting_review" {
			pending = append(pending, s.ID)
		}
	}
	if len(pending) == 0 {
		m.setNotice(pane.NoticeInfo, "No pending review")
		return nil
	}
	resolved := ""
	for _, step := range m.run.Steps {
		if step.ID == pending[0] {
			resolved = prettyToolArguments(string(step.Args))
			break
		}
	}
	m.openForm(screen.Form{
		Title:      "Review operation",
		Hint:       "Approval executes this resolved operation. Completed operations are not repeated.",
		ApplyLabel: "Continue",
		Fields: []formField{
			{Key: "step", Label: "Waiting node", Value: pending[0], ReadOnly: true},
			{Key: "resolved", Label: "Resolved arguments", Value: resolved, Multiline: true, ReadOnly: true},
			{Key: "decision", Label: "Decision", Value: "Reject"},
		},
		Choices: map[string][]string{"decision": {"Reject", "Approve"}},
	}, func(v map[string]string) error {
		m.queueAction("review-"+strings.ToLower(v["decision"]), v["step"])
		return nil
	})

	return nil
}
