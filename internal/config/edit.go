package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/speech"
)

// ApplySection applies editor values to a staged profile. Callers should clone the
// active profile first; validation failures can leave the staged copy partly edited.
//
// A nil Conversation or Speech is materialized rather than dereferenced. Both
// are legitimately absent from a profile — Validate permits it, and the
// bundled agent profile has neither a conversation block nor speech — so a
// section that supplies one has to be able to create it.
func ApplySection(profile *Profile, section string, values map[string]string) error {
	conversation := profile.Conversation
	if conversation == nil {
		conversation = &Conversation{UserName: DefaultUserName}
		profile.Conversation = conversation
	}
	if section == "speech" && profile.Speech == nil {
		profile.Speech = &speech.Config{}
	}
	switch section {
	case "character":
		conversation.Character = strings.TrimSpace(values["character"])
		conversation.Name = strings.TrimSpace(values["name"])
		if value, ok := values["greeting"]; ok {
			index, err := strconv.Atoi(value)
			if err != nil || index < 0 {
				return fmt.Errorf("greeting index must be a nonnegative integer")
			}
			conversation.GreetingIndex = index
		}
		if conversation.Character != "" {
			path, err := filepath.Abs(conversation.Character)
			if err != nil {
				return err
			}
			conversation.Character = path
		}
	case "you":
		conversation.UserName = strings.TrimSpace(values["user"])
		conversation.UserPersona = values["persona"]
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
			conversation.PromptPreset = value
		}
		conversation.SystemPrompt = values["system"]
		conversation.PostHistory = values["post"]
		main, err := strconv.ParseBool(values["prefer-main"])
		if err != nil {
			return fmt.Errorf("main prompt preference must be true/false")
		}
		post, err := strconv.ParseBool(values["prefer-post"])
		if err != nil {
			return fmt.Errorf("post-history preference must be true/false")
		}
		conversation.PreferCharacterPrompt = &main
		conversation.PreferCharacterPostHistory = &post
	case "speech":
		profile.Speech.Microphone = strings.TrimSpace(values["microphone"])
		variant := strings.TrimSpace(values["variant"])
		model := strings.TrimSpace(values["speech-model"])
		if variant != profile.Speech.Speaking.Variant && filepath.Base(model) == profile.Speech.Speaking.Variant {
			model = filepath.Join(filepath.Dir(model), variant)
		}
		profile.Speech.Speaking.Variant = variant
		path, err := filepath.Abs(model)
		if err != nil {
			return err
		}
		profile.Speech.Speaking.Model = path
		seconds, err := strconv.Atoi(values["seconds"])
		if err != nil {
			return fmt.Errorf("recording seconds must be an integer")
		}
		profile.Speech.MaxSeconds = seconds
		if value, ok := values["vad"]; ok {
			profile.Speech.VAD = value
		}
		if value, ok := values["mic-start"]; ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("speech onset must be milliseconds")
			}
			profile.Speech.MicStartMS = n
		}
		if value, ok := values["mic-end"]; ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("end silence must be milliseconds")
			}
			profile.Speech.MicEndMS = n
		}
		if value, ok := values["mic-threshold"]; ok {
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("threshold must be numeric")
			}
			profile.Speech.MicThreshold = n
		}
		profile.Speech.Enabled = false
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
