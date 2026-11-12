package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

func testModel() *Model {
	m := &Model{
		chrome:       &screen.Controls{},
		sessionsRoot: userdirs.Sub("tasks"),
		events:       make(chan events.Event, 16),
		hub:          harness.NewGateHub(nil),
		cancel:       func() {},
		started:      time.Now(),
		styles:       defaultStyles(),
		transcript:   newTranscript(), maxSteps: agent.DefaultMaxSteps,
		note: newNote(),
	}
	m.input = newInput()
	m.input.Prompt = "> "
	m.input.Focus()
	m.bindControls()
	return m
}

// tagged is the default display state: label rows on, content
// truncated. Hiding labels is a View-menu choice, so tests that exercise
// it ask for renderOpts{labels: false} by name rather than inheriting a
// zero value that silently means "off".
func tagged() renderOpts { return renderOpts{labels: true} }

func sizeModel(t *testing.T, m *Model) *Model {
	t.Helper()
	return sizeModelTo(t, m, 80, 24)
}

func sizeModelTo(t *testing.T, m *Model, width, height int) *Model {
	t.Helper()
	updated, _ := m.testUpdate(tea.WindowSizeMsg{Width: width, Height: height})
	mm, ok := updated.(*Model)
	if !ok {
		t.Fatal("Update did not return *Model")
	}
	return mm
}

func seedHistory(t *testing.T, path string) {
	t.Helper()
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	data, err := json.Marshal(conversation.New(history))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

type stubDone struct{}

func (stubDone) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

// consecutiveBlanks counts the longest run of visually blank lines
// (ANSI gutter + whitespace only). The block join contributes exactly
// one; anything more is a rendering defect.
func consecutiveBlanks(s string) int {
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	best, run := 0, 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(ansi.ReplaceAllString(line, "")) == "" {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}

// openPermissionGate drives a permission suspension the way the run
// worker would, returning the model with the gate overlay open.
func openPermissionGate(t *testing.T) (*Model, chan agent.SuspendReply) {
	t.Helper()
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "awaiting_input",
		Fields: map[string]any{
			"kind": "permission", "prompt": "allow?",
			"tool": "run_shell", "resource": "go test",
		},
	}))
	mm := updated.(*Model)
	if mm.state != stPermission || mm.gstage != gsPermit {
		t.Fatalf("state = %v stage = %v", mm.state, mm.gstage)
	}
	if len(mm.overlays) != 1 {
		t.Fatalf("gate must push one overlay, have %d", len(mm.overlays))
	}
	answered := make(chan agent.SuspendReply, 1)
	go func() {
		rep, err := mm.hub.Suspender()(t.Context(), agent.SuspendRequest{Kind: agent.SuspendPermission})
		if err != nil {
			return
		}
		answered <- rep
	}()
	waitPending(t, mm.hub)
	return mm, answered
}

func waitPending(t *testing.T, hub *harness.GateHub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !hub.HasPending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !hub.HasPending() {
		t.Fatal("gate never went pending")
	}
}

func awaitReply(t *testing.T, ch chan agent.SuspendReply) agent.SuspendReply {
	t.Helper()
	select {
	case rep := <-ch:
		return rep
	case <-time.After(2 * time.Second):
		t.Fatal("suspender never woke")
		return agent.SuspendReply{}
	}
}

func keyRunes(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: string([]rune{r})}
}

// ansiRE matches the SGR escapes lipgloss emits, so tests can assert on
// rendered text without depending on whether the run has a colour profile.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// helpText flattens the help data as plain lines for assertions. Tests
// need plain text; the only renderer the app ships is helpRows, which
// emits styled, per-section lines, so there is nothing to reuse here.
func helpText() []string {
	var out []string
	for _, s := range helpSections() {
		out = append(out, "# "+s.title)
		for _, r := range s.rows {
			out = append(out, "  "+r[0]+"  "+r[1])
		}
	}
	return out
}

// advanceFrames runs the reveal the way the tick loop does, until the
// queue is empty. Deltas are received but not shown until a frame
// moves them into the transcript.
func advanceFrames(t *testing.T, m tea.Model) *Model {
	t.Helper()
	mm, ok := m.(*Model)
	if !ok {
		t.Fatal("Update did not return *Model")
	}
	// Both live buffers: a turn can be revealing deliberation when no
	// answer text has arrived yet.
	for i := 0; i < 8192 && (mm.liveQueue != "" || mm.liveThinkQueue != ""); i++ {
		mm.testUpdate(tickMsg(time.Now()))
	}
	return mm
}

func headerText(items []screen.MenuItem) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString(item.Text + "\n")
	}
	return b.String()
}

// scriptClient supplies deterministic local model responses.
type scriptClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f scriptClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

// settingsModel supplies a configured conversation and saved history for Agent tests.
func settingsModel(t *testing.T) *Model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Identity: &config.Identity{UserName: "User", SystemPrompt: "Base rules"}}
	client := scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	sessions := t.TempDir()
	workspaceDir := t.TempDir()
	ws, err := workspace.New(workspaceDir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.wsRoot = ws.Root()
	m.baseEnv = roles.Env{Client: client, WS: ws, MaxTokens: 100, ContextLimit: 1000}
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		return harness.New(harness.Config{Env: m.sessionEnv(), Task: task, StateFile: stateFile, Plan: m.plan, Images: images})
	}
	id, _, err := app.CompileIdentity(app.IdentityOptions{Preset: profile.Identity.PromptPreset, Name: profile.Identity.Name, Persona: profile.Identity.UserPersona, User: profile.Identity.UserName, System: profile.Identity.SystemPrompt, Post: profile.Identity.PostHistory, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		t.Fatal(err)
	}
	m.config = &app.PreparedProfile{Identity: id, Directory: sessions, Profile: profile}
	m.sessionsRoot = sessions
	m.application = &settings.Application{
		Preferences:    config.Preferences{Version: 1, Workspace: workspaceDir},
		PreferencePath: filepath.Join(t.TempDir(), "application.json"),
	}
	m.stateFile = filepath.Join(sessions, "original", "state.json")
	m.sessionTitle = "Original"
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []llm.Message{
		{Role: llm.RoleSystem, Content: "Base rules"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "reply"},
	}
	if err := agent.SaveState(m.stateFile, saved); err != nil {
		t.Fatal(err)
	}
	return m
}

// eventVisitor supplies per-event assertions at the test boundary.
type eventVisitor func(events.Event)

func (visit eventVisitor) Handle(event events.Event) {
	visit(event)
}
