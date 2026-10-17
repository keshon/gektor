package settings

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

func PickerFiles(key string) bool {
	return key == "model-file" || key == "character" || key == "load-profile" || key == "app-workspace"
}

func PickerControl(key string) bool { return PickerFiles(key) || key == "model" || key == "microphone" }

func PickerAccepts(key, path string) bool {
	switch key {
	case "model-file":
		return strings.EqualFold(filepath.Ext(path), ".gguf")
	case "character":
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".png" || ext == ".json"
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
			return "Custom · Uses your base prompt and card overrides without adding a response-style preset."
		}
		return preset.Name + " · " + preset.Instruction
	}
	if key == "character" {
		card, err := character.Load(path)
		if err != nil {
			return "Unavailable card: " + err.Error()
		}
		return card.Name + " · " + strings.ReplaceAll(card.Description, "\n", " ") + " · " + path
	}
	if key == "load-profile" {
		p, err := config.Load(path)
		if err != nil {
			return "Unavailable profile: " + err.Error()
		}
		return p.Provider.Kind + " · " + p.Provider.Ownership + " · " + p.Provider.Model + " · " + path
	}
	return path
}

// Async discovery captures the configured recorder but never captures audio.

func ChoiceLabel(key, value string) string {
	switch key {
	case "prompt-preset":
		preset, err := prompts.ResponsePreset(value)
		if err == nil {
			return preset.Name
		}
	case "greeting":
		if value == "0" {
			return "Main greeting"
		}
		return "Alternate greeting " + value
	case "theme":
		if value == "light" {
			return "Light"
		}
		return "Dark"
	case "app-mode":
		if value == "chat" {
			return "Chat · no tools"
		}
		if value == "agent" {
			return "Agent · workspace tools"
		}
	case "app-voice":
		switch value {
		case "off":
			return "Off · text only"
		case "ptt":
			return "Push-to-talk"
		case "live":
			return "Hands-free"
		}
	case "ownership":
		if value == "managed" {
			return "Local runtime · llama.cpp"
		}
		if value == "external" {
			return "Connect to a running server"
		}
	case "variant":
		if value == "nano" {
			return "Chatterbox Nano"
		}
		if value == "turbo" {
			return "Chatterbox Turbo"
		}
	case "prefer-main", "prefer-post":
		if value == "true" {
			return "Enabled"
		}
		if value == "false" {
			return "Disabled"
		}
	}
	return value
}

func Hint(section string) string {
	switch section {
	case "profiles":
		return "Profiles save your setup, including the character chosen for Chat."
	case "character":
		return "Character sets identity and dialogue. Chat only."
	case "you":
		return "Your name and persona are used by the compiled prompts."
	case "application":
		return "Mode sets capabilities; switching modes starts a session of its own."
	case "model":
		return "Choose the thinking model."
	case "generation":
		return "Response budget and context window for one turn."
	case "speech":
		return "Choose voice services."
	case "prompts":
		return "Preset adds response style to the compiled instruction."
	}
	return "Changes apply to this session; history stays intact."
}

func RemoteModelChoices(kind, endpoint, keyEnv string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := llm.DiscoverModels(ctx, kind, endpoint, os.Getenv(keyEnv))
	if err != nil {
		return nil, fmt.Errorf("%w; Esc returns to manual entry", err)
	}
	return items, nil
}

func Choices(key string) []string {
	switch key {
	case "prompt-preset":
		ids := []string{}
		for _, preset := range prompts.Presets() {
			ids = append(ids, preset.ID)
		}
		return ids
	case "prefer-main", "prefer-post":
		return []string{"true", "false"}
	case "app-mode":
		return []string{"chat", "agent"}
	case "app-voice":
		return []string{"off", "ptt", "live"}
	case "vad":
		return []string{"silero", "energy"}
	case "ownership":
		return []string{"external", "managed"}
	case "kind":
		return []string{"llama", "kobold", "openai"}
	case "variant":
		return []string{"nano", "turbo"}
	}
	return nil
}

// SuggestedBackendURL changes only the previous protocol's local default.
// OpenAI denotes a compatible API, so its suggestion stays local.

func SuggestedBackendURL(endpoint, previous, next string) string {
	ports := map[string]string{"llama": "8080", "kobold": "5001", "openai": "8080"}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return endpoint
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return endpoint
	}
	path := strings.TrimSuffix(u.Path, "/")
	if u.Port() != ports[previous] || (path != "" && !(previous == "openai" && path == "/v1")) {
		return endpoint
	}
	if ports[next] == "" {
		return endpoint
	}
	u.Host = host + ":" + ports[next]
	u.Path = ""
	if next == "openai" {
		u.Path = "/v1"
	}
	return u.String()
}
