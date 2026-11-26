package fields

import (
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

type Styles struct {
	Title, Muted, Focus, Disabled, Error, Selected, Button lipgloss.Style
	FilledButtons                                          bool
}
type Props struct {
	Width, Height int
	Left, Top     int
	Title         string
	Styles        Styles
	Surface       popup.Styles
	SearchStyles  textinput.Styles
}

func (p Props) formStyles() form.Styles {
	return form.Styles{Normal: p.Surface.Normal, Title: p.Styles.Title, Disabled: p.Styles.Disabled, Muted: p.Styles.Muted, Focus: p.Styles.Focus, Error: p.Styles.Error, Scrollbar: p.Surface.Normal, Button: p.Styles.Button, FilledButtons: p.Styles.FilledButtons}
}

func (f *Model) Rows(props Props) ([]string, []form.Hit, int, int) {
	if f.FeedbackOpen {
		return f.feedbackRows(props)
	}
	if f.Picker != nil {
		return f.PickerRows(props)
	}
	return f.PlainRows(props)
}

// Choosers retain a value row while focused instead of opening an inline editor.
func (f *Model) display(field form.Field, value string, styles form.Styles) (string, bool) {
	if len(f.Options(field.Key)) > 0 {
		return f.fieldButton(field, f.ChoiceLabel(field.Key, value)+" ▾", styles), false
	}
	switch {
	case f.FilePicker(field.Key):
		shown := "Not selected"
		if value != "" {
			// A full path is noise in a two-column row; the last
			// element is what the reader recognises.
			shown = filepath.Base(value)
		}
		return shown + "  " + f.fieldButton(field, "Choose…", styles), false
	case f.DiscoveryFields[field.Key] != "":
		button := f.fieldButton(field, "Discover…", styles)
		if value == "" {
			return button, false
		}
		return value + "  " + button, false
	}
	return value, true
}

func (f *Model) fieldButton(field form.Field, label string, styles form.Styles) string {
	text := form.ButtonLabel(label, cell.Width(label)+4, styles)
	if !styles.FilledButtons {
		return text
	}
	focused := f.Index >= 0 && f.Index < len(f.Fields) && f.Fields[f.Index].Key == field.Key
	return form.Button(text, focused, true, false, styles)
}

func (f *Model) PlainRows(props Props) ([]string, []form.Hit, int, int) {
	l := f.plainLayout(props)
	return l.Rows, l.Hits, props.Width, props.Height
}

func (f *Model) plainLayout(props Props) form.Layout {
	if f.Title != "" {
		props.Title = f.Title
	}
	w, h := props.Width, props.Height
	feedbackHint := ""
	if f.HasFeedback() {
		feedbackHint = "Ctrl+E error details"
	}
	choice := ""
	if f.Index < len(f.Fields) && len(f.Options(f.Fields[f.Index].Key)) > 0 {
		choice = f.ChoiceLabel(f.Fields[f.Index].Key, f.Editor.Value())
	}
	styles := props.formStyles()
	appearance := form.Props{
		Title:        props.Title,
		Hint:         f.Hint,
		Error:        f.Error,
		FieldErrors:  f.FieldErrors,
		FeedbackHint: feedbackHint,
		PrimaryLabel: f.ApplyLabel,
		Width:        w,
		Height:       h,
		Destructive:  f.Destructive,
		Choice:       choice,
		Styles:       styles,
		Options:      f.inlineOptions,
		Visible:      f.FieldVisible,
		Label:        f.fieldLabel,
		Display: func(field form.Field, value string) (string, bool) {
			return f.display(field, value, styles)
		},
		Note:           f.Note,
		Shortcuts:      f.Shortcuts,
		DisablePrimary: f.DisablePrimary,
	}
	return f.Model.Layout(appearance)
}

func (f *Model) box(props Props) []string {
	rows, _, _, _ := f.Rows(props)
	return popup.ModalBox(rows, props.Width, 0, props.Surface)
}

// Origin is shared by rendering and hit testing; coordinates include Props.Left and Top.
func (f *Model) Origin(props Props) (x, y int) {
	framed := f.box(props)
	width := cell.Width(strings.Join(framed, "\n"))
	return props.Left + max((max(props.Width, 0)-width)/2, 0) + 2,
		props.Top + max((max(props.Height, 0)-len(framed))/2, 0) + 1
}

// View draws the form on its shared surface.
func (f *Model) View(props Props) string {
	framed := f.box(props)
	width := cell.Width(strings.Join(framed, "\n"))
	indent := strings.Repeat(" ", max((props.Width-width)/2, 0))
	for i := range framed {
		framed[i] = indent + framed[i]
	}
	return strings.Repeat("\n", max((props.Height-len(framed))/2, 0)) + strings.Join(framed, "\n")
}

// Overlay draws the same form at the same origin over the existing document.
func (f *Model) Overlay(background string, props Props) string {
	framed := f.box(props)
	width := cell.Width(strings.Join(framed, "\n"))
	return popup.Overlay(background, framed, max((props.Width-width)/2, 0), max((props.Height-len(framed))/2, 0), width, props.Width)
}

func (f *Model) fieldLabel(field form.Field) string {
	return f.FieldLabels[field.Key]
}

// Cursor shares the rendered field or picker origin with pointer hit testing.
func (f *Model) Cursor(props Props) *tea.Cursor {
	if f.FeedbackOpen {
		return nil
	}
	var caret *tea.Cursor
	if f.Picker != nil {
		picker := f.Picker
		if picker.Control != 0 || picker.NoSearch {
			return nil
		}
		layout := f.layoutPicker(props)
		caret = layout.cursor
	} else {
		caret = f.plainLayout(props).Cursor
	}
	if caret == nil {
		return nil
	}
	x, y := f.Origin(props)
	caret.X += x
	caret.Y += y
	return caret
}
