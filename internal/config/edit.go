package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
)

// Editor section identifiers are independent of persisted JSON field names.
const (
	SectionYou        = "you"
	SectionGeneration = "generation"
	SectionPrompts    = "prompts"
	SectionModel      = "model"
)

// Sections lists the pages the profile editor offers, in menu order.
func Sections() []string {
	return []string{SectionModel, SectionGeneration, SectionPrompts, SectionYou}
}

// ApplySection applies editor values to a staged profile. Callers should clone the
// active profile first; validation failures can leave the staged copy partly edited.
// An unknown section leaves the profile unchanged.
func ApplySection(profile *Profile, section string, values map[string]string) error {
	switch section {
	case SectionYou:
		identity := editableIdentity(profile)
		identity.UserName = strings.TrimSpace(values["user"])
		identity.UserPersona = values["persona"]
	case SectionGeneration:
		return applyGeneration(&profile.Generation, values)
	case SectionPrompts:
		identity := editableIdentity(profile)
		if value, ok := values["prompt-preset"]; ok {
			_, err := prompts.ResponsePreset(value)
			if err != nil {
				return err
			}
			identity.PromptPreset = value
		}
		identity.SystemPrompt = values["system"]
		identity.PostHistory = values["post"]
	case SectionModel:
		profile.Provider.Kind = strings.TrimSpace(values["kind"])
		profile.Provider.URL = strings.TrimSpace(values["endpoint"])
		profile.Provider.Model = strings.TrimSpace(values["model"])
		profile.Provider.APIKeyEnv = strings.TrimSpace(values["key-env"])
		descriptor, err := providers.Lookup(profile.Provider.Kind)
		// Unknown providers remain staged for Profile.Validate to reject.
		if err == nil && !descriptor.Reasoning {
			profile.Generation.ReasoningEffort = ""
			profile.Generation.ReasoningSummary = false
		}
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}

	return nil
}

// editableIdentity creates a block only for sections that actually edit identity.
func editableIdentity(profile *Profile) *Identity {
	if profile.Identity == nil {
		profile.Identity = &Identity{UserName: DefaultUserName}
	}

	return profile.Identity
}

func applyGeneration(generation *Generation, values map[string]string) error {
	if value, ok := values["reasoning-effort"]; ok {
		generation.ReasoningEffort = providers.NormalizeReasoningEffort(value)
	}

	if value, ok := values["reasoning-summary"]; ok {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("reasoning summary must be true or false")
		}
		generation.ReasoningSummary = enabled
	}

	var err error
	generation.Temperature, err = ParseTemperature(values["temperature"])
	if err != nil {
		return fmt.Errorf("temperature must be a number, or \"default\"")
	}

	generation.MaxTokens, err = strconv.Atoi(values["max"])
	if err != nil {
		return fmt.Errorf("response budget must be an integer")
	}

	if value, ok := values["context"]; ok {
		generation.ContextLimit, err = strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("context window must be an integer")
		}
	}

	return nil
}
