package settings

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/form"
)

func TestInlineChoiceKeyboardMouseAndInputOwnership(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 40}} {
		f := NewModel([]form.Field{{Key: "theme", Label: "Theme", Value: "dark"}}, textarea.New())
		f.Choices = map[string][]string{"theme": config.ThemeNames()}
		f.ApplyLabel = "Apply"
		f.Focus(0, nil)
		props := Props{Width: size[0], Height: size[1]}
		f.SyncInputFocus(true)
		if f.InputOwner() != NoInput || f.Editor.Focused() {
			t.Fatal("radio group owns a text caret")
		}
		f.UpdateInput(tea.PasteMsg{Content: "invalid"})
		f.UpdateKey(tea.KeyPressMsg{Code: 'x', Text: "x"}, props)
		if f.Editor.Value() != "dark" {
			t.Fatal("text input changed a fixed choice")
		}
		f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyRight}, props)
		if f.Picker != nil || f.Editor.Value() != "light" {
			t.Fatal("inline selection opened a picker or did not change")
		}
		rows, hits, _, _ := f.Rows(props)
		if len(rows)+2 > props.Height {
			t.Fatalf("inline choices overflow %v: %s", size, ansi.Strip(strings.Join(rows, "\n")))
		}
		x, y := f.Origin(props)
		clicked := false
		for _, hit := range hits {
			if hit.Option > 0 && f.Options("theme")[hit.Option-1] != "light" {
				want := f.Options("theme")[hit.Option-1]
				f.Mouse(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y0, Button: tea.MouseLeft}, props)
				if f.Editor.Value() != want || f.Picker != nil {
					t.Fatal("radio hit missed its drawn option")
				}
				clicked = true
				break
			}
		}
		if !clicked {
			t.Fatal("no selectable radio target")
		}
	}
}

func TestBooleanIsAnInlineCheckbox(t *testing.T) {
	f := NewModel([]form.Field{{Key: "enabled", Label: "Enabled", Value: "false"}}, textarea.New())
	f.Choices = map[string][]string{"enabled": {"true", "false"}}
	f.Focus(0, nil)
	props := Props{Width: 80, Height: 24}
	rows, _, _, _ := f.Rows(props)
	if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "[ ] Off") {
		t.Fatal("boolean does not render as a checkbox")
	}
	f.UpdateKey(tea.KeyPressMsg{Code: tea.KeySpace}, props)
	if f.Picker != nil || f.Editor.Value() != "true" {
		t.Fatal("Space did not toggle the checkbox in place")
	}
}

func TestPickerPagingOutsideScrollAndCancel(t *testing.T) {
	f := NewModel([]form.Field{{Key: "model", Value: "original"}}, textarea.New())
	f.Focus(0, nil)
	props := Props{Width: 100, Height: 24}
	f.OpenPicker(props)
	f.Picker.Loading = false
	for i := range 80 {
		f.Picker.Items = append(f.Picker.Items, fmt.Sprintf("model-%02d", i))
	}
	f.Picker.Filter()
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyPgDown}, props)
	if f.Picker.Cursor <= 1 {
		t.Fatal("Page Down did not page")
	}
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl}, props)
	if f.Picker.Cursor != 79 {
		t.Fatal("End did not reach the last result")
	}
	f.Mouse(tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelUp}, props)
	if f.Picker.Cursor != 79 {
		t.Fatal("scroll outside picker changed its selection")
	}
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl}, props)
	if f.Picker.Cursor != 0 {
		t.Fatal("Home did not reach the first result")
	}
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEscape}, props)
	if f.Picker != nil || f.Editor.Value() != "original" {
		t.Fatal("cancel changed the parent draft")
	}
}

func TestPickerActionsOwnFocusAndCancelWithoutChangingDraft(t *testing.T) {
	f := NewModel([]form.Field{{Key: "model", Value: "original"}}, textarea.New())
	f.Focus(0, nil)
	props := Props{Width: 100, Height: 24}
	f.OpenPicker(props)
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyTab}, props)
	f.SyncInputFocus(true)
	if f.Picker.Control != 1 || f.InputOwner() != NoInput || f.Picker.Search.Focused() {
		t.Fatal("Select button did not receive exclusive focus")
	}
	f.UpdateInput(tea.PasteMsg{Content: "unwanted"})
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
	if f.Picker == nil || f.Picker.Search.Value() != "" {
		t.Fatal("disabled Select accepted a loading result or paste reached the filter")
	}
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyTab}, props)
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeySpace}, props)
	if f.Picker != nil || f.Editor.Value() != "original" {
		t.Fatal("Space on Cancel changed the draft or failed to close")
	}
}
