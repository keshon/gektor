package workflow

import (
	"testing"

	workflowrun "github.com/keshon/gektor/internal/workflow"
)

func TestMenusRespectApplicationWorkAndFrozenRuns(t *testing.T) {
	cases := []struct {
		name                    string
		available, busy, frozen bool
		edit, stop, review      bool
	}{
		{name: "editable draft", available: true, edit: true},
		{name: "application busy"},
		{name: "running workflow", available: true, busy: true, stop: true},
		{name: "frozen run", available: true, frozen: true, review: true},
		{name: "frozen run while application busy", frozen: true},
		{name: "active run snapshot", available: true, busy: true, frozen: true, stop: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := workflowTestModel(t).workflows
			m.busy = tc.busy
			if tc.frozen {
				m.run = &workflowrun.Run{}
			}
			check := func(menu, command string, enabled bool) {
				t.Helper()
				for _, item := range m.MenuItems(menu, tc.available) {
					if item.Command == command {
						if item.Enabled != enabled {
							t.Fatalf("%s enabled=%v, want %v", command, item.Enabled, enabled)
						}
						return
					}
				}
				t.Fatal("missing menu command", command)
			}
			check("node", "/workflows add", tc.edit)
			check("recipe", "/workflows save", tc.edit)
			check("execution", "/workflows run", tc.edit)
			check("execution", "/workflows validate", tc.edit)
			check("execution", "/workflows stop", tc.stop)
			check("execution", "/workflows review", tc.review)
		})
	}
}

func TestMenuTogglesTrackWorkflowCommands(t *testing.T) {
	m := workflowTestModel(t).workflows
	for _, action := range []string{"raw-details", "lock-positions", "inspector", "overview"} {
		command := "/workflows " + action
		toggle, initial := m.MenuToggle(command)
		if !toggle {
			t.Fatal("command has no toggle", command)
		}
		m.Command(action)
		toggle, changed := m.MenuToggle(command)
		if !toggle || changed == initial {
			t.Fatal("menu did not follow command", command)
		}
		m.Command(action)
		_, restored := m.MenuToggle(command)
		if restored != initial {
			t.Fatal("second command did not restore toggle", command)
		}
	}
	m.Command("lock-positions")
	for _, item := range m.MenuItems("view", true) {
		if item.Command == "/workflows layout" && item.Enabled {
			t.Fatal("locked positions still allow auto-layout")
		}
	}
	if m.MenuItems("settings", true) != nil {
		t.Fatal("workflow claimed application settings")
	}
}
