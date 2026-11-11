package agent

import (
	"path/filepath"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
)

// ModeChange carries preferences and retained Agent input into a new launch.
// It is returned as an error to the application's relaunch loop.
type ModeChange struct {
	Preferences       config.Preferences
	History           []llm.Message
	Draft             string
	Images            []string
	Drafts            Drafts
	Directory         string
	KeepAct           bool
	ReassignWorkspace bool
}

func (m *ModeChange) Error() string { return "application settings changed" }

func (m *Model) sessionDirectory() string {
	if m.stateFile == "" {
		return ""
	}
	return filepath.Dir(m.stateFile)
}

func (m *Model) profile() *config.Profile {
	if m.config == nil {
		return nil
	}
	return &m.config.Profile
}
func (m *Model) savePreferences() error {
	return m.application.Save(m.profile(), m.modeName(), m.currentAppearance())
}
func (m *Model) saveHarness() error {
	if m.application == nil {
		return nil
	}
	h := m.application.Preferences.Harness
	switch {
	case m.maxStepsOverride == noStepLimit:
		h.MaxSteps = unlimitedSteps
	case m.maxStepsOverride > 0:
		h.MaxSteps = m.maxStepsOverride
	}
	h.Steering = m.steering
	return m.application.SaveHarness(h, m.profile(), m.modeName(), m.currentAppearance())
}
func (m *Model) saveDefaultMode() error {
	return m.application.SaveMode(m.modeName(), m.profile(), m.currentAppearance())
}

// Agent installs its current system instruction when resuming. Never carry the
// old session's leading prompt as a second, competing system instruction.
func agentSeed(history []llm.Message) []llm.Message {
	for len(history) > 0 && history[0].Role == llm.RoleSystem {
		history = history[1:]
	}
	return append([]llm.Message{{Role: llm.RoleSystem}}, history...)
}
