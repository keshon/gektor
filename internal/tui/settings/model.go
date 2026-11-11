package settings

import (
	"slices"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/tui/components/form"
)

type EditSession struct {
	Draft               *Draft
	Section, LoadedPath string
	Advanced            bool
	// Explicit Apply may persist launch overrides even without field edits.
	PersistCurrent bool
	AccountLabel   string
}
type FilePicker struct {
	Root       string
	Extensions []string
}

func (f *Model) FilePicker(key string) bool {
	_, ok := f.FileFields[key]
	return ok || PickerFiles(key)
}
func (f *Model) PickerControl(key string) bool { return f.FilePicker(key) || PickerControl(key) }

type Model struct {
	form.Model
	Error, Hint, ApplyLabel string
	Destructive             bool
	Choices                 map[string][]string
	FileFields              map[string]FilePicker
	EditSession             *EditSession
	Picker                  *Picker
	HasApplication          bool
}
type Category struct{ ID, Label string }

var categories = []Category{{"model", "Model connection"}, {"generation", "Response settings"}, {"profiles", "Launch profiles"}, {"workspace", "Workspace"}, {"you", "Identity and persona"}, {"prompts", "Agent instructions"}}

func SectionTitle(id string) string {
	for _, category := range categories {
		if category.ID == id {
			return category.Label
		}
	}
	return "Settings"
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
	if f.Picker != nil {
		if f.Picker.Field == "theme" || f.Picker.Control > 0 {
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
	if index >= 0 && index < len(f.Fields) && f.EditSession != nil {
		if d, err := providers.Lookup(f.providerKind()); err == nil {
			switch f.Fields[index].Key {
			case "reasoning-effort", "reasoning-summary":
				return d.Reasoning
			case "temperature":
				if !d.Sampling {
					return false
				}
			case "max":
				if !d.OutputLimit {
					return false
				}
			case "key-env":
				if d.Auth != providers.Bearer {
					return false
				}
			case "endpoint":
				if d.FixedURL != "" {
					return false
				}
			}
		}
	}
	if index >= len(f.Fields) || f.EditSession == nil || f.EditSession.Section != "model" {
		return true
	}
	if f.Fields[index].Key == "key-env" {
		return f.EditSession.Advanced
	}
	return true
}

func (f *Model) providerKind() string {
	if f.EditSession == nil {
		return ""
	}
	if f.EditSession.Section == "model" {
		for _, field := range f.Fields {
			if field.Key == "kind" {
				return field.Value
			}
		}
	}
	if fields, ok := f.EditSession.Draft.Fields("model"); ok {
		for _, field := range fields {
			if field.Key == "kind" {
				return field.Value
			}
		}
	}
	return f.EditSession.Draft.Base().Provider.Kind
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
	previous := f.Editor.Value()
	if f.Fields[f.Index].Key == "kind" {
		for i := range f.Fields {
			if f.Fields[i].Key == "endpoint" {
				f.Fields[i].Value = SuggestedBackendURL(f.Fields[i].Value, previous, next)
			}
		}
	}
	f.Editor.SetValue(next)
	f.Fields[f.Index].Value = next
}

// Short, fixed choices stay in their form. Discovery and file lists keep their
// searchable picker, regardless of how many results they currently contain.
func (f *Model) inlineOptions(field form.Field) []form.Option {
	if field.ReadOnly || f.PickerControl(field.Key) {
		return nil
	}
	values := f.Options(field.Key)
	if len(values) == 0 || (len(values) > 4 && field.Key != "theme") {
		return nil
	}
	options := make([]form.Option, 0, len(values))
	for _, value := range values {
		label := ChoiceLabel(field.Key, value)
		if len([]rune(label)) > 24 {
			return nil
		}
		options = append(options, form.Option{Value: value, Label: label})
	}
	return options
}

func (f *Model) StorePage() { f.Sync(); f.EditSession.Draft.Store(f.EditSession.Section, f.Fields) }
func (f *Model) Dirty() bool {
	if f.EditSession == nil {
		return true
	}
	fields := slices.Clone(f.Fields)
	if f.Index >= 0 && f.Index < len(fields) {
		fields[f.Index].Value = f.Editor.Value()
	}
	return f.EditSession.PersistCurrent || f.EditSession.Draft.Dirty(f.EditSession.Section, fields)
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
