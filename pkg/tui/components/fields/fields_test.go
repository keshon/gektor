package fields

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/form"
)

func TestFieldNamesDoNotSelectApplicationBehavior(t *testing.T) {
	for _, key := range []string{"model", "kind", "theme", "app-workspace", "load-profile", "reasoning-effort"} {
		f := NewModel([]form.Field{{Key: key, Label: "Plain field", Value: "typed value"}}, textarea.New())
		f.Focus(0, nil)
		intent, cmd := f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}, Props{})
		if intent.Action != None || cmd != nil || f.Picker != nil || f.Editor.Value() != "typed value" {
			t.Fatalf("field %q acquired application behavior", key)
		}
		if len(f.Options(key)) != 0 || !f.FieldVisible(0) || f.FieldLabels[key] != "" {
			t.Fatalf("field %q acquired implicit presentation", key)
		}
	}
}

func TestCompleteFeedbackIsReachableWithoutLosingTheDraft(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		f := NewModel([]form.Field{{Key: "name", Label: "Name", Value: "original"}}, textarea.New())
		f.Focus(0, nil)
		f.Editor.SetValue("unfinished edit")
		f.Error = strings.Repeat("A longer explanation. ", 60) + "FINAL REMEDY"
		f.FieldErrors = map[string]string{"name": "Choose a different name."}
		props := Props{Width: width, Height: 20}
		rows, _, _, _ := f.Rows(props)
		if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "Ctrl+E") {
			t.Fatal("truncated feedback has no disclosure")
		}

		f.UpdateKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, props)
		f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnd}, props)
		rows, _, _, _ = f.Rows(props)
		text := ansi.Strip(strings.Join(rows, "\n"))
		if !strings.Contains(text, "FINAL REMEDY") || !strings.Contains(text, "Choose a different name.") {
			t.Fatalf("last feedback is unreachable at width %d: %s", width, text)
		}
		if f.InputOwner() != NoInput || len(rows)+2 > props.Height {
			t.Fatal("feedback kept an editor active or exceeded its area")
		}

		f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEscape}, props)
		if f.FeedbackOpen || f.Index != 0 || f.Editor.Value() != "unfinished edit" || f.InputOwner() != FieldInput {
			t.Fatal("returning from feedback lost editing context")
		}
	}
}
