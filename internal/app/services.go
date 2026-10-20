// Package app composes and owns profile-driven services independently of any
// frontend. Frontends borrow a composed set for the length of one launch.
package app

import (
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
)

// Services is what one launch is composed from: a thinking client, the
// identity its runs speak with, and the profile it came from.
//
// There is no reconfigure callback. A launch composes once; changing settings
// ends the launch and composes a new one, so there is nothing for a callback
// to be called on behalf of.
type Services struct {
	Identity  Identity
	Warnings  []string
	Profile   config.Profile
	Client    llm.Client
	Directory string
}

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
		Preset:       id.PromptPreset,
		Name:         id.Name,
		Persona:      id.UserPersona,
		User:         id.UserName,
		System:       id.SystemPrompt,
		Post:         id.PostHistory,
		MaxTokens:    profile.Generation.MaxTokens,
		ContextLimit: profile.Generation.ContextLimit,
		Temperature:  profile.Generation.Temperature,
	})
	if err != nil {
		return out, err
	}
	out.Identity = identity
	out.Warnings = warnings
	return out, nil
}
