package settings

import (
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type Styles struct{ Muted, Focus, Error, Selected lipgloss.Style }
type Props struct {
	Width, Height int
	Left, Top     int
	Title         string
	Styles        Styles
	Surface       popup.Styles
	SearchStyles  textinput.Styles
}

func (p Props) formStyles() form.Styles {
	return form.Styles{Muted: p.Styles.Muted, Focus: p.Styles.Focus, Error: p.Styles.Error}
}

// Rows draws the section's fields.
//
// It delegates to the shared form component rather than rendering its own.
// The two were the same skeleton - title, hint, label/value pairs, actions,
// rule, key hints - with four small differences, which is how the settings
// modal came to look like nothing else in the app. Those four are now
// optional hooks on form.Props, so every form draws the same way and the
// editor states what it wants instead of drawing it.
func (f *Model) Rows(props Props) ([]string, []form.Hit, int, int) {
	if f.Picker != nil {
		return f.PickerRows(props)
	}
	if f.Workspace == nil {
		return f.PlainRows(props)
	}
	rows, hits := f.Model.Rows(form.Props{
		Title:        props.Styles.Focus.Render("Settings / " + f.sectionLabel()),
		Hint:         Hint(f.Workspace.Section),
		Error:        f.Error,
		PrimaryLabel: f.ApplyLabel,
		Width:        props.Width,
		Height:       props.Height,
		Choice:       f.choice(),
		Styles:       props.formStyles(),
		Visible:      f.FieldVisible,
		Label:        settingsLabel,
		Display:      f.display,
		Note:         f.note(),
		Footer:       "Tab controls · Ctrl+S apply · Esc cancel",
		// Nothing to save is nothing to apply. Offering a live Apply on an
		// unchanged form invites a relaunch that changes nothing.
		DisablePrimary: !f.Dirty(),
	})
	return rows, hits, props.Width, props.Height
}

// sectionLabel names the section for the title line.
func (f *Model) sectionLabel() string {
	if f.Workspace.Section == "application" {
		return "Application"
	}
	for _, c := range categories {
		if c.ID == f.Workspace.Section {
			return c.Label
		}
	}
	return ""
}

// choice is the label of the focused field's fixed options, if it has any.
func (f *Model) choice() string {
	if f.Index < len(f.Fields) && len(f.Options(f.Fields[f.Index].Key)) > 0 {
		return ChoiceLabel(f.Fields[f.Index].Key, f.Editor.Value())
	}
	return ""
}

// note is the line above the buttons when nothing has gone wrong.
func (f *Model) note() string {
	switch {
	case f.Error != "":
		return ""
	case f.Workspace.Section == "generation":
		return "Temperature: default uses " + strconv.FormatFloat(DefaultTemperature, 'g', -1, 64) + " · F4 Advanced"
	case f.Workspace.Section == "profiles":
		return "Ctrl+D saves a copy · Apply uses the loaded profile in this session"
	case f.Dirty():
		return "Apply saves a custom profile · F4 Advanced"
	default:
		return "No changes · F4 Advanced"
	}
}

// settingsLabel renames the two fields whose stored key says less than the
// thing it describes.
func settingsLabel(field form.Field) string {
	switch field.Key {
	case "endpoint":
		return "Server address"
	case "kind":
		return "Protocol"
	}
	return ""
}

// display renders a field's value row and reports whether it still opens an
// editor. A field that opens a chooser - a file, a set of fixed options, a
// discovery - is not edited in place, so it stays a value row even while it
// is the focused one, and says what pressing it will do.
func (f *Model) display(field form.Field, value string) (string, bool) {
	if len(f.Options(field.Key)) > 0 {
		return "[ " + ChoiceLabel(field.Key, value) + " ▾ ]", false
	}
	switch {
	case PickerFiles(field.Key):
		shown := "Not selected"
		if value != "" {
			// A full path is noise in a two-column row; the last
			// element is what the reader recognises.
			shown = filepath.Base(value)
		}
		return shown + "  [ Choose… ]", false
	case field.Key == "model":
		return value + "  [ Discover… ]", false
	}
	return value, true
}

func (f *Model) PlainRows(props Props) ([]string, []form.Hit, int, int) {
	w, h := props.Width, props.Height
	choice := ""
	if f.Index < len(f.Fields) && len(f.Options(f.Fields[f.Index].Key)) > 0 {
		choice = ChoiceLabel(f.Fields[f.Index].Key, f.Editor.Value())
	}
	rows, hits := f.Model.Rows(form.Props{Title: props.Title, Hint: f.Hint, Error: f.Error, PrimaryLabel: f.ApplyLabel, Width: w, Height: h, Destructive: f.Destructive, Choice: choice, Styles: props.formStyles()})
	return rows, hits, w, h
}

// box is the framed, centred surface every form is drawn on - the settings
// section editor, the confirmation, the file chooser. They are the same
// surface with different rows, so they are the same box.
//
// It is sized to its content and centred, like every other overlay in the
// app. The settings editor's own frame used to stretch to the whole screen,
// which put a handful of fields in the top eighth of the terminal and left a
// void that read as a failure to draw rather than as space.
func (f *Model) box(props Props) []string {
	rows, _, _, _ := f.Rows(props)
	return popup.Box(rows, props.Surface)
}

// Origin is the screen position of the first content cell inside the frame:
// row 0 of the rows, column 0 of a row's text.
//
// The renderer and the hit-test both read it, and callers can too. They used
// to compute it separately and drifted apart twice - once when the rows lost
// the category rail and the frame kept it, and once when the frame moved and
// the caller did not. Every time they disagreed, buttons were drawn correctly
// and clicked in the wrong place, and only a human looking at the screen could
// tell.
func (f *Model) Origin(props Props) (x, y int) {
	framed := f.box(props)
	width := cell.Width(strings.Join(framed, "\n"))
	return props.Left + max((max(props.Width, 0)-width)/2, 0) + 2,
		props.Top + max((max(props.Height, 0)-len(framed))/2, 0) + 1
}

// View draws the form on its shared surface.
func (f *Model) View(props Props) string {
	rows, _, _, _ := f.Rows(props)
	return popup.FrameBox(rows, props.Width, props.Height, props.Surface)
}
