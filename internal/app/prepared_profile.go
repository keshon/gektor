// Package app composes and owns profile-driven services independently of any
// frontend. Frontends borrow a composed set for the length of one launch.
package app

import (
	"github.com/keshon/gektor/internal/config"
)

// PreparedProfile contains a validated launch profile and compiled identity.
type PreparedProfile struct {
	Identity  Identity
	Warnings  []string
	Profile   config.Profile
	Directory string
}

// PrepareProfile clones and validates a profile and compiles its identity.
func PrepareProfile(profile config.Profile, directory string) (PreparedProfile, error) {
	profile = profile.Clone()
	out := PreparedProfile{Profile: profile, Directory: directory}
	if err := profile.Validate(); err != nil {
		return out, err
	}
	id := identityFrom(profile)
	identity, warnings, err := CompileIdentity(IdentityOptions{
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
