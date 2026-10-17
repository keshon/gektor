package app

import (
	"github.com/keshon/tars/internal/config"
)

// ConfigureAgent composes the services one agent launch runs against.
//
// There is one shape. It used to take two: a full configuration that built a
// client, and a variant that left the client nil for a session whose runner
// already held one. The second had no distinguishing feature left, so it is
// this.
func ConfigureAgent(profile config.Profile, directory string) (Services, error) {
	profile = profile.Clone()
	out := Services{Profile: profile, Directory: directory}
	if err := profile.Validate(); err != nil {
		return out, err
	}
	id := identityFrom(profile)
	identity, warnings, err := Compile(IdentityOptions{
		Preset:        id.PromptPreset,
		Name:          id.Name,
		Persona:       id.UserPersona,
		User:          id.UserName,
		System:        id.SystemPrompt,
		Post:          id.PostHistory,
		MaxTokens:     profile.Generation.MaxTokens,
		ContextLimit:  profile.Generation.ContextLimit,
		Temperature:   profile.Generation.Temperature,
	})
	if err != nil {
		return out, err
	}
	out.Identity = identity
	out.Warnings = warnings
	return out, nil
}