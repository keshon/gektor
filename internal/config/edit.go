package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/keshon/tars/internal/prompts"
)

// ApplySection applies editor values to a staged profile. Callers should clone the
// active profile first; validation failures can leave the staged copy partly edited.
//
// A nil Identity is materialized rather than dereferenced. It is legitimately
// absent from a profile — Validate permits it, and the bundled agent profile
// carries no identity block — so a section that supplies one has to be able to
// create it.
func ApplySection(profile *Profile, section string, values map[string]string) error {
	id := profile.Identity
	if id == nil {
		id = &Identity{UserName: DefaultUserName}
		profile.Identity = id
	}
	switch section {
	case "you":
		id.UserName = strings.TrimSpace(values["user"])
		id.UserPersona = values["persona"]
		id.Name = strings.TrimSpace(values["name"])
	case "generation":
		var err error
		profile.Generation.Temperature, err = strconv.ParseFloat(values["temperature"], 64)
		if err != nil {
			return fmt.Errorf("temperature must be a number")
		}
		profile.Generation.MaxTokens, err = strconv.Atoi(values["max"])
		if err != nil {
			return fmt.Errorf("response budget must be an integer")
		}
		profile.Generation.ContextLimit, err = strconv.Atoi(values["context"])
		if err != nil {
			return fmt.Errorf("context window must be an integer")
		}
	case "prompts":
		if value, ok := values["prompt-preset"]; ok {
			if _, err := prompts.ResponsePreset(value); err != nil {
				return err
			}
			id.PromptPreset = value
		}
		id.SystemPrompt = values["system"]
		id.PostHistory = values["post"]
	case "model":
		profile.Provider.Ownership = strings.TrimSpace(values["ownership"])
		profile.Provider.Runtime = strings.TrimSpace(values["runtime"])
		profile.Provider.ModelFile = strings.TrimSpace(values["model-file"])
		profile.Provider.ModelRoot = strings.TrimSpace(values["model-root"])
		layers, err := strconv.Atoi(values["gpu-layers"])
		if err != nil {
			return fmt.Errorf("GPU layers must be an integer")
		}
		profile.Provider.GPULayers = layers
		profile.Provider.Kind = strings.TrimSpace(values["kind"])
		profile.Provider.URL = strings.TrimSpace(values["endpoint"])
		profile.Provider.Model = strings.TrimSpace(values["model"])
		profile.Provider.APIKeyEnv = strings.TrimSpace(values["key-env"])
	}
	return nil
}