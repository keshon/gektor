package screen

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/table"
)

func TestDialogActionsUseOwnerLabelsAndIdentity(t *testing.T) {
	controls := &Controls{Props: ControlProps{Width: 120, Height: 30}}
	actions := []DialogAction{{ID: "archive", Key: 'x', Label: "archive record"}}
	controls.PresentDialog(Dialog{Title: "Record", Actions: actions})
	dialog := controls.DialogID()
	actions[0].ID = "caller changed"
	controls.TakeEvents()
	if !strings.Contains(controls.LayoutDialog().Hint, "archive record") {
		t.Fatal("owner action label missing from hints")
	}
	controls.InfoKey(tea.KeyPressMsg{Code: 'X', Text: "X"})
	events := controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != DialogActionRequested || events[0].Action != "archive" || events[0].ID != dialog {
		t.Fatal("action did not retain copied intent and producing dialog", events)
	}
}

func TestActionBrowserUsesMenuAvailabilityAndCommands(t *testing.T) {
	controls := &Controls{Props: ControlProps{Menus: []Menu{{Label: "Records", Items: []MenuItem{
		{Text: "Open record", Command: "record:open", Shortcut: "Enter", Enabled: true},
		{Text: "Delete record", Command: "record:delete", Enabled: false},
	}}}}}
	controls.OpenActions("")
	model := controls.Dialog.Table.Model
	if len(model.Rows) != 1 || model.Rows[0].ID != "record:open" || model.Focus != 1 {
		t.Fatal("browser ignored availability or menu command identity", model.Rows)
	}
	controls.Props.Area = pane.Rect{Width: 100, Height: 24}
	controls.TakeEvents()
	controls.TableInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	events := controls.TakeEvents()
	last := events[len(events)-1]
	if last.Kind != TableAction || last.Row != "record:open" {
		t.Fatal("browser dispatched an effect instead of reporting its command", events)
	}
}

func TestExpandedReadingPreservesTableAndDialogScroll(t *testing.T) {
	controls := &Controls{Props: ControlProps{Width: 120, Height: 30, Ready: true, Area: pane.Rect{Width: 120, Height: 26}}}
	controls.Open("Details", []string{strings.Repeat("Read this text. ", 200)})
	controls.ScrollDialog(7)
	offset := controls.Dialog.Offset
	controls.InfoKey(tea.KeyPressMsg{Code: tea.KeyF11})
	if controls.LayoutDialog().Inner != 114 || controls.Dialog.Offset != offset {
		t.Fatal("expanding details lost scroll or did not expand")
	}
	controls.InfoKey(tea.KeyPressMsg{Code: tea.KeyF11})
	if controls.LayoutDialog().Inner != 76 {
		t.Fatal("details did not restore normal width")
	}
	model := controls.NewTable()
	model.Rows = []table.Row{{ID: "stable", Cells: []string{"Record"}}}
	model.Focus = 1
	controls.PresentDialog(Dialog{Title: "Records", Table: &Table{Model: model}})
	controls.TableInput(tea.KeyPressMsg{Code: tea.KeyF11})
	if model.Layout(controls.TableProps()).Width != 118 || model.Focus != 1 || model.Selected() != "stable" {
		t.Fatal("expanding table lost context")
	}
}

func TestHostsKeepIndependentPalettesAndHonorColorPolicy(t *testing.T) {
	defer SetTheme("dark")
	makeHost := func(name string) *Controls {
		palette := NewPalette(name)
		return &Controls{Props: ControlProps{Width: 80, Height: 24, Palette: palette, Styles: palette.Styles(),
			Menus: []Menu{{ID: "records", Label: "Records"}}}}
	}
	dark, light := makeHost("dark"), makeHost("light")
	before := dark.Compose(tea.NewView("Content")).Content
	SetTheme("solarized-light")
	light.Compose(tea.NewView("Content"))
	if after := dark.Compose(tea.NewView("Content")).Content; after != before {
		t.Fatal("another host or application theme changed this host")
	}
	for _, profile := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.ASCII, colorprofile.ANSI, colorprofile.ANSI256} {
		dark.Props.ColorProfile = profile
		content := dark.Compose(tea.NewView("Content")).Content
		if !strings.Contains(ansi.Strip(content), "Records") || strings.Contains(content, ";2;") {
			t.Fatal("reduced-color rendering lost content or emitted truecolor", profile)
		}
		if profile == colorprofile.NoTTY && strings.Contains(content, "\x1b[") {
			t.Fatal("color-free policy emitted styling")
		}
	}
}
