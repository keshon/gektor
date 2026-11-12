package screen

// Event refers to the dialog that produced it, including after replacement.
// The application drains events on its update loop; controls never invoke owners.
type Event struct {
	Kind        EventKind
	Dialog      *DialogState
	First       bool
	Action, Row string
	Values      map[string]string
}

type EventKind uint8

const (
	Opened EventKind = iota
	Closed
	Replaced
	Suspended
	Apply
	SaveCopy
	Save
	Review
	TableAction
	TableChanged
	SecretApply
)

func (m *Controls) TakeEvents() []Event {
	events := m.events
	m.events = nil
	return events
}

// CompleteForm ignores replies for a dialog that has since been replaced.
func (m *Controls) CompleteForm(dialog *DialogState, err error) {
	if m.Dialog != dialog {
		return
	}
	if err != nil {
		dialog.Form.Error = err.Error()
		return
	}
	m.Close()
}

func (m *Controls) Suspend() {
	if m.Dialog == nil {
		return
	}
	dialog := m.Dialog
	if dialog.Secret != nil {
		dialog.Secret.Clear()
	}
	if dialog.Form != nil {
		dialog.Form.Sync()
	}
	m.Dialog = nil
	m.events = append(m.events, Event{Kind: Suspended, Dialog: dialog})
}

func (m *Controls) CompleteSave(dialog *DialogState, err error) {
	if m.Dialog != dialog {
		return
	}
	if err != nil {
		dialog.Lines = append(dialog.Lines, "", "Could not save: "+err.Error())
		dialog.Offset = 0
		return
	}
	m.Close()
}

// Resume takes back the draft retained during a permission prompt.
func (m *Controls) Resume(dialog *DialogState) {
	first := m.Dialog == nil
	if m.Dialog != nil {
		if m.Dialog.Secret != nil {
			m.Dialog.Secret.Clear()
		}
		m.events = append(m.events, Event{Kind: Replaced, Dialog: m.Dialog})
	}
	m.Dialog = dialog
	// A suspended secret always resumes empty.
	if dialog.Secret != nil {
		_ = dialog.Secret.editor.Focus()
	}
	m.events = append(m.events, Event{Kind: Opened, Dialog: dialog, First: first})
	m.ResizeFormEditor()
}

// Prepare updates editor styles on the update loop, before presentation reads.
func (m *Controls) Prepare() {
	if m.Dialog != nil && m.Dialog.Secret != nil {
		m.Dialog.Secret.editor.SetStyles(TextInputStyles(m.Dialog.Secret.editor.Styles()))
		m.refreshSecret()
	}
	if m.Dialog != nil && m.Dialog.Table != nil {
		search := &m.Dialog.Table.Model.Search
		search.SetStyles(TextInputStyles(search.Styles()))
	}
}
