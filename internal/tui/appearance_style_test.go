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
			if surface.Palette != m.chrome.Props.Palette || surface.Palette != screen.CurrentPalette() || surface.Palette.Modern != modern || surface.Styles.Tabs.Filled != modern || (surface.Layout(120, true).Footer.Height == 0) != modern {
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

func TestCaretChoicePersistsAcrossScreensAndThemes(t *testing.T) {
	previous := screen.CurrentPalette()
	defer func() {
		screen.SetTheme(previous.Name)
		screen.SetModern(previous.Modern)
		screen.SetCaret(previous.Caret)
	}()
	for _, workflow := range []bool{false, true} {
		m := sizeModelTo(t, settingsModel(t), 120, 30)
		if workflow {
			m.openWorkflows()
		}
		before := snapshotOf(m)
		for _, caret := range []string{"block", "underline", "bar"} {
			m.runAction("appearance")
			values := formSnapshot(m).Values()
			values["caret"] = caret
			values["theme"] = "light"
			err := m.applyValues(values)
			if err != nil {
				t.Fatal(err)
			}
			m.chrome.Close()
			m.settleControls()
			loaded, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil || loaded.Appearance.Caret != caret || m.agent.Appearance().Caret != caret || screen.CurrentPalette().Caret != caret {
				t.Fatal("caret selection not persisted/applied", workflow, caret, err)
			}
			m.runAction("appearance")
			if formSnapshot(m).Values()["caret"] != caret {
				t.Fatal("appearance lost caret choice", caret)
			}
			m.chrome.Close()
			after := snapshotOf(m)
			if after.Draft != before.Draft || !reflect.DeepEqual(after.History, before.History) {
				t.Fatal("caret changed conversation")
			}
		}
	}
}

func TestStatusAnimationPersistsAcrossThemeChanges(t *testing.T) {
	previous := screen.CurrentPalette().Appearance
	defer screen.SetAppearance(previous)
	m := sizeModelTo(t, settingsModel(t), 120, 30)
	m.runAction("appearance")
	values := formSnapshot(m).Values()
	values["status_animation"] = "text"
	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil || prefs.Appearance.StatusAnimation != "text" {
		t.Fatal("status animation was not saved", prefs.Appearance, err)
	}
	m.applyAppearance("dark", prefs.Appearance)
	m.applyTheme("light")
	if m.agent.Appearance().StatusAnimation != "text" {
		t.Fatal("theme change lost the status animation")
	}
}
