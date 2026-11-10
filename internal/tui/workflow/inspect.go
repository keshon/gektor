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
	w := m
	w.inspectorTab = max(0, min(2, tab))
	w.rawDetails = false
	w.offset = 0
	w.thumb = scrollDrag{}
}

func (m *Model) inspectorTabs() pane.TabLayout {
	w := m
	labels := []string{"Properties", "Result", "Code"}
	if m.selectedSchedule() != nil && w.run == nil {
		labels = []string{"Schedule", "Inputs", "Service"}
	}
	active := w.inspectorTab
	if w.rawDetails {
		active = -1
	}
	view := m.presentation()
	width := view.Layout(view.Panes(true).Side.Width, true).Body.Width
	return pane.Tabs(labels, active, width, w.inspect && !view.HeaderFocused,
		pane.TabStyles{Normal: view.Styles.Muted, Active: view.Styles.Panels.Title, Focus: view.Styles.Panels.Focus})
}

func (m *Model) focusPort(key string, output bool) {
	w := m
	w.inspectorTab = 0
	w.rawDetails = false
	w.portFocus, w.portOutput, w.portSelected = key, output, true
	w.offset = 0
	for i, row := range m.inspectorContent() {
		if strings.HasPrefix(ansi.Strip(row), "› ") {
			w.offset = max(0, i-2)
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
	var statuses []string
	for _, s := range run.Steps {
		if s.ID == id {
			return s.Status
		}
		if strings.HasPrefix(s.ID, id+"/") {
			statuses = append(statuses, s.Status)
		}
	}
	if len(statuses) == 0 {
		return "not reached"
	}
	for _, priority := range []string{"failed", "interrupted", "waiting_review", "running", "cancelled", "pending"} {
		for _, status := range statuses {
			if status == priority {
				return status
			}
		}
	}
	for _, status := range statuses {
		if status != "skipped" {
			return "completed"
		}
	}
	return "skipped"
}
