package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow/script"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if err := script.Serve(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type doneClient struct{}

func (doneClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}, nil
}

type recordingClient struct{ requests chan llm.ChatRequest }

func (c recordingClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests <- req
	return doneClient{}.Chat(ctx, req)
}
func assertSubmittedText(t *testing.T, requests <-chan llm.ChatRequest, want string) {
	t.Helper()
	select {
	case req := <-requests:
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == llm.RoleUser && req.Messages[i].Harness == "" {
				if req.Messages[i].Content != want {
					t.Fatalf("submitted %q, want %q", req.Messages[i].Content, want)
				}
				return
			}
		}
		t.Fatal("backend did not receive a user message")
	case <-time.After(5 * time.Second):
		t.Fatal("submission did not reach backend")
	}
}

func shellTestModel(t *testing.T, cfg Config) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	cfg.Env.WS = ws
	cfg.Env.Policy = permission.Default()
	if cfg.Env.Client == nil {
		cfg.Env.Client = doneClient{}
	}
	m, err := newModel(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	sizeModelTo(t, m, 120, 30)
	setMinimal(t, m, false)
	return m
}

func sizeModelTo(t *testing.T, m *Model, width, height int) *Model {
	t.Helper()
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}
func setMinimal(t *testing.T, m *Model, minimal bool) {
	t.Helper()
	m.runAction("appearance")
	values := m.chrome.Dialog.Form.Values()
	values["frame"] = "outlined"
	if minimal {
		values["frame"] = "minimal"
	}
	if err := m.applyValues(values); err != nil {
		t.Fatal(err)
	}
	m.chrome.Close()
	m.syncPresentation()
}
func shellInspectorModel(t *testing.T, width int) *Model {
	t.Helper()
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{InitialDraft: "parent draft", SeedHistory: []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "read-1", Content: strings.Repeat("line of output\n", 70)},
	}}})
	sizeModelTo(t, m, width, 30)
	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return m
}

const (
	paneTop                 = 4
	inspectorSplitWidth     = screen.SidePaneWidth
	colorSelectedText       = screen.ColorSelectedText
	colorSelectedBackground = screen.ColorSelectedBackground
	colorPopupText          = screen.ColorPopupText
	colorAccent             = screen.ColorAccent
	colorSuccess            = screen.ColorSuccess
	colorWarning            = screen.ColorWarning
	colorError              = screen.ColorError
	colorDisabledText       = screen.ColorDisabledText
)

func currentPalette() *screen.Palette { return screen.CurrentPalette() }
func setTheme(theme string)           { screen.SetTheme(theme) }
func themeColor(name string) string   { return screen.Color(name) }
func sameColor(got color.Color, want string) bool {
	if got == nil {
		return want == ""
	}
	r, g, b, _ := got.RGBA()
	return strings.EqualFold(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8), want)
}
func checkModalWidth(t *testing.T, view string, width int) {
	t.Helper()
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(row, "╭") || strings.Contains(row, "╰") {
			if ansi.StringWidth(strings.TrimLeft(row, " ")) != width {
				t.Fatalf("modal width: %q, want %d", row, width)
			}
		}
	}
}

// These adapters keep rendering assertions focused on the real parent and its
// controls without exposing either child screen's implementation to tests.
func (m *Model) screenContent() string              { m.syncPresentation(); return m.View().Content }
func (m *Model) workflowScreen() string             { return m.screenContent() }
func (m *Model) closeDialog()                       { m.chrome.Close(); m.syncPresentation() }
func (m *Model) closeHeader()                       { m.chrome.CloseHeader(); m.syncPresentation() }
func (m *Model) settingsArea() (int, int, int, int) { m.syncPresentation(); return m.chrome.Area() }
func (m *Model) dialogRows() ([]string, int, int)   { m.syncPresentation(); return m.chrome.DialogRows() }
func (m *Model) dialogView() string                 { m.syncPresentation(); return m.chrome.DialogView() }
func (m *Model) footerActions() []screen.Action     { m.syncPresentation(); return m.chrome.Props.Actions }
func (m *Model) rawHeaderItems() []screen.MenuItem {
	m.syncPresentation()
	return m.chrome.RawHeaderItems()
}

// finishAgentTurn delivers real effects until the running turn has completed.
func finishAgentTurn(t *testing.T, m *Model) {
	t.Helper()
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
			if !m.agent.Status().Idle {
				launch(cmd)
			}
		case <-deadline:
			t.Fatal("run completion did not reach the application")
		}
	}
}

func (m *Model) applyValues(values map[string]string) error {
	if m.form.dialog == m.chrome.Dialog && m.form.apply != nil {
		return m.applySubmission(m.chrome.Dialog, values)
	}
	dialog := m.chrome.Dialog
	m.workflows.HandleControl(screen.Event{Kind: screen.Apply, Dialog: dialog, Values: values})
	if dialog.Form.Error != "" {
		return fmt.Errorf("%s", dialog.Form.Error)
	}
	return nil
}
