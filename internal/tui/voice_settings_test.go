package tui

import (
	"context"
	"testing"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
)

func configPrefsWithModeAgent() config.Preferences {
	return config.Preferences{Version: 1, Mode: "agent", Voice: "live"}
}

func voiceSettingsModel(t *testing.T) *model {
	t.Helper()
	m := settingsModel(t)
	m.state = stDone
	m.chat.config.Profile.Speech = &speech.Config{
		Microphone: "mic", Recorder: "ffmpeg", PlayerBinary: "ffplay", MaxSeconds: 120,
		Speaking:  speech.Speaking{Provider: "chatterbox", Variant: "nano", Binary: "python", Model: "assets/models/speaking/chatterbox/nano"},
		Listening: speech.Listening{Ownership: "external", URL: "http://127.0.0.1:19082"},
	}
	m.chat.config.NewVoice = func(context.Context, string) (speech.Service, error) { return &voiceService{}, nil }
	if err := m.enableVoice(); err != nil {
		t.Fatal(err)
	}
	return m
}

// startHandsFree marks the session as capturing hands-free. NewLive builds
// the capture without starting a microphone, which is all this test needs.
func startHandsFree(t *testing.T, m *model) {
	t.Helper()
	capture, err := speech.NewLive(context.Background(), "ffmpeg.exe", "mic", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	m.listening = capture
}

// Opening a settings page stops capture, so reading the live voice mode at
// apply time always answered push-to-talk. A reader in hands-free who
// opened any settings page and pressed Apply had their voice input
// downgraded for the session and every launch after it.
func TestOpeningSettingsDoesNotDowngradeHandsFree(t *testing.T) {
	m := voiceSettingsModel(t)
	startHandsFree(t, m)
	if got := m.liveVoiceMode(); got != "live" {
		t.Fatalf("setup: hands-free not active, mode = %q", got)
	}

	m.editSettings("you")
	if m.listening != nil {
		t.Fatal("opening settings should still stop capture")
	}
	if got := m.currentVoiceMode(); got != "live" {
		t.Fatalf("voice mode after opening settings = %q, want live", got)
	}
	focusSetting(m, "user")
	m.dialog.form.Editor.SetValue("Renamed")
	focusSetting(m, "persona")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.application != nil && m.application.Preferences.Voice != "live" {
		t.Fatalf("persisted Voice = %q, want live", m.application.Preferences.Voice)
	}
	if !m.voiceEnabled {
		t.Fatal("voice was switched off by opening a settings page")
	}
}

// The Application page's Voice field used to be built from the live mode
// and compared against the torn-down one, so merely opening it reported
// itself as changed and put a mode switch in the ModeChange.
func TestApplicationPageDoesNotReportItselfChanged(t *testing.T) {
	m := voiceSettingsModel(t)
	startHandsFree(t, m)
	m.application = &Application{Preferences: configPrefsWithModeAgent()}
	m.editSettings("application")
	if m.settingsApplicationChanged() {
		t.Fatal("opening the Application page must not read as a change to it")
	}
}

// Previewing a voice sample enables voice, which flips Speech.Enabled on
// the live profile. The draft is a snapshot from before, so the next
// Apply read that as an edited speech setting and switched voice off —
// reporting only that settings had been applied.
func TestPreviewingVoiceSampleSurvivesAnUnrelatedApply(t *testing.T) {
	m := voiceSettingsModel(t)

	m.editSettings("you") // draft snapshot taken here, Enabled=false
	if m.dialog == nil {
		t.Fatal("you form did not open")
	}
	if err := m.enableVoice(); err != nil { // what Ctrl+P does inside the dialog
		t.Fatal(err)
	}
	if !m.chat.config.Profile.Speech.Enabled {
		t.Fatal("preview did not enable the live profile")
	}

	focusSetting(m, "user")
	m.dialog.form.Editor.SetValue("Renamed")
	focusSetting(m, "persona")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if !m.voiceEnabled {
		t.Fatal("an unrelated Apply switched voice off")
	}
}

// Enabled is runtime state. Only the settings themselves count.
func TestSpeechSettingsChangedIgnoresEnabled(t *testing.T) {
	base := &speech.Config{Microphone: "mic", MaxSeconds: 30, Enabled: false}
	on := *base
	on.Enabled = true
	if speechSettingsChanged(&on, base) {
		t.Fatal("Enabled is runtime state, not an edited setting")
	}
	edited := *base
	edited.Microphone = "other"
	if !speechSettingsChanged(&edited, base) {
		t.Fatal("a real speech edit must be detected")
	}
	if speechSettingsChanged(nil, nil) {
		t.Fatal("no speech on either side is not a change")
	}
	if !speechSettingsChanged(nil, base) || !speechSettingsChanged(base, nil) {
		t.Fatal("adding or removing speech must count as a change")
	}
}