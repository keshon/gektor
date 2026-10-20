package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/keshon/tars/internal/prompts"
)

// Sections are the editor's pages. They are named here rather than derived
// from the field keys, so a section this function does not recognise is a
// typo to report rather than a silent no-op that still mutated the profile.
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
//
// An unknown section is an error and nothing is written. It used to return
// success after having already materialised an Identity block, so a typo'd
// section name was accepted and the profile came back changed.
func ApplySection(profile *Profile, section string, values map[string]string) error {
	// Identity is legitimately absent from a profile - Validate permits it,
	// and the bundled agent profile carries no identity block - so a section
	// that supplies one has to be able to create it. Only those sections
	// touch it: creating one while changing the model wrote a block nobody
	// asked for, which then persisted into the profile file.
	identity := func() *Identity {
		if profile.Identity == nil {
			profile.Identity = &Identity{UserName: DefaultUserName}
		}
		return profile.Identity
	}

	switch section {
	case SectionYou:
		id := identity()
		id.UserName = strings.TrimSpace(values["user"])
		id.UserPersona = values["persona"]
	case SectionGeneration:
		var err error
		profile.Generation.Temperature, err = ParseTemperature(values["temperature"])
		if err != nil {
			return fmt.Errorf("temperature must be a number, or \"default\"")
		}
		profile.Generation.MaxTokens, err = strconv.Atoi(values["max"])
		if err != nil {
			return fmt.Errorf("response budget must be an integer")
		}
		if value, ok := values["context"]; ok {
			profile.Generation.ContextLimit, err = strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("context window must be an integer")
			}
		}
	case SectionPrompts:
		id := identity()
		if value, ok := values["prompt-preset"]; ok {
			if _, err := prompts.ResponsePreset(value); err != nil {
				return err
			}
			id.PromptPreset = value
		}
		id.SystemPrompt = values["system"]
		id.PostHistory = values["post"]
	case SectionModel:
		profile.Provider.Kind = strings.TrimSpace(values["kind"])
		profile.Provider.URL = strings.TrimSpace(values["endpoint"])
		profile.Provider.Model = strings.TrimSpace(values["model"])
		profile.Provider.APIKeyEnv = strings.TrimSpace(values["key-env"])
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
	return nil
}
