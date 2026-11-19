package fields

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/form"
)

func grabDrawnThumb(t *testing.T, f *Model, props Props) {
	t.Helper()
	rows, _, _, _ := f.Rows(props)
	x, y := f.Origin(props)
	for row, line := range rows {
		text := []rune(ansi.Strip(line))
		if len(text) > 0 && text[len(text)-1] == '█' {
			intent, cmd := f.Mouse(tea.MouseClickMsg{X: x + len(text) - 1, Y: y + row, Button: tea.MouseLeft}, props)
			if !f.scroll.Active || intent.Action != None || cmd != nil {
				t.Fatal("drawn thumb did not capture the mouse")
			}
			return
		}
	}
	t.Fatal("fixture has no drawn thumb")
}

func TestFormScrollbarDragsVisibleFieldsAndPreservesDraft(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		f := NewModel(nil, textarea.New())
		f.Editor.SetHeight(1)
		for i := range 30 {
			key := fmt.Sprintf("field-%02d", i)
			f.Fields = append(f.Fields, form.Field{Key: key, Label: key, Value: "original"})
		}
		f.Hidden = map[string]bool{"field-01": true, "field-29": true}
		f.Focus(0, f.FieldVisible)
		f.Editor.SetValue("unfinished edit")
		props := Props{Width: width, Height: 18, Left: 3, Top: 4}
		grabDrawnThumb(t, &f, props)

		intent, _ := f.Mouse(tea.MouseMotionMsg{X: -5, Y: 100, Button: tea.MouseLeft}, props)
		l := f.plainLayout(props)
		if intent.Action != None || l.Scroll.Offset != l.Scroll.Total-l.Scroll.Height || f.Values()["field-00"] != "unfinished edit" {
			t.Fatal("drag did not reach the last visible fields or lost the draft", width, l.Scroll.Offset, f.Index)
		}
		f.Mouse(tea.MouseMotionMsg{X: 0, Y: -5, Button: tea.MouseLeft}, props)
		f.Mouse(tea.MouseReleaseMsg{X: 0, Y: -5}, props)
		if f.scroll.Active || f.plainLayout(props).Scroll.Offset != 0 {
			t.Fatal("drag did not clamp at the top and release", width)
		}
	}
}

func TestPickerScrollbarDragsWithoutAcceptingChoice(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		f := NewModel([]form.Field{{Key: "choice", Value: "item 00"}}, textarea.New())
		items := []string{}
		for i := range 60 {
			items = append(items, fmt.Sprintf("item %02d", i))
		}
		f.Choices = map[string][]string{"choice": items}
		f.Focus(0, nil)
		props := Props{Width: width, Height: 20, Left: 2, Top: 3}
		f.OpenPicker(props)
		grabDrawnThumb(t, &f, props)
		f.Mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft}, props)
		l := f.layoutPicker(props)
		if l.scroll.Offset != len(items)-l.capacity || f.Editor.Value() != "item 00" {
			t.Fatal("picker did not scroll to the end or accepted the choice", width)
		}
		intent, _ := f.Mouse(tea.MouseReleaseMsg{X: 0, Y: 100}, props)
		if f.scroll.Active || f.Picker == nil || intent.Action != None {
			t.Fatal("release closed picker or retained capture")
		}

		grabDrawnThumb(t, &f, props)
		props.Height = 5
		f.Mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft}, props)
		if f.scroll.Active || f.Picker == nil {
			t.Fatal("resize retained an invisible drag or lost picker")
		}
	}
}

func TestFeedbackScrollbarReachesCompleteErrorAndReturnsToDraft(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		f := NewModel([]form.Field{{Key: "name", Label: "Name", Value: "old"}}, textarea.New())
		f.Focus(0, nil)
		f.Editor.SetValue("unfinished edit")
		f.Error = strings.Repeat("Detailed explanation with a remedy. ", 80) + "FINAL REMEDY"
		props := Props{Width: width, Height: 20, Left: 5, Top: 3}
		f.UpdateKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, props)
		grabDrawnThumb(t, &f, props)
		f.Mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft}, props)
		f.Mouse(tea.MouseReleaseMsg{X: 0, Y: 100}, props)
		rows, _, _, _ := f.Rows(props)
		if f.scroll.Active || !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "FINAL REMEDY") {
			t.Fatal("feedback drag could not reach final remedy", width)
		}
		f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEscape}, props)
		if f.FeedbackOpen || f.Index != 0 || f.Editor.Value() != "unfinished edit" {
			t.Fatal("feedback drag changed the parent draft")
		}
	}
}
