package screen

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/settings"
)

// Controls holds application-owned menu and overlay state. A shell supplies the
// same controls to its screens; each screen retains its own execution state.
// Callbacks run on the update loop. Opening receives true for the first overlay;
// Closed restores its focus owner, and Changed refits the underlying screen.
type Controls struct {
	Props   ControlProps
	Opening func(first bool)
	Closed  func()
	Changed func()
	Header  MenuState
	Dialog  *DialogState
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

// DialogState is an informational, form, or table overlay. Callbacks belong to
// the feature that opened it and run on the terminal update loop.
type DialogState struct {
	Table              *TableState
	Shortcuts          []popup.Shortcut
	OnClose, OnSuspend func()
	Drag               Drag
	Review             func()
	Save               func() error
	Form               *FormState
	Title              string
	Lines              []string
	Help               bool
	Page, Offset       int
}

// FormState combines a reusable editor with the operation its owner supplies.
type FormState struct {
	settings.Model
	Apply    func(map[string]string) error
	SaveCopy func()
}

// TableState keeps presentation and stable-ID actions together for an overlay.
type TableState struct {
	Model                     *table.Model
	List                      bool
	Refresh                   func()
	Columns                   []table.Column
	Description, EmptyMessage string
	Actions                   func() []table.Action
	Activate                  func(action, id string) tea.Cmd
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
