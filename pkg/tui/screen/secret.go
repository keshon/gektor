package screen

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

// secretState is isolated from form fields, submission maps and history.
// Take consumes the value; closing, replacing or suspending the overlay clears it.
type secretState struct {
	editor      textinput.Model
	Hint, Error string
}

func (s *secretState) Clear() {
	s.editor.Reset()
	s.editor.Blur()
}

func (s *secretState) Take() string {
	value := s.editor.Value()
	s.editor.Reset()
	return value
}

// TakeSecret consumes the value only from the current secret dialog. Callers
// must not retain the returned value after handing it to their credential store.
func (m *Controls) TakeSecret(id DialogID) (string, bool) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Secret == nil {
		return "", false
	}
	return m.dialog.Secret.Take(), true
}

// SetSecretError updates validation feedback only for its originating dialog.
func (m *Controls) SetSecretError(id DialogID, err error) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Secret == nil {
		return false
	}
	m.dialog.Secret.Error = ""
	if err != nil {
		m.dialog.Secret.Error = err.Error()
	}
	m.refreshSecret()
	return true
}

func (m *Controls) PresentSecret(title, hint string) {
	m.Open(title, nil)
	editor := textinput.New()
	editor.SetStyles(m.palette().TextInputStyles(editor.Styles()))
	editor.EchoMode = textinput.EchoPassword
	editor.EchoCharacter = '•'
	if m.palette().Modern {
		editor.EchoCharacter = '▪'
	}
	editor.CharLimit = 0
	editor.Prompt = "Secret: "
	editor.SetVirtualCursor(false)
	// Terminal bracketed paste is accepted; no asynchronous clipboard read may outlive this overlay.
	editor.KeyMap.Paste.SetEnabled(false)
	_ = editor.Focus() // The terminal owns the native caret.
	m.dialog.Secret = &secretState{editor: editor, Hint: hint}
	m.dialog.Shortcuts = []popup.Shortcut{{Key: "Ctrl+S", Action: "save encrypted"}, {Key: "Esc", Action: "cancel"}}
	m.refreshSecret()
}

func (m *Controls) SecretInput(msg tea.Msg) tea.Cmd {
	s := m.dialog.Secret
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.Close()
			return nil
		case "ctrl+s":
			m.events = append(m.events, Event{Kind: SecretApply, ID: m.DialogID()})
			return nil
		case "enter":
			return nil
		}
	}
	if paste, ok := msg.(tea.PasteMsg); ok && (!utf8.ValidString(paste.Content) || strings.IndexFunc(paste.Content, unicode.IsControl) >= 0) {
		s.Error = "Secret must be valid text without control characters."
		m.refreshSecret()
		return nil
	}
	s.Error = ""
	s.editor, _ = s.editor.Update(msg) // Clipboard commands are disabled; the terminal owns caret blinking.
	if len(s.editor.Value()) > 16384 {
		s.editor.Reset()
		s.Error = "Secret exceeds 16384 bytes; input cleared. Enter it again."
	}
	m.refreshSecret()
	return nil
}

func (m *Controls) refreshSecret() {
	s := m.dialog.Secret
	_, _, width, _ := m.Area()
	s.editor.SetWidth(max(popup.ModalWidth(width, 0)-5-cell.Width(s.editor.Prompt), 1))
	m.dialog.Lines = []string{s.Hint, "", s.editor.View()}
	if s.Error != "" {
		m.dialog.Lines = append(m.dialog.Lines, "", s.Error)
	}
}

func (s *secretState) String() string   { return "[masked secret input]" }
func (s *secretState) GoString() string { return "[masked secret input]" }

// secretButtons keeps the masked editor's actions inside its own modal.
func (m *Controls) secretButtons(width int) (string, int) {
	styles := m.FormButtonStyles()
	label := "Save encrypted"
	if width < 32 {
		label = "Save"
	}
	save := form.Button(form.ButtonLabel(label, width, styles), true, true, false, styles)
	cancel := form.Button(form.ButtonLabel("Cancel", width, styles), false, true, false, styles)
	return save + "  " + cancel, lipgloss.Width(save) + 2
}
