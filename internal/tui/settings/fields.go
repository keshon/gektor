package settings

import (
	"strconv"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/tui/components/form"
)

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
	title := ""
	switch section {
	case "application":
		title = "Application"
		p := preferences
		add("app-workspace", "Workspace", p.Workspace, false)
	case "profiles":
		title = "Profiles"
		add("load-profile", "Load setup from profile", "", false)
		add("profile-name", "Save current setup as (Ctrl+D)", "custom", false)
	case "you":
		title = "You"
		add("user", "Your name", identity.UserName, false)
		add("persona", "Your persona description", identity.UserPersona, true)
	case "generation":
		title = "Generation"
		add("temperature", "Temperature (0 = default 0.4)", strconv.FormatFloat(profile.Generation.Temperature, 'f', -1, 64), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		add("context", "Context window (tokens)", strconv.Itoa(profile.Generation.ContextLimit), false)
	case "prompts":
		title = "Identity prompts"
		preset, _ := prompts.ResponsePreset(identity.PromptPreset)
		add("prompt-preset", "Prompt preset", preset.ID, false)
		add("system", "Base system prompt", identity.SystemPrompt, true)
		add("post", "Base post-history directive", identity.PostHistory, true)
	case "model":
		title = "Model"
		add("kind", "Protocol (llama/kobold/openai)", profile.Provider.Kind, false)
		add("endpoint", "Backend URL", profile.Provider.URL, false)
		add("model", "Model identifier", profile.Provider.Model, false)
		add("key-env", "API key environment variable", profile.Provider.APIKeyEnv, false)
	}
	return title, fields
}
