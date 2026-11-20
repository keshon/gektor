package screen

import (
	tea "charm.land/bubbletea/v2"

	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/components/table"
)

// Form supplies initial draft values. Controls copy its fields and choice lists.
type Form struct {
	Title, Hint, ApplyLabel    string
	DialogTitle                string
	Fields                     []form.Field
	Choices                    map[string][]string
	Labels                     map[string]map[string]string
	InlineChoices              map[string]int
	NoSearchFields             map[string]bool
	ShowWhen                   map[string][]form.Condition
	FileFields                 map[string]fieldform.FilePicker
	Destructive, StartOnCancel bool
	SaveCopy                   bool
}

// Dialog describes an informational overlay with owner-supplied actions or a table.
type Dialog struct {
	Help           bool
	Title          string
	PreferredWidth int
	Lines          []string
	Shortcuts      []popup.Shortcut
	Actions        []DialogAction
	Table          *Table
}

// Table supplies copied row snapshots and an optional reusable navigation state.
// Nil Rows preserves that state's previous rows; a non-nil empty slice clears it.
// Selected requests a visible row by identity; empty preserves the selection.
// A state from another Controls instance never shares navigation.
type Table struct {
	State        TableID
	Rows         []table.Row
	Selected     string
	Columns      []table.Column
	Description  string
	Actions      []table.Action
	List         bool
	EmptyMessage string
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

// DialogAction binds a visible shortcut to an owner-defined intent.
type DialogAction struct {
	ID    string
	Key   rune
	Label string
}
