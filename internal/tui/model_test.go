package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestScreenSwitchPreservesAgentDraftAndOwnsTerminalModes(t *testing.T) {
	draft := "unfinished agent message"
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{InitialDraft: draft}})
	for _, command := range []string{"screen:workflows", "screen:agent", "screen:workflows", "screen:agent"} {
		m.runAction(command)
		m.syncPresentation()
		view := m.View()
		if !view.AltScreen || !view.ReportFocus || view.MouseMode != tea.MouseModeCellMotion {
			t.Fatal("shell lost terminal modes", command)
		}
		if command == "screen:agent" && !strings.Contains(ansi.Strip(view.Content), draft) {
			t.Fatal("Agent draft lost")
		}
	}
}

func TestWorkflowPasteCannotChangeHiddenAgentDraft(t *testing.T) {
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{InitialDraft: "original draft"}})
	m.runAction("screen:workflows")
	m.syncPresentation()
	m.Update(tea.PasteMsg{Content: "/quit\n/new another task"})
	m.runAction("screen:agent")
	m.syncPresentation()
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "original draft") || strings.Contains(view, "another task") || m.quit || !m.agent.Status().Idle {
		t.Fatal("Workflow paste reached hidden Agent input")
	}
}

func TestWorkflowEscapeRestoresAgentInput(t *testing.T) {
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{InitialDraft: "retained"}})
	m.runAction("screen:workflows")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.workflowActive() || m.View().Cursor == nil {
		t.Fatal("Escape did not restore Agent input focus")
	}
	m.Update(tea.PasteMsg{Content: " text"})
	if draftOf(m) != "retained text" {
		t.Fatal("returning to Agent lost or blocked the retained draft")
	}
}

func TestRunningAgentCannotOpenSettings(t *testing.T) {
	m := settingsModel(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.agent.Status().Idle {
		t.Fatal("fixture did not start an Agent turn")
	}
	m.runAction("edit:model")
	if m.chrome.Dialog != nil || m.modeChange != nil {
		t.Fatal("settings interrupted the active Agent turn")
	}
	finishAgentTurn(t, m)
}

func TestPeerMenusAndOverflow(t *testing.T) {
	for _, active := range []string{"screen:agent", "screen:workflows"} {
		m := shellTestModel(t, Config{})
		m.runAction(active)
		m.syncPresentation()
		var labels []string
		owners := map[string]string{}
		for _, menu := range m.chrome.Props.Menus {
			labels = append(labels, menu.Label)
			for _, item := range menu.Items {
				if item.Command == "" {
					continue
				}
				if owner := owners[item.Command]; owner != "" {
					t.Fatalf("%s belongs to %s and %s", item.Command, owner, menu.ID)
				}
				owners[item.Command] = menu.ID
			}
		}
		want := []string{"GEKTOR", "Session", "Run", "View", "Settings", "Help"}
		if active == "screen:workflows" {
			want = []string{"GEKTOR", "Recipe", "Node", "Run", "View", "Settings", "Help"}
		}
		if !reflect.DeepEqual(labels, want) {
			t.Fatal(labels)
		}
		for _, width := range []int{24, 40, 80, 120} {
			sizeModelTo(t, m, width, 24)
			reached := map[string]bool{}
			for _, cell := range m.chrome.MenuCells() {
				if cell.End > width {
					t.Fatal("menu outside terminal", width)
				}
				if cell.Action.Name != "overflow" {
					reached[cell.Action.Name] = true
					continue
				}
				m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "overflow"}
				items := m.chrome.HeaderItems()
				for i, item := range items {
					m.chrome.Header.Menu, m.chrome.Header.Row = "overflow", i
					m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					target := strings.TrimPrefix(item.Command, "menu:")
					if m.chrome.Header.Menu != target {
						t.Fatal("overflow target did not open", target)
					}
					layout := m.chrome.HeaderMenuLayout()
					if layout.X < 0 || layout.X+layout.Width > width {
						t.Fatal("popup outside terminal", width)
					}
					reached[target] = true
				}
			}
			for _, menu := range m.chrome.Props.Menus {
				if !reached[menu.ID] {
					t.Fatal("unreachable menu", width, menu.ID)
				}
			}
			m.chrome.CloseHeader()
		}
	}
}

func TestWorkflowMenuMouseIgnoresFrameAndOutsideWheel(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "settings"}
		m.chrome.Header.Row = m.chrome.HeaderActionRow(0, 1)
		before := m.chrome.Header.Row
		m.Update(tea.MouseWheelMsg{X: 119, Y: 27, Button: tea.MouseWheelDown})
		if m.chrome.Header.Row != before {
			t.Fatal("outside wheel moved selection")
		}
		layout := m.chrome.HeaderMenuLayout()
		m.Update(tea.MouseClickMsg{X: layout.X, Y: layout.Y + 1, Button: tea.MouseLeft})
		if m.chrome.Dialog != nil || m.chrome.Header.Menu != "settings" {
			t.Fatal("menu frame executed a command")
		}
	}
}

func TestQuitChecksRetainedWorkflowFromAgent(t *testing.T) {
	m := workflowTestModel(t)
	seedWorkflow(t, m, workflowRecipe())
	m.runAction("screen:agent")
	m.syncPresentation()
	m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if m.quit || m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Quit with unsaved workflow?" {
		t.Fatal("hidden dirty workflow bypassed quit protection")
	}
	m.Update(tea.PasteMsg{Content: "discard"})
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !m.quit || m.workflows != nil || m.chrome.Dialog != nil {
		t.Fatal("confirmed discard did not finish quitting")
	}
}

