// Package screen defines shared presentation data and overlay requests for
// terminal screens and their application host.
package screen

import (
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/settings"
)

// Host presents overlays. Returned form models belong to that newly opened overlay;
// a screen may use the handle to report validation errors without inspecting the shell.
type Host struct {
	OpenForm        func(Form) *settings.Model
	OpenDialog      func(Dialog)
	CloseDialog     func()
	FinishOperation func()
}

// Form describes an editable overlay and its completion contract.
type Form struct {
	Title, Hint, ApplyLabel    string
	Fields                     []form.Field
	Choices                    map[string][]string
	FileFields                 map[string]settings.FilePicker
	Destructive, StartOnCancel bool
	Apply                      func(map[string]string) error
	OnClose                    func()
}

// Dialog describes an informational overlay, optionally with a save action or table.
type Dialog struct {
	Title     string
	Lines     []string
	Shortcuts []popup.Shortcut
	Save      func() error
	OnClose   func()
	Table     *Table
}

// Table supplies rows through its model and resolves actions by stable row ID.
type Table struct {
	Model       *table.Model
	Columns     []table.Column
	Description string
	Actions     func() []table.Action
	Activate    func(action, id string) tea.Cmd
}

// Action describes a screen shortcut rendered and dispatched by the shell.
type Action struct {
	Key                rune
	Mod                tea.KeyMod
	Name, Label, Short string
	Enabled            bool
}
