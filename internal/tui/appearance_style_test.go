package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestAppearanceStylesSwitchWithoutChangingTheConversation(t *testing.T) {
	defer screen.SetModern(false)
	for _, workflow := range []bool{false, true} {
		m := sizeModelTo(t, settingsModel(t), 120, 30)
		if workflow {
			m.openWorkflows()
		}
		before := snapshotOf(m)
		for _, style := range []string{"modern", "commander", "modern"} {
			m.runAction("appearance")
			values := formSnapshot(m).Values()
			values["style"] = style
			if err := m.applyValues(values); err != nil {
				t.Fatal(err)
			}
			m.chrome.Close()
			m.settleControls()
			m.syncPresentation()
			modern := style == "modern"
			surface := m.workflowSurface()
			if surface.Palette == nil || surface.Palette.Modern != modern || surface.Styles.Tabs.Filled != modern || (surface.Layout(120, true).Footer.Height == 0) != modern {
				t.Fatal("workflow inspector did not receive the selected style", workflow, style)
			}
			if m.agent.Appearance().Style != style || screen.CurrentPalette().Modern != modern || m.chrome.ActionBarVisible() == modern {
				t.Fatal("style is not shared across screen, palette and controls", workflow, style)
			}
			loaded, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil || loaded.Appearance.Style != style {
				t.Fatal("style was not persisted", err, loaded.Appearance.Style)
			}
			after := snapshotOf(m)
			if after.Draft != before.Draft || !reflect.DeepEqual(after.History, before.History) {
				t.Fatal("style changed the draft or model history", workflow, style)
			}
			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(rows) != 30 {
				t.Fatal("switching styles lost rows", workflow, style)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != 120 {
					t.Fatal("switching styles lost columns", workflow, style)
				}
			}
		}
	}
}
