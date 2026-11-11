package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
)

// Clone returns an independently editable profile, including the optional
// identity block.
func (p Profile) Clone() Profile {
	if p.Identity != nil {
		identity := *p.Identity
		p.Identity = &identity
	}

	return p
}

// Provider identifies an external backend. APIKeyEnv names an environment variable, not a secret.
type Provider struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// Generation stores request limits and sampling controls. Zero temperature uses the transport default.
type Generation struct {
	ReasoningEffort  string  `json:"reasoning_effort,omitempty"`
	ReasoningSummary bool    `json:"reasoning_summary,omitempty"`
	MaxTokens        int     `json:"max_tokens"`
	ContextLimit     int     `json:"context_limit"`
	Temperature      float64 `json:"temperature,omitempty"`
}

// DefaultProfile is the first-run setup; no file needs to be installed.
func DefaultProfile() Profile {
	descriptor := providers.Default()

	return Profile{
		Version: 1,
		Provider: Provider{
			Kind:  descriptor.ID,
			URL:   descriptor.DefaultURL,
			Model: "local",
		},
		Generation: Generation{
			MaxTokens:    8192,
			ContextLimit: 8192,
			Temperature:  0.7,
		},
		Identity: &Identity{
			UserName:     DefaultUserName,
			SystemPrompt: DefaultSystemPrompt,
			PromptPreset: "coding",
		},
	}
}

// DefaultSystemPrompt is the base instruction when none is configured.
const DefaultSystemPrompt = "You are a helpful assistant."

// DefaultTemperature is what a backend uses when a profile sets none. Zero
// means "unset", not "deterministic", so anything that shows this number has
// to say so rather than rendering a bare 0.
const DefaultTemperature = 0.4

// ParseTemperature accepts a number, or the word "default" to hand the
// choice back to the backend.
func ParseTemperature(s string) (float64, error) {
	value := strings.TrimSpace(s)
	if strings.EqualFold(value, "default") {
		return 0, nil
	}

	return strconv.ParseFloat(value, 64)
}

// DefaultUserName is the placeholder for an unset user name.
const DefaultUserName = "User"

// Identity is the persona a profile speaks with: who the assistant is, who
// it is talking to, and how it should answer.
type Identity struct {
	PromptPreset string `json:"prompt_preset,omitempty"`
	Name         string `json:"name,omitempty"`
	UserPersona  string `json:"user_persona,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	PostHistory  string `json:"post_history,omitempty"`
}

// Validate checks the profile without changing it or contacting the provider.
func (p Profile) Validate() error {
	if p.Identity != nil {
		_, err := prompts.ResponsePreset(p.Identity.PromptPreset)
		if err != nil {
			return err
		}
	}

	temperature := p.Generation.Temperature
	invalidTemperature := math.IsNaN(temperature) || math.IsInf(temperature, 0) || temperature < 0 || temperature > 2
	if invalidTemperature {
		return fmt.Errorf("profile: temperature must be between 0 and 2; zero uses transport default")
	}

	if p.Version != 1 {
		return fmt.Errorf("profile: unsupported version %d", p.Version)
	}

	descriptor, err := providers.Lookup(p.Provider.Kind)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}

	reasoningEffort := providers.NormalizeReasoningEffort(p.Generation.ReasoningEffort)
	usesReasoning := reasoningEffort != "" || p.Generation.ReasoningSummary
	if usesReasoning && !descriptor.Reasoning {
		return fmt.Errorf("profile: %s does not support reasoning controls", descriptor.Label)
	}

	err = descriptor.ValidateEndpoint(p.Provider.URL)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}

	if p.Identity != nil && len(p.Identity.Name) > 100 {
		return fmt.Errorf("profile: assistant name must be at most 100 characters")
	}

	if p.Provider.Model == "" {
		return fmt.Errorf("profile: model is required")
	}

	// MaxTokens is a ceiling; the agent reduces it to the room left in the
	// server's window on each request, including after a smaller model loads.
	if p.Generation.MaxTokens <= 0 || p.Generation.ContextLimit <= 0 {
		return fmt.Errorf("profile: context_limit and max_tokens must be positive")
	}

	return nil
}
