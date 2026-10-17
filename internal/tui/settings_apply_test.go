package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/app"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
)

func TestTUIRuntimeOwnershipOnStartupFailure(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		client := &settingsClient{}
		services := &ChatConfig{Client: client, Directory: t.TempDir()}
		cfg := Config{Chat: services}
		if borrowed {
			cfg.Runtime = app.NewRuntime(services)
		}
		if _, err := Run(context.Background(), cfg); err == nil {
			t.Fatal("invalid conversation settings accepted")
		}
		want := 1
		if borrowed {
			want = 0
		}
		if client.closed != want {
			t.Fatal("TUI closed a borrowed runtime or leaked its fallback", client.closed, borrowed)
		}
		if borrowed {
			cfg.Runtime.Close()
			if client.closed != 1 {
				t.Fatal("owner failed to close borrowed runtime")
			}
		}
	}
}

type settingsClient struct {
	llm.Client
	closed int
}

func (c *settingsClient) Close() { c.closed++ }

func TestSettingsSaveFailurePreservesRuntimeAndAllFiles(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-client", true: "reuse-client"}[reuse], func(t *testing.T) {
			m := settingsModel(t)
			root := t.TempDir()
			profile := m.chat.config.Profile.Clone()
			profile.Provider.ModelFile = filepath.Join(root, "assets", "model.gguf")
			m.chat.config.Profile = profile.Clone()
			custom := filepath.Join(root, "profiles", "custom", "custom-test.json")
			if err := config.SaveCopy(profile, custom); err != nil {
				t.Fatal(err)
			}
			m.application = &Application{PreferencePath: filepath.Join(root, "var", "application.json"), Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", ProfilePath: custom, Profile: &profile}}
			if err := m.saveSessionSettings(); err != nil {
				t.Fatal(err)
			}
			// A directory at the global file path forces a late preparation failure.
			if err := os.MkdirAll(m.application.PreferencePath, 0700); err != nil {
				t.Fatal(err)
			}
			paths := []string{custom, m.stateFile, filepath.Join(m.sessionDirectory(), "settings.json")}
			before := make(map[string]string)
			for _, path := range paths {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(b)
			}
			old := &settingsClient{Client: m.chat.config.Client}
			candidate := &settingsClient{Client: m.chat.config.Client}
			m.chat.config.Client = old
			current := m.chat.current
			configure := m.chat.config.Configure
			m.chat.config.Configure = func(p config.Profile) (ChatConfig, error) {
				next, err := configure(p)
				next.Client = candidate
				return next, err
			}
			m.editSettings("generation")
			if !reuse {
				focusSetting(m, "context")
				m.dialog.form.Editor.SetValue("2000")
			} else {
				m.dialog.form.Editor.SetValue("0.8")
			}
			m.applyForm()
			if m.dialog == nil || m.dialog.form.Error == "" {
				t.Fatal("save failure accepted")
			}
			if m.chat.config.Client != old || m.chat.current != current || old.closed != 0 || candidate.closed != 1 {
				t.Fatal("failed apply changed runtime ownership", old.closed, candidate.closed)
			}
			if m.application.Preferences.ProfilePath != custom || m.dialog.form.Workspace.AutoSaved {
				t.Fatal("failed apply changed profile selection")
			}
			for _, path := range paths {
				b, err := os.ReadFile(path)
				if err != nil || string(b) != before[path] {
					t.Fatal("failed apply changed persisted file", path, err)
				}
			}
			// Correct the storage failure and retry the same draft.
			if err := os.Remove(m.application.PreferencePath); err != nil {
				t.Fatal(err)
			}
			candidate = &settingsClient{Client: candidate.Client}
			m.applyForm()
			if m.dialog != nil {
				t.Fatal("retry failed", m.dialog.form.Error)
			}
			if reuse {
				if m.chat.config.Client != old || old.closed != 0 || candidate.closed != 1 {
					t.Fatal("unchanged provider not reused")
				}
			} else if m.chat.config.Client != candidate || old.closed != 1 || candidate.closed != 0 {
				t.Fatal("successful apply lost client ownership")
			}
			loaded, err := config.Load(custom)
			if err != nil || loaded.Generation != m.chat.config.Profile.Generation || loaded.Provider.ModelFile != profile.Provider.ModelFile {
				t.Fatal("retry did not persist staged generation", err)
			}
		})
	}
}

// Resolve writes neither Profiles nor Application, so a dirty draft made
// only of those pages produced no change and no file — while the
// transcript claimed the settings were applied.
func TestProfilesApplyDoesNotClaimSuccess(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("profiles")
	ws := m.settingsWorkspace()
	if len(appliedSections(ws)) != 0 {
		t.Fatal("precondition: the Profiles page has nothing Resolve writes")
	}
	focusSetting(m, "profile-name")
	m.dialog.form.Editor.SetValue("copy")
	focusSetting(m, "load-profile")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	for _, b := range m.transcript.blocks {
		if b.role == roleMarker && strings.Contains(b.text, "Settings applied") {
			t.Fatal("claimed settings were applied when the Profiles page writes nothing")
		}
	}
}

// A profile loaded from disk has nothing to diff but must still take
// effect, or loading a setup would silently do nothing.
func TestLoadedProfileStillAppliesWithNoFieldEdits(t *testing.T) {
	m := settingsModel(t)
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off"}}
	root := t.TempDir()
	p := m.chat.config.Profile.Clone()
	p.Conversation.UserName = "Alex"
	path := filepath.Join(root, "custom.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.openSettingsPicker()
	picker := m.dialog.form.Picker
	m.acceptSettingsScan(settingsScanMsg{ID: picker.ID, Items: []string{path}})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.editSettings("generation")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.chat.config.Profile.Conversation.UserName != "Alex" {
		t.Fatalf("loaded profile was not committed: %q", m.chat.config.Profile.Conversation.UserName)
	}
}

func TestSettingsInvalidCandidateClosesOnlyCandidate(t *testing.T) {
	m := settingsModel(t)
	old := &settingsClient{Client: m.chat.config.Client}
	candidate := &settingsClient{Client: old.Client}
	m.chat.config.Client = old
	configure := m.chat.config.Configure
	m.chat.config.Configure = func(p config.Profile) (ChatConfig, error) {
		next, err := configure(p)
		next.Client = candidate
		next.Settings = chat.Config{} // Configure succeeded, conversation validation fails.
		return next, err
	}
	m.editSettings("model")
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("changed-model")
	m.applyForm()
	if m.dialog == nil || old.closed != 0 || candidate.closed != 1 || m.chat.config.Client != old {
		t.Fatal("invalid candidate leaked or replaced active client")
	}
}
