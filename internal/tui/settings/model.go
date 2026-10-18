package settings

import (
	"slices"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/tui/components/form"
)

type Workspace struct {
	Draft               *Draft
	Section, LoadedPath string
	Advanced            bool
}
type Model struct {
	form.Model
	Error, Hint, ApplyLabel string
	Destructive             bool
	Choices                 map[string][]string
	Workspace               *Workspace
	Picker                  *Picker
	HasApplication          bool
}
type Category struct{ ID, Label string }

var categories = []Category{{"model", "Model"}, {"generation", "Generation"}, {"profiles", "Profiles"}, {"application", "Application"}, {"you", "You"}, {"prompts", "Prompts"}}

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
	if f.Picker != nil {
		return PickerInput
	}
	if f.Index >= 0 && f.Index < len(f.Fields) {
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
	if index >= len(f.Fields) || f.Workspace == nil || f.Workspace.Section != "model" {
		return true
	}
	if f.Fields[index].Key == "key-env" {
		return f.Workspace.Advanced
	}
	return true
}

func (f *Model) FirstVisibleField() int {
	for i := range f.Fields {
		if f.FieldVisible(i) {
			return i
		}
	}
	return len(f.Fields)
}

func (f *Model) Options(key string) []string {
	if choices := f.Choices[key]; len(choices) > 0 {
		return choices
	}
	return Choices(key)
}
func (f *Model) Focus(index int, visible func(int) bool) {
	f.Model.Focus(index, visible)
}
func (f *Model) CycleChoice() {
	options := f.Options(f.Fields[f.Index].Key)
	if len(options) == 0 {
		return
	}
	previous := f.Editor.Value()
	next := options[0]
	for i, value := range options {
		if value == previous {
			next = options[(i+1)%len(options)]
			break
		}
	}
	if f.Fields[f.Index].Key == "kind" {
		for i := range f.Fields {
			if f.Fields[i].Key == "endpoint" {
				f.Fields[i].Value = SuggestedBackendURL(f.Fields[i].Value, previous, next)
			}
		}
	}
	f.Editor.SetValue(next)
}

func (f *Model) StorePage() { f.Sync(); f.Workspace.Draft.Store(f.Workspace.Section, f.Fields) }
func (f *Model) Dirty() bool {
	if f.Workspace == nil {
		return true
	}
	fields := slices.Clone(f.Fields)
	if f.Index >= 0 && f.Index < len(fields) {
		fields[f.Index].Value = f.Editor.Value()
	}
	return f.Workspace.Draft.Dirty(f.Workspace.Section, fields)
}
func (f *Model) UpdateInput(msg tea.Msg) tea.Cmd {
	switch f.InputOwner() {
	case PickerInput:
		return f.Picker.UpdateInput(msg)
	case FieldInput:
		var cmd tea.Cmd
		f.Editor, cmd = f.Editor.Update(msg)
		return cmd
	}
	return nil
}
