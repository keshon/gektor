package screen

// Event refers to the dialog that produced it, including after replacement.
// The application drains events on its update loop; controls never invoke owners.
type Event struct {
	Kind        EventKind
	ID          DialogID
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
	DialogActionRequested
	TableAction
	TableChanged
	SecretApply
)

func (m *Controls) TakeEvents() []Event {
	events := m.events
	m.events = nil
	return events
}

func (m *Controls) completeForm(dialog *dialogState, err error) {
	if m.dialog != dialog {
		return
	}
	if err != nil {
		dialog.Form.Error = err.Error()
		return
	}
	m.Close()
}

func (m *Controls) Suspend() {
	if m.dialog == nil {
		return
	}
	dialog := m.dialog
	id := m.DialogID()
	if dialog.Secret != nil {
		dialog.Secret.Clear()
	}
	if dialog.Form != nil {
		dialog.Form.Sync()
	}
	dialog.suspended = true
	m.dialog = nil
	m.events = append(m.events, Event{Kind: Suspended, ID: id})
}

func (m *Controls) completeSave(dialog *dialogState, err error) {
	if m.dialog != dialog {
		return
	}
	if err != nil {
		dialog.Lines = append(dialog.Lines, "", "Could not save: "+err.Error())
		dialog.Offset = 0
		return
	}
	m.Close()
}

func (m *Controls) resume(dialog *dialogState) {
	dialog.suspended = false
	first := m.dialog == nil
	if m.dialog != nil {
		if m.dialog.Secret != nil {
			m.dialog.Secret.Clear()
		}
		m.events = append(m.events, Event{Kind: Replaced, ID: m.DialogID()})
	}
	m.dialog = dialog
	// A suspended secret always resumes empty.
	if dialog.Secret != nil {
		_ = dialog.Secret.editor.Focus()
	}
	m.events = append(m.events, Event{Kind: Opened, ID: m.DialogID(), First: first})
	m.ResizeFormEditor()
}

// Prepare updates editor styles on the update loop, before presentation reads.
func (m *Controls) Prepare() {
	if m.dialog != nil && m.dialog.Form != nil {
		f := m.dialog.Form
		f.Editor.SetStyles(m.palette().InputStyles(f.Editor.Styles()))
		if f.Picker != nil {
			f.Picker.Search.SetStyles(m.palette().TextInputStyles(f.Picker.Search.Styles()))
		}
	}

	if m.dialog != nil && m.dialog.Secret != nil {
		m.dialog.Secret.editor.SetStyles(m.palette().TextInputStyles(m.dialog.Secret.editor.Styles()))
		m.dialog.Secret.editor.EchoCharacter = '•'
		if m.Props.Modern {
			m.dialog.Secret.editor.EchoCharacter = '▪'
		}
		m.refreshSecret()
	}
	if m.dialog != nil && m.dialog.Table != nil {
		search := &m.dialog.Table.model.Search
		search.SetStyles(m.palette().TextInputStyles(search.Styles()))
	}
}

// CompleteFormID applies a validation result only to its originating form.
func (m *Controls) CompleteFormID(id DialogID, err error) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return
	}
	m.completeForm(id.dialog, err)
}

// CompleteSaveID applies a save result only to its originating information dialog.
func (m *Controls) CompleteSaveID(id DialogID, err error) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form != nil || m.dialog.Table != nil || m.dialog.Secret != nil {
		return
	}
	m.completeSave(id.dialog, err)
}

// ResumeID restores a suspended draft owned by these controls. Closed and
// replaced dialogs cannot be revived, and a draft can only resume once.
func (m *Controls) ResumeID(id DialogID) bool {
	if id.owner != m || id.dialog == nil || !id.dialog.suspended {
		return false
	}
	m.resume(id.dialog)
	return true
}
