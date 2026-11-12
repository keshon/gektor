package screen

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/settings"
)

// Form supplies initial draft values. Controls copy its fields and choice lists.
type Form struct {
	Title, Hint, ApplyLabel    string
	Fields                     []form.Field
	Choices                    map[string][]string
	ShowWhen                   map[string][]form.Condition
	FileFields                 map[string]settings.FilePicker
	Destructive, StartOnCancel bool
}

// Dialog describes an informational overlay, optionally with a save action or table.
type Dialog struct {
	Title     string
	Lines     []string
	Shortcuts []popup.Shortcut
	Save      bool
	Table     *Table
}

// Table transfers its model to controls while the overlay is open. The feature
// may retain it for later navigation but must not update it during presentation.
type Table struct {
	Model       *table.Model
	Columns     []table.Column
	Description string
	Actions     []table.Action
}

// Action describes a screen shortcut rendered and dispatched by the shell.
type Action struct {
	Key                rune
	Mod                tea.KeyMod
	Name, Label, Short string
	Enabled            bool
}

// Navigate requests an application screen change, optionally with a screen action.
type Navigate struct{ Screen, Action string }

// QuitRequest asks the application to apply its complete quit policy.
type QuitRequest struct{}

// ApplicationCommand requests a global setting or theme operation from the shell.
type ApplicationCommand struct{ Command string }
