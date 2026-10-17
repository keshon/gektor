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
		c = &config.Conversation{UserName: config.DefaultUserName, SystemPrompt: config.DefaultSystemPrompt}
	}
	// No character, and no greeting index or card-prompt preference.
	// Cards belong to Chat. A card is written for a conversation: its
	// opening greeting has no place in a run that starts from a task, its
	// dialogue examples crowd out tool-call examples, and its
	// post_history_instructions ("respond directly to the latest message,
	// do not invent actions or capabilities") states the opposite of what
	// an agent loop is for. What survives is the profile's own identity
	// and response style: name, user, persona, base prompt, post-history,
	// and the preset.
	//
	// An Agent's instructions are otherwise its own prompts — see
	// roles.Interactive — and nothing here competes with them.
	var err error
	out.Settings, out.Warnings, err = CompileChatSettings(ChatOptions{Preset: c.PromptPreset, Name: c.Name, Persona: c.UserPersona, User: c.UserName, System: c.SystemPrompt, Post: c.PostHistory, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
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
