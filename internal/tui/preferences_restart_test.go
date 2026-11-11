package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestPreferenceRestartHelper(t *testing.T) {
	path := os.Getenv("GEKTOR_TEST_PREFERENCES")
	if path == "" {
		t.Skip("subprocess helper")
	}
	p, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	m := settingsModelFor(t, &p)
	*m.application = Application{PreferencePath: path, Preferences: p, Temporary: true}
	m.profile.Profile = p.Profile.Clone()
	m.applyTheme(p.Theme)
	m.agent.ApplyAppearance(p.Appearance)
	phase := os.Getenv("GEKTOR_TEST_PREF_PHASE")
	if phase == "verify" || phase == "verify-reset" {
		on := phase == "verify"
		if m.agent.Appearance().ExpandThinking != on || m.agent.Appearance().HideTools != on || m.agent.Appearance().HideSystem != on || m.agent.Appearance().HideNames != on || m.agent.Appearance().HideLabels != on || (m.agent.Appearance().Frame != "outlined") == on {
			t.Fatal("View choices lost on restart", p.Appearance)
		}
		wantTheme := "light"
		if !on {
			wantTheme = "dark"
		}
		if currentPalette().Name != wantTheme || snapshotOf(m).KeepAct || p.Harness.MaxSteps != 42 {
			t.Fatal("theme/mode/step limit lost", p.Theme, p.Mode, p.Harness)
		}
		profile := p.Profile
		if profile.Provider.Kind != "openai" || profile.Provider.URL != "http://127.0.0.1:12345/v1" || profile.Provider.Model != "fixture-model" || profile.Provider.APIKeyEnv != "GEKTOR_TEST_KEY" {
			t.Fatal("model settings lost", profile.Provider)
		}
		if profile.Generation.Temperature != .42 || profile.Generation.MaxTokens != 1536 || profile.Generation.ContextLimit != 12288 {
			t.Fatal("generation settings lost", profile.Generation)
		}
		id := profile.Identity
		if id.UserName != "Operator" || id.UserPersona != "Precise" || id.PromptPreset != "custom" || id.SystemPrompt != "BASE_RULES" || id.PostHistory != "POST_RULES" {
			t.Fatal("identity/prompt settings lost", id)
		}
		if p.Workspace != filepath.Join(filepath.Dir(path), "workspace") {
			t.Fatal("workspace lost", p.Workspace)
		}
		if _, err := config.Load(p.ProfilePath); err != nil {
			t.Fatal("selected profile not durable", err)
		}
		return
	}
	for _, command := range []string{"toggle-details", "toggle-tools", "toggle-sys", "toggle-names", "toggle-labels"} {
		m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "view"}
		m.chrome.Header.Row = headerRowFor(m, command)
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.Dialog != nil {
			t.Fatal("View save failed", m.chrome.Dialog.Lines)
		}
	}
	m.openAppearance()
	frame := "minimal"
	if m.agent.Appearance().Frame != "outlined" {
		frame = "outlined"
	}
	focusSetting(m, "frame")
	m.chrome.Dialog.Form.Editor.SetValue(frame)
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatal("Appearance save failed", m.chrome.Dialog.Form.Error)
	}
	if phase == "reset" {
		m.command("theme dark")
		return
	}
	m.command("theme light")
	m.command("mode plan")
	m.command("max-steps 42")
	for _, edit := range [][3]string{
		{"model", "kind", "openai"}, {"model", "endpoint", "http://127.0.0.1:12345/v1"}, {"model", "model", "fixture-model"}, {"model", "key-env", "GEKTOR_TEST_KEY"},
		{"generation", "temperature", "0.42"}, {"generation", "max", "1536"}, {"generation", "context", "12288"},
		{"you", "user", "Operator"}, {"you", "persona", "Precise"},
		{"prompts", "prompt-preset", "custom"}, {"prompts", "system", "BASE_RULES"}, {"prompts", "post", "POST_RULES"},
		{"workspace", "app-workspace", filepath.Join(filepath.Dir(path), "workspace")},
	} {
		m.editSettings(edit[0])
		if edit[1] == "key-env" {
			m.formKey(tea.KeyPressMsg{Code: tea.KeyF4})
		}
		focusSetting(m, edit[1])
		m.chrome.Dialog.Form.Editor.SetValue(edit[2])
	}
	m.applyForm()
	if m.modeChange == nil {
		t.Fatal("Settings Apply failed", m.chrome.Dialog)
	}
	// The application loop persists this payload before replacing services.
	if err := config.SavePreferences(path, m.modeChange.Preferences); err != nil {
		t.Fatal(err)
	}
}

// Each phase starts a new process with only the persisted file in common.
func TestPreferencesSurviveProcessRestart(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	profile := config.DefaultProfile()
	profile.Provider.Kind = "openai"
	path := filepath.Join(root, "application.json")
	if err := config.SavePreferences(path, config.Preferences{Version: 1, Profile: &profile, Workspace: workspace, Mode: "act", Appearance: config.Appearance{}}); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"edit", "verify", "reset", "verify-reset"} {
		cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestPreferenceRestartHelper$")
		cmd.Env = append(os.Environ(), "GEKTOR_TEST_PREFERENCES="+path, "GEKTOR_TEST_PREF_PHASE="+phase)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", phase, err, out)
		}
	}
}
