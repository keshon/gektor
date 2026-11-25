package screen

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/pane"
)

func TestDialogCaretsFollowFocusedEditors(t *testing.T) {
	previous := CurrentPalette()
	defer func() { SetTheme(previous.Name); SetModern(previous.Modern); SetCaret(previous.Caret) }()
	for _, modern := range []bool{false, true} {
		SetModern(modern)
		for name, shape := range map[string]tea.CursorShape{"block": tea.CursorBlock, "bar": tea.CursorBar, "underline": tea.CursorUnderline} {
			SetCaret(name)
			controls := Controls{Props: ControlProps{Width: 100, Height: 30, Ready: true, Area: pane.Rect{X: 3, Y: 2, Width: 90, Height: 24}, Palette: CurrentPalette(), Styles: DefaultStyles()}}
			check := func(text string) {
				t.Helper()
				view := controls.Compose(tea.NewView(""))
				caret := view.Cursor
				if caret == nil || caret.Shape != shape || !caret.Blink {
					t.Fatal("missing/wrong dialog caret", modern, name, caret)
				}
				rows := strings.Split(ansi.Strip(view.Content), "\n")
				if caret.Y < 0 || caret.Y >= len(rows) {
					t.Fatal("caret outside view", caret)
				}
				row := rows[caret.Y]
				at := strings.Index(row, text)
				if at < 0 || caret.X != ansi.StringWidth(row[:at])+ansi.StringWidth(text) {
					t.Fatal("caret detached from editor", text, row, caret)
				}
			}
			controls.OpenForm("Edit", "", "Apply", []form.Field{{Key: "title", Label: "Title", Value: "аб界"}})
			controls.dialog.Form.Editor.CursorEnd()
			check("аб界")
			controls.FocusForm(1)
			if controls.dialogCursor() != nil {
				t.Fatal("button retained caret")
			}
			controls.FocusForm(0)
			controls.dialog.Form.Choices = map[string][]string{"title": {"alpha", "beta"}}
			controls.OpenFormPicker(controls.DialogID())
			picker := controls.dialog.Form.Picker
			picker.Search.SetValue("alp")
			picker.Search.CursorEnd()
			check("Search: alp")
			picker.Control = 1
			if controls.dialogCursor() != nil {
				t.Fatal("picker action retained caret")
			}
			picker.Control = 0
			controls.Header.Focused = true
			if controls.dialogCursor() != nil {
				t.Fatal("menu retained dialog caret")
			}
			controls.Header.Focused = false
			controls.PresentDialog(Dialog{Title: "Rows", Table: &Table{}})
			model := controls.dialog.Table.model
			model.Focus = 1
			model.SyncFocus(true)
			model.Search.SetValue("filter")
			model.Search.CursorEnd()
			check("Filter: filter")
			model.Focus = 0
			if controls.dialogCursor() != nil {
				t.Fatal("table rows retained caret")
			}
			controls.PresentSecret("Credential", "Enter a secret")
			controls.SecretInput(tea.PasteMsg{Content: "password"})
			caret := controls.dialogCursor()
			if caret == nil || caret.Shape != shape || strings.Contains(controls.DialogView(), "password") {
				t.Fatal("secret caret or masking changed", caret)
			}
		}
	}
}
