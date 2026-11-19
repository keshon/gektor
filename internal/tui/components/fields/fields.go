// Package fields owns form drafts and pickers. Applications supply choices and
// discovery results; the controls own editing and report submission intent.
package fields

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type FilePicker struct {
	Root       string
	Extensions []string
	Directory  bool
	Recursive  bool
}

func (f *Model) FilePicker(key string) bool {
	_, ok := f.FileFields[key]
	return ok
}
func (f *Model) PickerControl(key string) bool {
	return f.FilePicker(key) || f.DiscoveryFields[key] != ""
}

type Model struct {
	form.Model
	Title, Error, Hint, ApplyLabel string
	Destructive                    bool
	Choices                        map[string][]string
	ShowWhen                       map[string][]form.Condition
	FileFields                     map[string]FilePicker
	Picker                         *Picker
	DisablePrimary                 bool
	Note                           string
	Shortcuts                      []popup.Shortcut
	Hidden                         map[string]bool
	Labels                         map[string]map[string]string
	FieldLabels                    map[string]string
	DiscoveryFields                map[string]string
	NoSearchFields                 map[string]bool
	InlineChoices                  map[string]int
	FieldErrors                    map[string]string
	FeedbackOpen                   bool
	FeedbackOffset                 int
	scroll                         popup.ScrollDrag
}
type InputOwner int

const (
	NoInput InputOwner = iota
	FieldInput
	PickerInput
)

func NewModel(fields []form.Field, editor textarea.Model) Model {
	m := Model{Model: form.New(fields, editor)}
	return m
}

func (f *Model) InputOwner() InputOwner {
	if f.FeedbackOpen {
		return NoInput
	}
	if f.Picker != nil {
		if f.Picker.NoSearch || f.Picker.Control > 0 {
			return NoInput
		}

		return PickerInput
	}

	if f.Index >= 0 && f.Index < len(f.Fields) {
		if len(f.Options(f.Fields[f.Index].Key)) > 0 {
			return NoInput
		}

		return FieldInput
	}

	return NoInput
}

func (f *Model) SyncInputFocus(active bool) {
	if active && f.InputOwner() == FieldInput {
		if !f.Editor.Focused() {
			f.Editor.Focus()
		}
	} else {
		f.Editor.Blur()
	}

	if f.Picker != nil {
		if active && f.InputOwner() == PickerInput {
			if !f.Picker.Search.Focused() {
				f.Picker.Search.Focus()
			}
		} else {
			f.Picker.Search.Blur()
		}
	}
}

func (f *Model) FieldVisible(index int) bool {
	if index >= 0 && index < len(f.Fields) {
		for _, condition := range f.ShowWhen[f.Fields[index].Key] {
			matches := false
			for _, field := range f.Fields {
				if field.Key == condition.Key && field.Value == condition.Value {
					matches = true
					break
				}
			}
			if !matches {
				return false
			}
		}
	}

	return index < 0 || index >= len(f.Fields) || !f.Hidden[f.Fields[index].Key]
}

func (f *Model) FirstVisibleField() int {
	for i := range f.Fields {
		if f.FieldVisible(i) {
			return i
		}
	}

	return len(f.Fields)
}

func (f *Model) Options(key string) []string { return f.Choices[key] }

func (f *Model) ChoiceLabel(key, value string) string {
	if label, ok := f.Labels[key][value]; ok {
		return label
	}
	return value
}

func (f *Model) Focus(index int, visible func(int) bool) {
	f.Model.Focus(index, visible)
	if f.InputOwner() != FieldInput {
		f.Editor.Blur()
	}
}
func (f *Model) CycleChoice() {
	f.cycleChoice(1)
}

func (f *Model) cycleChoice(step int) {
	options := f.Options(f.Fields[f.Index].Key)
	if len(options) == 0 {
		return
	}

	previous := f.Editor.Value()
	next := options[0]

	for i, value := range options {
		if value == previous {
			next = options[(i+step+len(options))%len(options)]
			break
		}
	}

	f.setChoice(next)
}

func (f *Model) setChoice(next string) {
	f.Editor.SetValue(next)
	f.Fields[f.Index].Value = next
}

func (f *Model) inlineOptions(field form.Field) []form.Option {
	if field.ReadOnly || f.PickerControl(field.Key) {
		return nil
	}

	values := f.Options(field.Key)
	limit := max(4, f.InlineChoices[field.Key])
	if len(values) == 0 || len(values) > limit {
		return nil
	}

	options := make([]form.Option, 0, len(values))

	for _, value := range values {
		label := f.ChoiceLabel(field.Key, value)
		if len([]rune(label)) > 24 {
			return nil
		}

		options = append(options, form.Option{Value: value, Label: label})
	}

	return options
}

func (f *Model) UpdateInput(msg tea.Msg) tea.Cmd {
	switch f.InputOwner() {
	case PickerInput:
		return f.Picker.UpdateInput(msg)
	case FieldInput:
		if f.Fields[f.Index].ReadOnly {
			return nil
		}

		var cmd tea.Cmd
		f.Editor, cmd = f.Editor.Update(msg)

		return cmd
	}

	return nil
}
