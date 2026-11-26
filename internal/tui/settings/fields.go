package settings

import (
	"os"
	"strconv"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/pkg/tui/components/form"
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
		add("share-user-name", "Share name with Agent", strconv.FormatBool(identity.ShareUserName), false)
		add("persona", "Your persona description", identity.UserPersona, true)
	case "generation":
		add("temperature", "Temperature", temperatureValue(profile.Generation.Temperature), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		if _, err := providers.Lookup(profile.Provider.Kind); err == nil {
			value := "Auto"
			if profile.Generation.ContextLimit > 0 {
				value = strconv.Itoa(profile.Generation.ContextLimit)
			}
			add("context", "Context window (Auto or tokens)", value, false)
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
		mode, env := authenticationFields(profile.Provider)
		add("auth", "Authentication", string(mode), false)
		add("key-env", "API key environment variable", env, false)
	case "network":
		network := preferences.Network
		add("network-type", "Proxy type", network.Type, false)
		add("network-host", "Proxy host", network.Host, false)
		add("network-port", "Proxy port", network.Port, false)
	}
	return title, fields
}

func authenticationFields(provider config.Provider) (providers.KeyMode, string) {
	if provider.KeyMode != "" {
		return provider.KeyMode, provider.APIKeyEnv
	}

	d, err := providers.Lookup(provider.Kind)
	if err != nil || d.Auth != providers.Bearer {
		return providers.KeyNone, ""
	}
	mode := d.DefaultKeyMode(provider.URL, provider.APIKeyEnv)
	if mode != "" {
		return mode, provider.APIKeyEnv
	}
	if os.Getenv(d.DefaultKeyEnv) != "" {
		return providers.KeyEnv, d.DefaultKeyEnv
	}
	return providers.KeySaved, ""
}
