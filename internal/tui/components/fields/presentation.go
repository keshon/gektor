package fields

import (
	"maps"
	"slices"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// Snapshot copies the edited values and feedback without exposing editor or
// picker navigation state. Taking it synchronizes the focused field first.
type Snapshot struct {
	Fields       []form.Field
	Error        string
	FieldErrors  map[string]string
	PickerID     RequestID
	PickerOpen   bool
	FeedbackOpen bool
}

func (f *Model) Snapshot() Snapshot {
	f.Sync()
	s := Snapshot{Fields: slices.Clone(f.Fields), Error: f.Error, FieldErrors: maps.Clone(f.FieldErrors), FeedbackOpen: f.FeedbackOpen}
	if f.Picker != nil {
		s.PickerOpen, s.PickerID = true, f.Picker.ID
	}
	return s
}

// Presentation supplies domain choices, visibility and feedback. Values contains
// explicit value corrections; applying it retains focus, cursor and picker state.
type Presentation struct {
	Hint, Note, Error string
	DisablePrimary    bool
	Values            map[string]string
	Choices           map[string][]string
	Labels            map[string]map[string]string
	FieldLabels       map[string]string
	FileFields        map[string]FilePicker
	Hidden            map[string]bool
	DiscoveryFields   map[string]string
	NoSearchFields    map[string]bool
	InlineChoices     map[string]int
	FieldErrors       map[string]string
	Shortcuts         []popup.Shortcut
}

// Configure copies presentation data so later owner mutations cannot change the
// live form. Unchanged focused values do not reset the text cursor or selection.
func (f *Model) Configure(p Presentation) {
	f.Sync()
	for i := range f.Fields {
		value, ok := p.Values[f.Fields[i].Key]
		if !ok || value == f.Fields[i].Value {
			continue
		}
		f.Fields[i].Value = value
		if i == f.Index {
			f.Editor.SetValue(value)
		}
	}
	f.Hint, f.Note, f.Error, f.DisablePrimary = p.Hint, p.Note, p.Error, p.DisablePrimary
	f.Choices = maps.Clone(p.Choices)
	for key, values := range f.Choices {
		f.Choices[key] = slices.Clone(values)
	}
	f.Labels = maps.Clone(p.Labels)
	for key, values := range f.Labels {
		f.Labels[key] = maps.Clone(values)
	}
	f.FileFields = maps.Clone(p.FileFields)
	for key, picker := range f.FileFields {
		picker.Extensions = slices.Clone(picker.Extensions)
		f.FileFields[key] = picker
	}
	f.FieldLabels = maps.Clone(p.FieldLabels)
	f.Hidden = maps.Clone(p.Hidden)
	f.DiscoveryFields = maps.Clone(p.DiscoveryFields)
	f.NoSearchFields = maps.Clone(p.NoSearchFields)
	f.InlineChoices = maps.Clone(p.InlineChoices)
	f.FieldErrors = maps.Clone(p.FieldErrors)
	f.Shortcuts = slices.Clone(p.Shortcuts)
}
