package workflow

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureSelection()
	key := msg.String()
	if m.inspectorOpen && (key == "1" || key == "2" || key == "3") {
		m.setInspectorTab(int(key[0] - '1'))
		m.inspect = true
		return m, nil
	}
	if m.inspectorOpen && m.inspect {
		switch key {
		case "left":
			m.setInspectorTab((m.inspectorTab + 2) % 3)
			return m, nil
		case "right":
			m.setInspectorTab((m.inspectorTab + 1) % 3)
			return m, nil
		case "e", "enter":
			if m.inspectorTab == 2 {
				m.editCode()
			} else if m.inspectorTab == 0 && m.run == nil && !m.busy {
				m.edit()
			}
			return m, nil
		}
	}
	action := ""
	switch key {
	case "esc":
		if m.busy {
			action = "stop"
		} else if m.connectFrom != "" {
			m.connectFrom = ""
			m.setNotice(pane.NoticeInfo, "Connection cancelled")
		} else if m.inspectorOpen && m.inspect {
			m.inspectorOpen = false
			m.inspect = false
			m.thumb = scrollDrag{}
		} else {
			action = "chat"
		}
	case "ctrl+j":
		action = "raw-details"
	case "ctrl+t":
		action = "inspector"
	case "ctrl+o":
		action = "open"
	case "ctrl+n":
		action = "new"
	case "ctrl+s":
		action = "save"
	case "a":
		action = "add"
	case "enter":
		if m.run != nil {
			m.inspectorOpen = true
			m.inspect = true
			m.setInspectorTab(1)
			return m, nil
		}
		action = "edit"
	case "g":
		action = "lock-positions"
	case "c":
		action = "link"
	case "b":
		action = "connections"
	case "delete":
		action = "delete"
	case "e":
		action = "behavior"
	case "i":
		action = "ai"
	case "j":
		action = "recipe"
	case "m":
		action = "name"
	case "u":
		action = "inputs"
	case "o":
		action = "outputs"
	case "v":
		action = "validate"
	case "r":
		action = "run"
	case "h":
		action = "runs"
	case "p":
		action = "review"
	case "d":
		action = "draft"
	case "f":
		m.overview = !m.overview
	case "l":
		action = "layout"
	case "ctrl+z":
		action = "undo"
	case "ctrl+y":
		action = "redo"
	case "tab":
		if m.inspectorOpen {
			m.inspect = !m.inspect
		}
	case "up", "down", "left", "right":
		delta := 1
		if key == "up" || key == "left" {
			delta = -1
		}
		if m.inspect {
			m.offset = max(0, m.offset+delta)
		} else {
			m.portSelected = false
			m.offset = 0
			m.graph.SelectNext(delta)
			cw, ch := m.canvasSize()
			m.graph.Reveal(cw, ch)
		}
	case "shift+left":
		if m.positionsLocked {
			break
		}
		m.graph.Move(-2, 0)
	case "shift+right":
		if m.positionsLocked {
			break
		}
		m.graph.Move(2, 0)
	case "shift+up":
		if m.positionsLocked {
			break
		}
		m.graph.Move(0, -1)
	case "shift+down":
		if m.positionsLocked {
			break
		}
		m.graph.Move(0, 1)
	case "alt+left":
		m.graph.Pan.X -= 8
	case "alt+right":
		m.graph.Pan.X += 8
	case "alt+up":
		m.graph.Pan.Y -= 4
	case "alt+down":
		m.graph.Pan.Y += 4
	case "pgup", "pgdown", "home", "end":
		m.page(key)
	case "f1":
		m.dialogs.PresentDialog(screen.Dialog{Title: "Workflow keys", Lines: strings.Split("Ctrl+O open · Ctrl+N new · Ctrl+S save\nA add · Enter edit/inspect · Delete delete\nC connect · B connections · E node behavior · J complete recipe JSON\nM recipe name · U inputs · O outputs\nArrows select · Shift+arrows move · G locks positions · Alt+arrows pan\nDrag background to pan · Shift+wheel horizontal\nClick output then input to connect · Ctrl+T inspector\nTab pane focus · 1/2/3 inspector tabs · Ctrl+J raw data\nInspector E edits properties/code · Esc closes focused inspector\nF compact overview · L auto-layout · Ctrl+Z undo · Ctrl+Y redo\nV validate · R run · H saved runs · P review\nD editable draft · I AI proposal\nEsc chat (draft retained) / stop active operation\nF9 main menu · Ctrl+Q quit", "\n")})
		return m, nil
	}
	if strings.HasPrefix(key, "shift+") {
		m.persistLayout()
	}
	if action != "" {
		if m.run != nil && (action == "ai" || action == "edit" || action == "add" || action == "link" || action == "connections" || action == "delete" || action == "recipe") {
			m.setNotice(pane.NoticeInfo, "Viewing a frozen run · D returns to the editable draft")
			return m, nil
		}
		return m, m.command(action)
	}
	return m, nil
}
