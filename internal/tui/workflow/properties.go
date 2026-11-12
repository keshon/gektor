package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
)

func (m *Model) selectedStep() workflow.Step {
	for _, s := range m.definition().Steps {
		if s.ID == m.graph.Selected {
			return s
		}
	}
	return workflow.Step{}
}

func propertyValue(raw json.RawMessage) string {
	var ref struct {
		Step    string `json:"$step"`
		Input   string `json:"$input"`
		Pointer string `json:"pointer"`
	}
	if json.Unmarshal(raw, &ref) == nil {
		if ref.Step != "" {
			name := ref.Step
			if ref.Pointer != "" {
				name += "." + strings.ReplaceAll(strings.TrimPrefix(ref.Pointer, "/"), "/", ".")
			}
			return "← " + name
		}
		if ref.Input != "" {
			return "← Workflow input " + ref.Input
		}
	}
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		if *text == "" {
			return "(empty text)"
		}
		return *text
	}
	return string(raw)
}

func workflowPortLabel(n graph.Node, key string, output bool) string {
	ports := n.Inputs
	if output {
		ports = n.Outputs
	}
	for _, p := range ports {
		if p.Key == key {
			return p.Label
		}
	}
	return key
}

func (m *Model) detailRows() []string {
	if schedule := m.selectedSchedule(); schedule != nil && m.run == nil {
		return m.scheduleDetails(*schedule)
	}
	step := m.selectedStep()
	rows := []string{"Inspect"}
	if step.ID == "" {
		return append(rows, "Select a node to inspect it.")
	}
	if m.rawDetails {
		var raw []byte
		if m.run != nil {
			for _, r := range m.run.Steps {
				if r.ID == step.ID || strings.HasPrefix(r.ID, step.ID+"/") {
					b, _ := json.MarshalIndent(r, "", "  ")
					raw = append(raw, b...)
					raw = append(raw, '\n')
				}
			}
		} else {
			raw, _ = json.MarshalIndent(step, "", "  ")
		}
		return append(rows, strings.Split(cleanText(string(raw)), "\n")...)
	}
	if m.inspectorTab == 1 {
		return append(rows, m.resultRows(step)...)
	}
	if m.inspectorTab == 2 {
		return append(rows, m.codeRows(step)...)
	}
	e := &Editor{Definition: m.definition()}
	nodes := map[string]graph.Node{}
	for _, n := range e.Graph(m.graph).Nodes {
		nodes[n.ID] = n
	}
	n := nodes[step.ID]
	operation := n.Label
	if operation == "" {
		operation = step.Tool
	}
	status := "Not run"
	if m.run != nil {
		status = workflowStepStatus(m.run, step.ID)
	}
	rows = append(rows, operation+" / "+status)
	if m.run != nil {
		rows = append(rows, "Frozen recipe / "+m.run.RunID)
	} else {
		rows = append(rows, "Draft properties")
	}
	section := func(title string) { rows = append(rows, "", m.surface.Styles.Accent.Render(title)) }
	valueRow := func(key, label, value string, output bool) {
		prefix := "  "
		if m.portSelected && m.portFocus == key && m.portOutput == output && (key != "" || output) {
			prefix = "› "
		}
		row := prefix + label + strings.Repeat(" ", max(2, 18-ansi.StringWidth(label))) + cleanText(value)
		if prefix == "› " {
			row = m.surface.Styles.Accent.Bold(true).Render(row)
		}
		rows = append(rows, row)
	}
	section("INPUTS")
	count := 0
	for _, p := range n.Inputs {
		if p.Key == "@order" {
			continue
		}
		field, path := p.Key, ""
		if strings.HasPrefix(p.Key, "args/") {
			field = "args"
			path = strings.TrimPrefix(p.Key, "args")
		} else if name, rest, ok := strings.Cut(p.Key, "/"); ok {
			field = name
			path = "/" + rest
		}
		value := connectionValue(m.definition(), step.ID, field, path)
		valueRow(p.Key, p.Label, propertyValue(json.RawMessage(value)), false)
		count++
	}
	if count == 0 {
		rows = append(rows, "  No data inputs")
	}
	section("OUTPUTS")
	links := e.Connections(step.ID)
	for _, p := range n.Outputs {
		if p.Key == "@order" {
			continue
		}
		var targets []string
		for _, c := range links {
			if c.From == step.ID && c.Field != "depends_on" && c.Pointer == p.Key {
				targets = append(targets, c.To+"."+workflowPortLabel(nodes[c.To], c.Field+c.Path, false))
			}
		}
		value := "No connected node"
		if p.Key == "" {
			value = "Whole output"
		}
		if len(targets) > 0 {
			value = "→ " + strings.Join(targets, ", ")
		}
		valueRow(p.Key, p.Label, value, true)
	}
	section("EXECUTION")
	after := "No additional ordering"
	if len(step.DependsOn) > 0 {
		after = "← " + strings.Join(step.DependsOn, ", ")
	}
	valueRow("@order", "after", after, false)
	var then []string
	for _, c := range links {
		if c.From == step.ID && c.Field == "depends_on" {
			then = append(then, c.To+".after")
		}
	}
	thenText := "No ordering connections"
	if len(then) > 0 {
		thenText = "→ " + strings.Join(then, ", ")
	}
	valueRow("@order", "then", thenText, true)
	condition := "Always"
	if len(step.When) > 0 {
		condition = propertyValue(step.When)
		if string(step.When) == "false" {
			condition = "Never (false)"
		}
	}
	valueRow("", "Condition", condition, false)
	approval := "Not required"
	if step.Review {
		approval = "Required before execution"
	}
	valueRow("", "Approval", approval, false)
	repeats := "Once"
	if len(step.ForEach) > 0 {
		repeats = "For each item"
	}
	valueRow("", "Repeats", repeats, false)
	retry := "No automatic retry"
	if step.Retry != nil {
		retry = fmt.Sprintf("Up to %d attempts", step.Retry.MaxAttempts)
	}
	valueRow("", "Retry", retry, false)
	return rows
}

