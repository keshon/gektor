package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "Review the media tools. Keep the implementation native where possible."},
		{Role: llm.RoleUser, Content: "[harness] Workspace restored. Continuing the saved conversation."},
		{Role: llm.RoleAssistant, Reasoning: "Check the shared capability contracts and compare both adapters.", ToolCalls: []llm.ToolCall{{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"internal/capabilities/media/service.go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "read-1", Content: "Native YouTube downloads and optional FFmpeg processing."},
		{Role: llm.RoleAssistant, Content: "The tools and nodes use the same media capability. Native downloads stay separate from conversion, and backend changes remain explicit.\n\nI found one issue to fix: source selection should check the uploader and duration before downloading."},
	}
	defer screen.SetTheme("dark")
	defer screen.SetModern(false)
	for _, theme := range []string{"dark", "light"} {
		m := shellTestModel(t, Config{AgentConfig: AgentConfig{SeedHistory: history, InitialDraft: "Show me the proposed change", Env: roles.Env{Model: "Ornith", ContextLimit: 131072}}})
		m.colorProfile = colorprofile.TrueColor
		m.applyTheme(theme)
		m.agent.ApplyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true})
		sizeModelTo(t, m, 120, 30)
		if err := os.WriteFile(filepath.Join(dir, "modern-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
		m.runAction("about")
		m.settleControls()
		if err := os.WriteFile(filepath.Join(dir, "modern-modal-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}

	}
}
