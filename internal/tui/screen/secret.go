package screen

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/popup"
)

// SecretState is isolated from form fields, submission maps and history.
// Take consumes the value; closing, replacing or suspending the overlay clears it.
type SecretState struct {
	editor      textinput.Model
	Hint, Error string
}

func (s *SecretState) Clear() {
	s.editor.Reset()
	s.editor.Blur()
}

func (s *SecretState) Take() string {
	value := s.editor.Value()
	s.editor.Reset()
	return value
}

// TakeSecret consumes the value only from the current secret dialog. Callers
// must not retain the returned value after handing it to their credential store.
func (m *Controls) TakeSecret(id DialogID) (string, bool) {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Secret == nil {
		return "", false
	}
	return m.Dialog.Secret.Take(), true
}

// SetSecretError updates validation feedback only for its originating dialog.
func (m *Controls) SetSecretError(id DialogID, err error) bool {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Secret == nil {
		return false
	}
	m.Dialog.Secret.Error = ""
	if err != nil {
		m.Dialog.Secret.Error = err.Error()
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
	editor.CharLimit = 0
	editor.Prompt = "Secret: "
	editor.SetVirtualCursor(true)
	// Terminal bracketed paste is accepted; no asynchronous clipboard read may outlive this overlay.
	editor.KeyMap.Paste.SetEnabled(false)
	_ = editor.Focus() // A steady virtual caret needs no blink command.
	m.Dialog.Secret = &SecretState{editor: editor, Hint: hint}
	m.Dialog.Shortcuts = []popup.Shortcut{{Key: "Ctrl+S", Action: "save encrypted"}, {Key: "Esc", Action: "cancel"}}
	m.refreshSecret()
}

func (m *Controls) SecretInput(msg tea.Msg) tea.Cmd {
	s := m.Dialog.Secret
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
	s.editor, _ = s.editor.Update(msg) // Clipboard commands are disabled; cursor blink does not own secret input.
	if len(s.editor.Value()) > 16384 {
		s.editor.Reset()
		s.Error = "Secret exceeds 16384 bytes; input cleared. Enter it again."
	}
	m.refreshSecret()
	return nil
}

func (m *Controls) refreshSecret() {
	s := m.Dialog.Secret
	_, _, width, _ := m.Area()
	s.editor.SetWidth(max(popup.ModalWidth(width, 0)-14, 8))
	m.Dialog.Lines = []string{s.Hint, "", s.editor.View()}
	if s.Error != "" {
		m.Dialog.Lines = append(m.Dialog.Lines, "", s.Error)
	}
}

func (s *SecretState) String() string   { return "[masked secret input]" }
func (s *SecretState) GoString() string { return "[masked secret input]" }
