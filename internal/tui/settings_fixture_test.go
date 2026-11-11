package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

func settingsModel(t *testing.T) *Model { return settingsModelFor(t, nil) }
func settingsModelFor(t *testing.T, preferences *config.Preferences) *Model {
	return settingsModelWithHistory(t, preferences, nil)
}
func settingsModelWithHistory(t *testing.T, preferences *config.Preferences, extra []llm.Message) *Model {
	t.Helper()
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Identity: &config.Identity{UserName: "User", SystemPrompt: "Base rules"}}
	if preferences != nil && preferences.Profile != nil {
		profile = preferences.Profile.Clone()
	}
	prepared, err := app.PrepareProfile(profile, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	state := filepath.Join(prepared.Directory, "original", "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "Base rules"}, {Role: llm.RoleUser, Content: "hello"}, {Role: llm.RoleAssistant, Content: "reply"}}
	history = append(history, extra...)
	if err := agent.SaveState(state, history); err != nil {
		t.Fatal(err)
	}
	application := &Application{Preferences: config.Preferences{Version: 1, Workspace: ws.Root()}, PreferencePath: filepath.Join(t.TempDir(), "application.json")}
	if preferences != nil {
		application.Preferences = *preferences
		application.Temporary = true
	}
	m, err := newModel(t.Context(), Config{AgentConfig: AgentConfig{Application: application, Plan: preferences != nil && preferences.Mode == "plan", PreparedProfile: &prepared, InitialDraft: "unfinished draft", StateFile: state, SessionDirectory: filepath.Dir(state), SeedHistory: history, Env: roles.Env{WS: ws, Client: doneClient{}, MaxTokens: 100, ContextLimit: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}
func snapshotOf(m *Model) *ModeChange                             { snapshot, _ := m.agent.Snapshot(); return snapshot }
func stateFileOf(m *Model) string                                 { return filepath.Join(snapshotOf(m).Directory, "state.json") }
func draftOf(m *Model) string                                     { return snapshotOf(m).Draft }
func (m *Model) applyForm()                                       { m.chrome.ApplyForm(); m.syncPresentation() }
func (m *Model) focusForm(i int)                                  { m.chrome.FocusForm(i) }
func (m *Model) formKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.Update(key) }
func (m *Model) headerKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, m.runAction(m.chrome.HeaderKey(key))
}
func (m *Model) headerItems() []screen.MenuItem { m.syncPresentation(); return m.chrome.HeaderItems() }
func (m *Model) menuItems(id string) []screen.MenuItem {
	m.syncPresentation()
	for _, menu := range m.chrome.Props.Menus {
		if menu.ID == id {
			return menu.Items
		}
	}
	return nil
}
func (m *Model) command(command string) (tea.Model, tea.Cmd) {
	cmd := m.runAction(command)
	_, next := m.Update(nil)
	return m, tea.Batch(cmd, next)
}
func (m *Model) openForm(title, hint, label string, fields []form.Field, apply func(map[string]string) error) {
	m.chrome.OpenForm(title, hint, label, fields, apply)
}

type scriptClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f scriptClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

func (m *Model) formRows() ([]string, []form.Hit, int, int) {
	m.syncPresentation()
	return m.chrome.FormRows()
}
func (m *Model) settingsProps() settings.Props        { m.syncPresentation(); return m.chrome.SettingsProps() }
func (m *Model) currentAppearance() config.Appearance { return m.agent.Appearance() }

// clickVisible clicks the first drawn occurrence of label.
//
// Tests used to work out where a button was by repeating the layout maths the
// component already does. That made them a second opinion about the renderer
// rather than a check on it: every geometry change showed up as several
// failing mouse tests and no failing renderer, and one of those renderer bugs
// had been hidden because the copied maths happened to agree with it.
func clickVisible(t *testing.T, m *Model, label string) {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for y, line := range lines {
		at := strings.Index(line, label)
		if at < 0 {
			continue
		}
		x := len([]rune(line[:at])) + 1
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no %q on screen:\n%s", label, strings.Join(lines, "\n"))
}

// clickHit clicks a hit at the position the component reports for itself.
//
// The geometry comes from the model rather than from the test, so a test
// cannot fail - or pass - because it disagreed with the renderer about where
// something was.
func clickHit(t *testing.T, m *Model, index int) {
	t.Helper()
	_, hits, _, _ := m.formRows()
	x, y := m.chrome.Dialog.Form.Origin(m.settingsProps())
	for _, hit := range hits {
		if hit.Index != index {
			continue
		}
		m.Update(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y0, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no hit for index %d in %+v", index, hits)
}

// fieldHit reports the hit for a field key, so a test can name the field it
// means instead of its position in a list that a toggle can change.
func fieldHit(t *testing.T, m *Model, key string) int {
	t.Helper()
	for i, f := range m.chrome.Dialog.Form.Fields {
		if f.Key == key {
			return i
		}
	}
	t.Fatalf("no field %q", key)
	return -1
}
