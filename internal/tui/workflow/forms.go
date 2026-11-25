package workflow

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func (m *Model) setupForm() tea.Cmd {
	if m.run != nil {
		return nil
	}
	sections := []string{"Name", "Inputs", "Outputs", "AI change", "Complete JSON"}
	if m.selectedStep().ID != "" {
		sections = append(sections, "Selected step arguments")
	}
	m.openForm(screen.Form{
		Title:      "Recipe settings",
		Hint:       "Choose what to change. AI proposals are reviewed before applying.",
		ApplyLabel: "Open",
		Fields:     []formField{{Key: "section", Label: "Section", Value: sections[0]}},
		Choices:    map[string][]string{"section": sections},
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
	descriptions := map[string]string{"Schedule trigger": "Schedule trigger · Starts the entire recipe"}
	for _, name := range m.registry.Names() {
		node, err := m.registry.Node(name)
		if err == nil && node.Description != "" {
			descriptions[name] = name + " · " + node.Description
		}
	}
	m.openForm(screen.Form{
		Title:      "Add node",
		Hint:       "Choose an operation or a Schedule trigger.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "type", Label: "Node type", Value: names[0]}},
		Choices:    map[string][]string{"type": names},
		Labels:     map[string]map[string]string{"type": descriptions},
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
	from := m.selectedStep().ID
	var options []string
	destinations := map[string]Connection{}
	for _, step := range m.editor.Definition.Steps {
		if step.ID == from {
			continue
		}
		paths, labels := m.connectionDestinations(step)
		for _, label := range labels {
			choice := step.ID + " / " + label
			options = append(options, choice)
			destinations[choice] = paths[label]
		}
	}
	if from == "" || len(options) == 0 {
		m.setNotice(pane.NoticeWarning, "Select a source node and add at least two nodes first")
		return nil
	}
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Connect from " + from,
		Hint:       "Choose a destination argument to pass data, or Run after to set order.",
		ApplyLabel: "Review",
		Fields:     []formField{{Key: "destination", Label: "Destination", Value: options[0]}},
		Choices:    map[string][]string{"destination": options},
	}, func(v map[string]string) error {
		if m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen connections")
		}
		dest, ok := destinations[v["destination"]]
		if !ok {
			return fmt.Errorf("choose a destination")
		}
		m.queueConnection(workflowConnectionEdit{From: from, To: dest.To, Field: dest.Field, Path: dest.Path, Draft: draft})
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
	for _, s := range workflow.ScopedSteps(*m.run) {
		if s.Result.Status == "waiting_review" && s.Definition.Review && !m.run.Approved[s.ID] {
			pending = append(pending, s.ID)
		}
	}
	if len(pending) == 0 {
		m.setNotice(pane.NoticeInfo, "No pending review")
		return nil
	}
	resolved := ""
	for _, step := range workflow.ScopedSteps(*m.run) {
		if step.ID == pending[0] {
			resolved = prettyToolArguments(string(step.Result.Args))
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
