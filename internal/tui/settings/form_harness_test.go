package settings

import (
	"slices"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/form"
)

// testForm connects settings policy to the real shared editor as the application
// does: policy receives copied snapshots and controls receive presentation data.
type testForm struct {
	fieldform.Model
	policy      Model
	EditSession *EditSession
}

func newTestForm(fields []form.Field, editor textarea.Model) testForm {
	f := testForm{Model: fieldform.NewModel(fields, editor)}
	f.Prepare()
	return f
}

func (f *testForm) Prepare() {
	f.policy.EditSession = f.EditSession
	f.policy.Choices = f.Choices
	f.Configure(f.policy.Prepare(f.Snapshot()))
}

func (f *testForm) FieldVisible(index int) bool {
	f.policy.EditSession = f.EditSession
	f.policy.Fields = slices.Clone(f.Fields)
	return f.policy.FieldVisible(index)
}

func (f *testForm) UpdateKey(msg tea.KeyPressMsg, props fieldform.Props) (fieldform.Intent, tea.Cmd) {
	f.Prepare()
	if intent, handled := f.policy.Shortcut(msg.String()); handled {
		f.Prepare()
		return intent, nil
	}
	intent, cmd := f.Model.UpdateKey(msg, props)
	f.Prepare()
	return intent, cmd
}

func (f *testForm) OpenPicker(props fieldform.Props) tea.Cmd {
	f.Prepare()
	return f.Model.OpenPicker(props)
}

func (f *testForm) Provider(request fieldform.DiscoveryRequest) (config.Provider, error) {
	f.policy.EditSession = f.EditSession
	return f.policy.Provider(request)
}

func (f *testForm) note() string {
	f.policy.EditSession = f.EditSession
	f.policy.Fields = slices.Clone(f.Fields)
	return f.policy.note()
}