func withoutWorkflowCode(raw json.RawMessage) json.RawMessage {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return raw
	}
	delete(values, "code")
	b, _ := json.Marshal(values)
	return b
}

func (m *Model) resultRows(step workflow.Step) []string {
	if m.run == nil {
		return []string{"No recorded result for this draft.", "", "H opens saved runs."}
	}
	rows := []string{"Recorded run: " + m.run.RunID, "Recipe: " + m.run.RecipePath, "Status: " + workflowStepStatus(m.run, step.ID)}
	for _, r := range m.run.Steps {
		if r.ID != step.ID && !strings.HasPrefix(r.ID, step.ID+"/") {
			continue
		}
		if r.ID != step.ID {
			rows = append(rows, "", r.ID+" / "+r.Status)
		}
		if r.Error != "" {
			rows = append(rows, "", m.surface.Styles.Error.Render("ERROR"), cleanText(r.Error))
		}
		if len(r.Args) > 0 {
			rows = append(rows, "", m.surface.Styles.Accent.Render("RESOLVED INPUTS"))
			rows = append(rows, workflowValues(withoutWorkflowCode(r.Args))...)
		}
		if r.Output != "" {
			rows = append(rows, "", m.surface.Styles.Accent.Render("OUTPUT"))
			rows = append(rows, workflowValues(json.RawMessage(r.Output))...)
		}
		if len(r.Iterations) > 0 {
			rows = append(rows, "", m.surface.Styles.Accent.Render("REPEATED ITEMS"))
			for i, item := range r.Iterations {
				rows = append(rows, fmt.Sprintf("%d  %s", i+1, item.Status))
				if item.Error != "" {
					rows = append(rows, "  "+cleanText(item.Error))
				}
				if i >= 9 {
					rows = append(rows, "Ctrl+J shows all recorded items")
					break
				}
			}
		}
		if len(r.Attempts) > 1 {
			rows = append(rows, fmt.Sprintf("Attempts: %d", len(r.Attempts)))
		}
	}
	return rows
}

func workflowScript(raw json.RawMessage) (string, bool) {
	var values map[string]json.RawMessage
	var code string
	if json.Unmarshal(raw, &values) != nil || json.Unmarshal(values["code"], &code) != nil {
		return "", false
	}
	return code, true
}

func (m *Model) codeRows(step workflow.Step) []string {
	args := step.Args
	label := "Draft script / E edits"
	if m.run != nil {
		label = "Frozen recipe script / read-only"
		for _, r := range m.run.Steps {
			if r.ID == step.ID {
				if _, ok := workflowScript(r.Args); ok {
					args = r.Args
					label = "Recorded script / " + m.run.RunID
				}
				break
			}
		}
	}
	code, ok := workflowScript(args)
	if !ok {
		return []string{"No literal script on this node.", "Scripts supplied by references or repeated items are available in recorded raw data."}
	}
	return append([]string{label, ""}, strings.Split(cleanText(code), "\n")...)
}

func (m *Model) editCode() {
	if m.run != nil || m.busy {
		m.setNotice(pane.NoticeWarning, "Recorded scripts are read-only")
		return
	}
	step := m.selectedStep()
	code, ok := workflowScript(step.Args)
	if !ok {
		m.setNotice(pane.NoticeWarning, "No literal script to edit; use node arguments for references")
		return
	}
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Code / " + step.ID,
		Hint:       "Edits the draft script. Run and validation remain separate actions.",
		ApplyLabel: "Apply",
		Fields:     []formField{{Key: "code", Label: "JavaScript", Value: code, Multiline: true}},
	}, editNodeCode{
		draft: draft,
		step:  step,
	})
}

type editNodeCode struct {
	draft string
	step  workflow.Step
}

func (edit editNodeCode) apply(m *Model, v map[string]string) error {
	if m.run != nil || m.busy || m.editor.Source() != edit.draft {
		return fmt.Errorf("draft changed; reopen the script")
	}

	d := m.editor.Clone()
	for i := range d.Steps {
		if d.Steps[i].ID == edit.step.ID {
			var args map[string]json.RawMessage
			if err := json.Unmarshal(d.Steps[i].Args, &args); err != nil {
				return err
			}
			args["code"], _ = json.Marshal(v["code"])
			d.Steps[i].Args, _ = json.Marshal(args)
			m.editor.Replace(d)
			m.graph = m.buildGraph(m.graph)
			return nil
		}
	}
	return fmt.Errorf("node no longer exists")
}
