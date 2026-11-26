package settings

import (
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func CatalogDetail(key, path string) string {
	if key == "prompt-preset" {
		preset, err := prompts.ResponsePreset(path)
		if err != nil {
			return err.Error()
		}
		if preset.ID == "custom" {
			return "Custom" + screen.Separator() + "Uses your base prompt without adding a response-style preset."
		}
		return preset.Name + "" + screen.Separator() + "" + preset.Instruction
	}
	if key == "load-profile" {
		p, err := config.Load(path)
		if err != nil {
			return "Unavailable profile: " + err.Error()
		}
		return p.Provider.Kind + "" + screen.Separator() + "" + p.Provider.Model + "" + screen.Separator() + "" + path
	}
	return path
}

func ChoiceLabel(key, value string) string {
	switch key {
	case "network-type":
		switch value {
		case "":
			return "Use environment"
		case "http":
			return "HTTP"
		case "https":
			return "HTTPS"
		case "socks5":
			return "SOCKS5"
		}
	case "auth":
		switch providers.KeyMode(value) {
		case providers.KeyNone:
			return "None"
		case providers.KeyEnv:
			return "Environment variable"
		case providers.KeySaved:
			return "Saved key"
		}
	case "kind":
		if descriptor, err := providers.Lookup(value); err == nil {
			return descriptor.Label
		}
	case "reasoning-effort":
		if value == "default" {
			return "Model default"
		}
	case "share-user-name":
		if value == "true" {
			return "On"
		}
		return "Off"
	case "reasoning-summary":
		if value == "true" {
			return "On (summary)"
		}
		return "Off"
	case "prompt-preset":
		preset, err := prompts.ResponsePreset(value)
		if err == nil {
			return preset.Name
		}
	case "theme":
		switch value {
		case "dark":
			return "Dark"
		case "light":
			return "Light"
		case "one-half-dark":
			return "One Half Dark"
		case "one-half-light":
			return "One Half Light"
		case "solarized-dark":
			return "Solarized Dark"
		case "solarized-light":
			return "Solarized Light"
		}
		return value
	}
	return value
}

// Hint explains what each section changes and when it takes effect.
func Hint(section string) string {
	switch section {
	case "network":
		return "Apply restarts GEKTOR with this proxy. Use environment reads the proxy variables."
	case "you":
		return "Sharing adds your name to the Agent prompt from the next turn. Saved messages are kept."
	case "prompts":
		return "Applies to the next Agent turn and future launches. Existing messages remain unchanged."
	case "workspace":
		return "Changes the working directory for subsequent operations. The conversation and draft are preserved."
	case "profiles":
		return "Selects the model and Agent configuration for subsequent requests and future launches. Existing messages and recipes remain unchanged."
	case "model":
		return "Connection used by Agent and Workflow AI drafting. Applies to subsequent requests and future launches. Recipe node settings remain unchanged."
	case "generation":
		return "Request limits used by Agent and Workflow AI drafting. Applies to subsequent requests and future launches. Recipe node settings remain unchanged."
	}
	return "Changes apply to subsequent requests and future launches."
}

func Choices(key string) []string {
	switch key {
	case "prompt-preset":
		ids := []string{}
		for _, preset := range prompts.Presets() {
			ids = append(ids, preset.ID)
		}
		return ids
	case "prefer-main", "prefer-post", "reasoning-summary", "share-user-name":
		return []string{"true", "false"}
	case "network-type":
		return []string{"", "http", "https", "socks5"}
	case "auth":
		return []string{string(providers.KeyNone), string(providers.KeyEnv), string(providers.KeySaved)}
	case "kind":
		return providers.Kinds()
	}
	return nil
}

// SuggestedBackendURL changes only the previous protocol's local default.
// OpenAI denotes a compatible API, so its suggestion stays local.
func SuggestedBackendURL(endpoint, previous, next string) string {
	return providers.SuggestedURL(endpoint, previous, next)
}

type Category struct{ ID, Label string }

func categories() []Category {
	return []Category{{"model", "Model connection"}, {"generation", "Response settings"}, {"profiles", "Launch profiles"}, {"workspace", "Workspace"}, {"you", "Identity and persona"}, {"prompts", "Agent instructions"}, {"network", "Network"}}
}
func SectionTitle(id string) string {
	for _, category := range categories() {
		if category.ID == id {
			return category.Label
		}
	}
	return "Settings"
}
