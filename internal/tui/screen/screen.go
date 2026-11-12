// Package screen owns shared terminal controls and presentation. The application
// shell owns Controls; feature screens retain their own execution state.
package screen

import (
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/settings"
)

// Controls holds application-owned menu and overlay state. A shell supplies the
// same controls to its screens; each screen retains its own execution state.
// The shell drains events after input and coordinates focus with their owners.
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
		m.events = append(m.events, Event{Kind: Replaced, Dialog: m.Dialog})
	}
	m.Dialog = &DialogState{Title: title, Lines: append([]string(nil), lines...)}
	m.events = append(m.events, Event{Kind: Opened, Dialog: m.Dialog, First: first})
}

func (m *Controls) Close() {
	if m.Dialog == nil {
		return
	}
	dialog := m.Dialog
	if dialog.Secret != nil {
		dialog.Secret.Clear()
	}
	m.Dialog = nil
	m.events = append(m.events, Event{Kind: Closed, Dialog: dialog})
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
	Table     *TableState
	Secret    *SecretState
	Shortcuts []popup.Shortcut

	Drag         Drag
	Review       bool
	Save         bool
	Form         *FormState
	Title        string
	Lines        []string
	Help         bool
	Page, Offset int
}

// FormState holds the draft only; the opening feature owns its submission.
type FormState struct {
	settings.Model
	SaveCopy bool
}

// TableState keeps presentation and stable-ID actions together for an overlay.
type TableState struct {
	Model                     *table.Model
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
	Width, Height                              int
	Area                                       pane.Rect
	Inset                                      int
	Ready, Blocked, HasApplication, Evaluating bool
	Identity                                   string
	Styles                                     Styles
	Menus, BadgeMenus                          []Menu
	Badges                                     []Badge
	Actions                                    []Action
	Help                                       []HelpSection
}

// DialogLayout is shared by overlay painting and pointer hit testing.
type DialogLayout struct {
	Rows, Lines                    []string
	Tabs                           pane.TabLayout
	Hint                           string
	Inner, Capacity, Width, Height int
}
