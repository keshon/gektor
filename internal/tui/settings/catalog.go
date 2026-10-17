package settings

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

func PickerFiles(key string) bool {
	return key == "load-profile" || key == "app-workspace"
}

func PickerControl(key string) bool { return PickerFiles(key) || key == "model" }

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

// Async discovery captures the configured recorder but never captures audio.

func ChoiceLabel(key, value string) string {
	switch key {
	case "prompt-preset":
		preset, err := prompts.ResponsePreset(value)
		if err == nil {
			return preset.Name
		}
	case "theme":
		if value == "light" {
			return "Light"
		}
		return "Dark"
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
	case "kind":
		return []string{"llama", "kobold", "openai"}
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
