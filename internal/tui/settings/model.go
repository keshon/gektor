package settings

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/tui/components/form"
)

type Workspace struct {
	Draft                    *Draft
	SampleID                 uint64
	SampleCancel             context.CancelFunc
	Section, LoadedPath      string
	// VoiceMode is the voice input mode as it was when the form opened.
	// Opening a settings page stops microphone capture, so the live mode
	// afterwards no longer describes what the reader had; persisting that
	// silently downgraded hands-free to push-to-talk on every Apply.
	VoiceMode                string
	AutoSaved, Nav, Advanced bool
}
type Model struct {
	form.Model
	Error, Hint, ApplyLabel   string
	Destructive               bool
	Choices                   map[string][]string
	ScannedRoot, GreetingPath string
	Workspace                 *Workspace
	Picker                    *Picker
	HasApplication, Chat      bool
}
type Category struct{ ID, Label string }

var categories = []Category{{"model", "Model"}, {"generation", "Generation"}, {"speech", "Speech"}, {"profiles", "Profiles"}, {"application", "Application"}, {"character", "Character"}, {"you", "You"}, {"prompts", "Prompts"}}

type InputOwner int

const (
	NoInput InputOwner = iota
	FieldInput
	PickerInput
)

func NewModel(fields []form.Field, editor textarea.Model) Model {
	m := Model{Model: form.New(fields, editor)}
	m.RefreshChoices()
	return m
}

func (f *Model) InputOwner() InputOwner {
	if f.Picker != nil {
		return PickerInput
	}
	if f.Workspace != nil && f.Workspace.Nav {
		return NoInput
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
	if index < len(f.Fields) && f.Workspace != nil && f.Workspace.Section == "character" && f.Fields[index].Key == "name" {
		for _, field := range f.Fields {
			if field.Key == "character" && strings.TrimSpace(field.Value) != "" {
				return false
			}
		}
	}
	if f.Workspace != nil && f.Workspace.Section == "speech" && index < len(f.Fields) {
		switch f.Fields[index].Key {
		case "vad", "mic-start", "mic-end", "mic-threshold":
			return f.Workspace.Advanced
		}
	}
	if index >= len(f.Fields) || f.Workspace == nil || f.Workspace.Section != "model" {
		return true
	}
	key := f.Fields[index].Key
	managed := false
	for _, field := range f.Fields {
		if field.Key == "ownership" {
			v := field.Value
			if f.Index < len(f.Fields) && f.Fields[f.Index].Key == "ownership" {
				v = f.Editor.Value()
			}
			managed = v == "managed"
		}
	}
	if key == "ownership" {
		return true
	}
	if managed {
		switch key {
		case "model-file", "gpu-layers":
			return true
		case "runtime", "model-root", "endpoint":
			return f.Workspace.Advanced
		default:
			return false
		}
	}
	switch key {
	case "runtime", "model-root", "model-file", "gpu-layers":
		return false
	case "key-env":
		return f.Workspace.Advanced
	default:
		return true
	}
}

func (f *Model) FirstVisibleField() int {
	for i := range f.Fields {
		if f.FieldVisible(i) {
			return i
		}
	}
	return len(f.Fields)
}

// RefreshChoices runs during updates, never while rendering.
func (f *Model) RefreshChoices() {
	hasGreeting := false
	for _, field := range f.Fields {
		if field.Key == "greeting" {
			hasGreeting = true
			break
		}
	}
	if !hasGreeting {
		return
	}
	path := ""
	for _, field := range f.Fields {
		if field.Key == "character" {
			path = field.Value
		}
	}
	if f.Choices == nil {
		f.Choices = map[string][]string{}
	}
	if path != f.GreetingPath || len(f.Choices["greeting"]) == 0 {
		f.GreetingPath = path
		f.Choices["greeting"] = []string{"0"}
		if card, err := character.Load(path); err == nil {
			for i := range card.AlternateGreetings {
				f.Choices["greeting"] = append(f.Choices["greeting"], strconv.Itoa(i+1))
			}
		}
	}

}
func (f *Model) Options(key string) []string {
	if choices := f.Choices[key]; len(choices) > 0 {
		return choices
	}
	return Choices(key)
}
func (f *Model) Focus(index int, visible func(int) bool) {
	f.Model.Focus(index, visible)
	f.RefreshChoices()
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

func (f *Model) HasSpeech() bool { return f.Workspace != nil && f.Workspace.Draft.Base().Speech != nil }
func (f *Model) AvailableCategories() []Category {
	var out []Category
	for _, c := range categories {
		if c.ID == "application" && !f.HasApplication {
			continue
		}
		if c.ID == "speech" && !f.HasSpeech() {
			continue
		}
		// Cards belong to Chat. An Agent never reads one, so the page
		// would be a setting that saves a value nothing consumes.
		if c.ID == "character" && !f.Chat {
			continue
		}
		out = append(out, c)
	}
	return out
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
