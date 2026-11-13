package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/workflow"
)

func (m *Model) setInspectorTab(tab int) {
	m.inspectorTab = max(0, min(2, tab))
	m.rawDetails = false
	m.offset = 0
	m.thumb = scrollDrag{}
}

func (m *Model) inspectorTabs() pane.TabLayout {
	labels := []string{"Properties", "Result", "Code"}
	if m.selectedSchedule() != nil && m.run == nil {
		labels = []string{"Schedule", "Inputs", "Service"}
	}
	active := m.inspectorTab
	if m.rawDetails {
		active = -1
	}
	view := m.surface
	width := view.Layout(view.Panes(true).Side.Width, true).Body.Width
	return pane.Tabs(labels, active, width, m.inspect && !view.HeaderFocused,
		pane.TabStyles{Normal: view.Styles.Muted, Active: view.Styles.Panels.Title, Focus: view.Styles.Panels.Focus})
}

func (m *Model) focusPort(key string, output bool) {
	m.inspectorTab = 0
	m.rawDetails = false
	m.portFocus, m.portOutput, m.portSelected = key, output, true
	m.offset = 0
	for i, row := range m.inspectorContent() {
		if strings.HasPrefix(ansi.Strip(row), "› ") {
			m.offset = max(0, i-2)
			break
		}
	}
}

// Present actual values and references without asking a model to explain code.
func workflowValues(raw json.RawMessage) []string {
	var lines []string
	var walk func(string, json.RawMessage, int)
	walk = func(label string, value json.RawMessage, depth int) {
		indent := strings.Repeat("  ", depth)
		if readable := workflowValue(value); readable != string(value) {
			lines = append(lines, indent+label+": "+readable)
			return
		}
		var object map[string]json.RawMessage
		if depth < 6 && json.Unmarshal(value, &object) == nil && object != nil {
			if len(object) == 0 {
				if label == "" {
					lines = append(lines, "(empty object)")
					return
				}
				lines = append(lines, indent+label+": empty")
				return
			}
			if label != "" {
				lines = append(lines, indent+label+":")
				depth++
			}
			keys := make([]string, 0, len(object))
			for key := range object {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(key, object[key], depth)
			}
			return
		}
		if label == "" {
			label = "Value"
		}
		var text *string
		if json.Unmarshal(value, &text) == nil && text != nil {
			display := *text
			if display == "" {
				display = "(empty text)"
			}
			lines = append(lines, indent+label+":")
			for _, row := range strings.Split(cleanText(display), "\n") {
				lines = append(lines, indent+"  "+row)
			}
			return
		}
		var items []json.RawMessage
		if depth < 6 && json.Unmarshal(value, &items) == nil && items != nil {
			lines = append(lines, fmt.Sprintf("%s%s: %d items", indent, label, len(items)))
			for i, item := range items {
				if i >= 5 {
					lines = append(lines, indent+"  … Ctrl+J shows all values")
					break
				}
				walk(fmt.Sprintf("%d", i+1), item, depth+1)
			}
			return
		}
		lines = append(lines, indent+label+": "+cleanText(string(value)))
	}
	if !json.Valid(raw) {
		return strings.Split(cleanText(string(raw)), "\n")
	}
	walk("", raw, 0)
	return lines
}

func workflowStepStatus(run *workflow.Run, id string) string {
	for _, step := range workflow.ScopedSteps(*run) {
		if step.ID == id {
			return step.Result.Status
		}
	}
	return "not reached"
}