func TestSettingsMenusAreIdenticalAcrossModes(t *testing.T) {
	m := settingsModel(t)
	settingsItems := func() []screen.MenuItem {
		for _, menu := range m.chrome.Props.Menus {
			if menu.ID == "settings" {
				return menu.Items
			}
		}
		return nil
	}
	agentItems := append([]screen.MenuItem(nil), settingsItems()...)
	m.openWorkflows()
	m.syncPresentation()
	if len(agentItems) == 0 || !reflect.DeepEqual(agentItems, settingsItems()) {
		t.Fatal("Settings changed with the active mode")
	}
	for _, menu := range m.chrome.Props.Menus {
		if menu.ID == "session" {
			t.Fatal("Agent session menu exposed in Workflow")
		}
		for _, item := range menu.Items {
			if item.Command == "/processes" || item.Command == "prompt-info" || item.Command == "help-commands" {
				t.Fatalf("Agent execution command in Workflow: %s", item.Command)
			}
		}
	}
	for _, section := range []string{"model", "generation", "profiles", "you", "prompts"} {
		m.runAction("edit:" + section)
		if m.chrome.Dialog == nil || m.chrome.Dialog.Form.EditSession.Section != section {
			t.Fatalf("settings %s unavailable in Workflow", section)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !m.workflowActive() || draftOf(m) != "unfinished draft" {
			t.Fatal("settings cancellation changed mode or draft")
		}
	}
	m.runAction("appearance")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Appearance" {
		t.Fatal("global appearance unavailable in Workflow")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestWorkflowWorkspacePickerHasNoAgentSettings(t *testing.T) {
	m := settingsModel(t)
	m.openWorkflows()
	m.syncPresentation()
	m.runAction("workspace")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Workspace" {
		t.Fatal("workspace picker did not open")
	}
	f := m.chrome.Dialog.Form
	if f.EditSession != nil || len(f.Fields) != 1 || f.Fields[0].Key != "app-workspace" {
		t.Fatal("workspace picker exposed Agent settings")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.workflowActive() || draftOf(m) != "unfinished draft" {
		t.Fatal("workspace cancellation changed screen or Agent draft")
	}
}

func TestWorkflowAppearanceUpdatesBothScreensWithoutRelaunch(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	m.openWorkflows()
	draft := seedWorkflow(t, m, workflowRecipe())
	before := draft.Source()
	m.syncPresentation()
	m.runAction("appearance")
	values := m.chrome.Dialog.Form.Values()
	values["theme"], values["frame"] = "light", "outlined"
	if err := m.applyValues(values); err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	if !m.workflowActive() || !m.workflowSurface().Outlined || currentPalette().Name != "light" {
		t.Fatal("appearance did not immediately update Workflow")
	}
	if m.modeChange != nil || draft.Source() != before || !m.workflows.Dirty() {
		t.Fatal("appearance relaunched the application or changed the recipe")
	}
	m.showAgent()
	m.syncPresentation()
	if m.agent.Appearance().Frame != "outlined" || draftOf(m) != "unfinished draft" {
		t.Fatal("appearance did not preserve the Agent draft and update its frame")
	}
}

func TestWorkflowSharedModelApplyPreservesConversation(t *testing.T) {
	m := settingsModel(t)
	before := snapshotOf(m)
	m.openWorkflows()
	m.syncPresentation()
	m.runAction("edit:generation")
	m.chrome.Dialog.Form.Editor.SetValue("0.7")
	m.applyForm()
	change := m.modeChange
	if change == nil || change.Preferences.Profile.Generation.Temperature != 0.7 {
		t.Fatal("shared model settings did not queue the new application configuration")
	}
	if change.Draft != before.Draft || change.Directory != before.Directory || !reflect.DeepEqual(change.History, before.History) {
		t.Fatal("settings changed existing conversation data")
	}
}

func TestSettingsRelaunchProtectsRetainedWorkflow(t *testing.T) {
	m := settingsModel(t)
	m.openWorkflows()
	draft := seedWorkflow(t, m, workflowRecipe())
	before := draft.Source()
	m.showAgent()
	m.syncPresentation()
	m.editSettings("generation")
	m.chrome.Dialog.Form.Editor.SetValue("0.8")
	m.applyForm()
	if m.modeChange != nil || m.chrome.Dialog == nil || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("settings discarded a retained unsaved workflow")
	}
	if draft.Source() != before || !m.workflows.Dirty() || draftOf(m) != "unfinished draft" {
		t.Fatal("failed settings apply changed a retained draft")
	}
}

type stoppedClient struct{}

func (stoppedClient) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	<-ctx.Done()
	return llm.ChatResponse{}, ctx.Err()
}

func TestShellDeliversCompletionAfterStopBeforeQuit(t *testing.T) {
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{Task: "wait", Env: roles.Env{Client: stoppedClient{}}}})
	if m.agent.Status().Idle {
		t.Fatal("initial task did not start")
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if m.quit {
		t.Fatal("first quit did not wait for cancellation")
	}
	// Run the real Bubble Tea effects and route their results through the parent.
	// Completion is delivered by the child's event queue, without Program.Send.
	finishAgentTurn(t, m)
	m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if !m.quit {
		t.Fatal("second quit did not exit")
	}
}
