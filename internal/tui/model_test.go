package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestWorkflowCanOpenSharedProcessManager(t *testing.T) {
	m := workflowTestModel(t)
	found := false
	for _, group := range m.chrome.Props.Menus {
		for _, item := range group.Items {
			if group.ID == "execution" && item.Command == "/processes" && item.Enabled {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("process manager missing from Workflow Run menu")
	}
	m.runAction("/processes")
	m.syncPresentation()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil || len(m.chrome.Dialog.Table.Model.Rows) != 0 {
		t.Fatal("empty process manager did not open")
	}
	for _, width := range []int{40, 80, 120} {
		sizeModelTo(t, m, width, 24)
		if m.dialogView() == "" {
			t.Fatal("empty dialog")
		}
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
	results := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var launch func(tea.Cmd)
	launch = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, child := range batch {
					launch(child)
				}
				return
			}
			select {
			case results <- msg:
			case <-ctx.Done():
			}
		}()
	}
	launch(m.Init())
	deadline := time.After(5 * time.Second)
	for !m.agent.Status().Idle {
		select {
		case msg := <-results:
			_, cmd := m.Update(msg)
			launch(cmd)
		case <-deadline:
			t.Fatal("run completion did not reach the application")
		}
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if !m.quit {
		t.Fatal("second quit did not exit")
	}
}
