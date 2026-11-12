package workflow

import (
	"fmt"

	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

// formEdit belongs to this screen. Shared controls retain only the draft;
// they never retain an operation or a reference back to its owner.
type formEdit interface {
	apply(*Model, map[string]string) error
}

func (m *Model) openForm(form screen.Form, edit formEdit) *settings.Model {
	draft := m.dialogs.PresentForm(form)
	if m.formEdits == nil {
		m.formEdits = make(map[*screen.DialogState]formEdit)
	}
	m.formEdits[m.dialogs.Dialog] = edit
	return draft
}

// applySubmission validates and applies a draft on the owner's update loop.
// The dialog identity prevents late input from applying to a replacement form.
func (m *Model) applySubmission(dialog *screen.DialogState, values map[string]string) error {
	edit := m.formEdits[dialog]
	if edit == nil || m.dialogs.Dialog != dialog {
		return nil
	}
	return edit.apply(m, values)
}

type formAction uint8

const (
	chooseNodeForm formAction = iota
	connectNodesForm
	proposeChangeForm
	reviewOperationForm
	saveRecipeForm
	chooseSettingsForm
)

func (action formAction) apply(m *Model, values map[string]string) error {
	switch action {
	case chooseNodeForm:
		return m.applyNodeChoice(values)
	case connectNodesForm:
		return m.applyNodeConnection(values)
	case proposeChangeForm:
		return m.applyChangePrompt(values)
	case reviewOperationForm:
		return m.applyOperationReview(values)
	case saveRecipeForm:
		return m.applyRecipePath(values)
	case chooseSettingsForm:
		return m.applySettingsChoice(values)
	}
	return fmt.Errorf("unknown form action %d", action)
}
