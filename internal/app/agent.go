package app

import (
	"context"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
	speechservice "github.com/keshon/tars/internal/speech/service"
)

func ConfigureAgent(profile config.Profile, directory string) (Services, error) {
	profile = profile.Clone()
	out := Services{Profile: profile, Directory: directory}
	if err := profile.Validate(); err != nil {
		return out, err
	}
	c := profile.Conversation
	if c == nil {
		c = &config.Conversation{UserName: "User", SystemPrompt: "You are a helpful conversational assistant."}
	}
	main, post := true, true
	if c.PreferCharacterPrompt != nil {
		main = *c.PreferCharacterPrompt
	}
	if c.PreferCharacterPostHistory != nil {
		post = *c.PreferCharacterPostHistory
	}
	var err error
	out.Settings, out.Warnings, err = CompileChatSettings(ChatOptions{Preset: c.PromptPreset, Name: c.Name, Persona: c.UserPersona, GreetingIndex: c.GreetingIndex, Character: c.Character, User: c.UserName, System: c.SystemPrompt, Post: c.PostHistory, PreferMain: main, PreferPost: post, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		return out, err
	}
	if profile.Speech != nil {
		cfg := *profile.Speech
		out.NewVoice = func(ctx context.Context, dir string) (speech.Service, error) { return speechservice.New(ctx, cfg, dir) }
	}
	out.Configure = func(next config.Profile) (Services, error) { return ConfigureAgent(next, directory) }
	return out, nil
}
