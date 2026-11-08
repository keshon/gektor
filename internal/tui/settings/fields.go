package settings

import (
	"strconv"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/tui/components/form"
)

// DefaultTemperature is what the backend uses when a profile sets none.
//
// It lives next to the value it describes, in config, so the field that has
// to name it and the validator that has to accept it cannot disagree.
const DefaultTemperature = config.DefaultTemperature

// Keep editable values parseable; the note displays the effective default.
func temperatureValue(t float64) string {
	if t == 0 {
		return "default"
	}
	return strconv.FormatFloat(t, 'g', -1, 64)
}

func PageFields(profile config.Profile, section string, preferences config.Preferences) (string, []form.Field) {
	identity := config.Identity{UserName: config.DefaultUserName}
	if profile.Identity != nil {
		identity = *profile.Identity
	}
	profile.Identity = &identity
	fields := []form.Field{}
	add := func(key, label, value string, multi bool) {
		fields = append(fields, form.Field{Key: key, Label: label, Value: value, Multiline: multi})
	}
	title := SectionTitle(section)
	switch section {
	case "workspace":
		p := preferences
		add("app-workspace", "Workspace", p.Workspace, false)
	case "profiles":
		add("load-profile", "Load setup from profile", "", false)
		add("profile-name", "Save current setup as (Ctrl+D)", "custom", false)
	case "you":
		add("user", "Your name", identity.UserName, false)
		add("persona", "Your persona description", identity.UserPersona, true)
	case "generation":
		add("temperature", "Temperature", temperatureValue(profile.Generation.Temperature), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		if d, err := providers.Lookup(profile.Provider.Kind); err == nil && d.SelectModel {
			add("context", "Context window (tokens)", strconv.Itoa(profile.Generation.ContextLimit), false)
		}
		effort := profile.Generation.ReasoningEffort
		if effort == "" {
			effort = "default"
		}
		add("reasoning-effort", "Reasoning effort", effort, false)
		add("reasoning-summary", "Show reasoning summary", strconv.FormatBool(profile.Generation.ReasoningSummary), false)
	case "prompts":
		preset, _ := prompts.ResponsePreset(identity.PromptPreset)
		add("prompt-preset", "Prompt preset", preset.ID, false)
		add("system", "Base system prompt", identity.SystemPrompt, true)
		add("post", "Base post-history directive", identity.PostHistory, true)
	case "model":
		add("kind", "Provider", profile.Provider.Kind, false)
		add("endpoint", "Backend URL", profile.Provider.URL, false)
		add("model", "Model identifier", profile.Provider.Model, false)
		add("key-env", "API key environment variable", profile.Provider.APIKeyEnv, false)
	}
	return title, fields
}
