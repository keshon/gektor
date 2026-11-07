package settings

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
)

func PickerFiles(key string) bool {
	return key == "load-profile" || key == "app-workspace"
}

func PickerControl(key string) bool {
	return PickerFiles(key) || key == "model" || key == "reasoning-effort"
}

func PickerAccepts(key, path string) bool {
	switch key {
	case "load-profile":
		return strings.EqualFold(filepath.Ext(path), ".json")
	}
	return false
}

func CatalogFiles(root, key string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && PickerAccepts(key, path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func CatalogDetail(key, path string) string {
	if key == "prompt-preset" {
		preset, err := prompts.ResponsePreset(path)
		if err != nil {
			return err.Error()
		}
		if preset.ID == "custom" {
			return "Custom · Uses your base prompt without adding a response-style preset."
		}
		return preset.Name + " · " + preset.Instruction
	}
	if key == "load-profile" {
		p, err := config.Load(path)
		if err != nil {
			return "Unavailable profile: " + err.Error()
		}
		return p.Provider.Kind + " · " + p.Provider.Model + " · " + path
	}
	return path
}

func ChoiceLabel(key, value string) string {
	switch key {
	case "kind":
		if descriptor, err := providers.Lookup(value); err == nil {
			return descriptor.Label
		}
	case "reasoning-effort":
		if value == "default" {
			return "Model default"
		}
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

func Hint(section string) string {
	switch section {
	case "profiles":
		return "Profiles save your setup, including the identity it speaks with."
	case "you":
		return "Your name and persona are used by the compiled prompts."
	case "application":
		return "The workspace tasks run in."
	case "model":
		return "Choose the thinking model."
	case "generation":
		return "Response budget and context window for one turn."
	case "prompts":
		return "Preset adds response style to the compiled instruction."
	}
	return "Changes apply to this session; history stays intact."
}

func Choices(key string) []string {
	switch key {
	case "prompt-preset":
		ids := []string{}
		for _, preset := range prompts.Presets() {
			ids = append(ids, preset.ID)
		}
		return ids
	case "prefer-main", "prefer-post", "reasoning-summary":
		return []string{"true", "false"}
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
