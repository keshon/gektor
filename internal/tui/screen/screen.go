// Package screen owns shared terminal controls and presentation. The application
// shell owns Controls; feature screens retain their own execution state.
package screen

import (
	"github.com/charmbracelet/colorprofile"

	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
)

// Controls holds application-owned menu and overlay state. A shell supplies the
// same controls to its screens; each screen retains its own execution state.
// The shell drains events after input and coordinates focus with their owners.
// Controls must not be copied after first use; IDs belong to this owner.
type Controls struct {
	Props  ControlProps
	events []Event
	Header MenuState
	Dialog *DialogState
}

// Open copies the content and replaces the current overlay. Replacement does not
// restore underlying focus; the shell observes one continuous overlay lifetime.
func (m *Controls) Open(title string, lines []string) {
	first := m.Dialog == nil
	if m.Dialog != nil {
		if m.Dialog.Secret != nil {
			m.Dialog.Secret.Clear()
		}
		m.events = append(m.events, Event{Kind: Replaced, ID: m.DialogID()})
	}
	m.Dialog = &DialogState{Title: title, Lines: append([]string(nil), lines...)}
	m.events = append(m.events, Event{Kind: Opened, ID: m.DialogID(), First: first})
}

func (m *Controls) Close() {
	if m.Dialog == nil {
		return
	}
	dialog := m.Dialog
	id := m.DialogID()
	if dialog.Secret != nil {
		dialog.Secret.Clear()
	}
	m.Dialog = nil
	m.events = append(m.events, Event{Kind: Closed, ID: id})
}

// MenuState tracks focus and selection in the application menu or a badge popup.
type MenuState struct {
	Global, Focused bool
	Index           int
	Menu            string
	Row             int
}

// Drag tracks a scrollbar thumb grab in terminal rows.
type Drag struct {
	Active bool
	Grab   int
	Moved  bool
}

// DialogState holds controls-owned presentation state. Its identity is retained
// in events so owners can discard results from replaced overlays.
type DialogState struct {
	suspended bool
	Table     *TableState
	Secret    *SecretState
	Shortcuts []popup.Shortcut

	Drag           popup.ScrollDrag
	Actions        []DialogAction
	Form           *FormState
	Title          string
	PreferredWidth int
	Maximized      bool
	Lines          []string
	Help           bool
	Page, Offset   int
}

// FormState holds the draft only; the opening feature owns its submission.
type FormState struct {
	fieldform.Model
	SaveCopy bool
}

// TableState keeps presentation and stable-ID actions together for an overlay.
type TableState struct {
	model                     *table.Model
	List                      bool
	Columns                   []table.Column
	Description, EmptyMessage string
	Actions                   []table.Action
}

// HelpSection supplies content for a tabbed help overlay.
type HelpSection struct {
	Title string
	Rows  [][2]string
}

// ControlProps is the current application presentation, independent of either
// screen's execution state. Area is the region available to shared dialogs.
type ControlProps struct {
	Width, Height     int
	Area              pane.Rect
	Inset             int
	Ready, Blocked    bool
	Palette           *Palette
	ColorProfile      colorprofile.Profile
	Identity          string
	Styles            Styles
	Menus, BadgeMenus []Menu
	Badges            []Badge
	Actions           []Action
	Help              []HelpSection
}

// DialogLayout is shared by overlay painting and pointer hit testing.
type DialogLayout struct {
	Rows, Lines                    []string
	Tabs                           pane.TabLayout
	Hint                           string
	Inner, Capacity, Width, Height int
}
