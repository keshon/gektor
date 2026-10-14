package main

import (
	"context"
	"fmt"
	"github.com/keshon/tars/internal/backend"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/speech"
	speechservice "github.com/keshon/tars/internal/speech/service"
	"github.com/keshon/tars/internal/tui"
	"path/filepath"
	"strings"
)

type chatOptions struct {
	Preset                        string
	Name, Persona                 string
	GreetingIndex                 int
	Character, User, System, Post string
	PreferMain, PreferPost        bool
	MaxTokens, ContextLimit       int
	Temperature                   float64
}

func compileChatSettings(o chatOptions) (chat.Config, []string, error) {
	preset, err := prompts.ResponsePreset(o.Preset)
	if err != nil {
		return chat.Config{}, nil, err
	}
	cfg := chat.Config{System: o.System, PostHistory: o.Post, MaxTokens: o.MaxTokens, ContextLimit: o.ContextLimit, Temperature: o.Temperature}
	if strings.TrimSpace(o.User) == "" {
		return cfg, nil, fmt.Errorf("chat: user name cannot be blank")
	}

	card := character.Card{Name: o.Name}
	if card.Name == "" {
		card.Name = "Assistant"
	}
	if o.Character != "" {
		card, err = character.Load(o.Character)
		if err != nil {
			return cfg, nil, fmt.Errorf("character: %w", err)
		}
	}
	card.UserPersona = o.Persona
	greeting, err := card.Greeting(o.GreetingIndex)
	if err != nil {
		return cfg, nil, err
	}
	examples, exampleErr := card.ExampleMessages(o.User)
	compileCard := card
	if exampleErr == nil {
		compileCard.ExamplesAsMessages = true
	}
	compileCard.GreetingIndex = o.GreetingIndex
	cfg.System, cfg.PostHistory = compileCard.Compile(o.User, o.System, o.PreferMain, o.PreferPost, o.Post)
	if preset.Instruction != "" {
		cfg.System += "\n\nResponse style (" + preset.Name + "):\n" + preset.Instruction
	}
	if o.Persona != "" {
		cfg.System += "\n\nUser persona: " + card.Render(o.Persona, o.User, "")
	}
	cfg.Greeting = card.Render(greeting, o.User, "")
	cfg.CharacterName = card.Name
	cfg.Examples = examples
	if exampleErr != nil {
		card.Warnings = append(card.Warnings, "Dialogue examples retained as prompt text: "+exampleErr.Error())
	}
	for _, field := range []string{card.Description, card.Personality, card.Scenario, card.Examples, o.Persona, greeting, o.System, o.Post, card.SystemPrompt, card.PostHistory} {
		_, warnings := card.RenderChecked(field, o.User, o.System)
		card.Warnings = append(card.Warnings, warnings...)
	}

	unique := []string{}
	seen := map[string]bool{}
	for _, warning := range card.Warnings {
		if !seen[warning] {
			seen[warning] = true
			unique = append(unique, warning)
		}
	}
	return cfg, unique, nil
}

func configureChat(profile config.Profile, key, directory string) (tui.ChatConfig, error) {
	var out tui.ChatConfig
	if err := profile.Validate(); err != nil {
		return out, err
	}
	c := profile.Conversation
	if c == nil {
		return out, fmt.Errorf("conversation settings required")
	}
	preferMain, preferPost := true, true
	if c.PreferCharacterPrompt != nil {
		preferMain = *c.PreferCharacterPrompt
	}
	if c.PreferCharacterPostHistory != nil {
		preferPost = *c.PreferCharacterPostHistory
	}
	settings, warnings, err := compileChatSettings(chatOptions{Preset: c.PromptPreset, Name: c.Name, Persona: c.UserPersona, GreetingIndex: c.GreetingIndex, Character: c.Character, User: c.UserName, System: c.SystemPrompt, Post: c.PostHistory, PreferMain: preferMain, PreferPost: preferPost, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		return out, err
	}
	client, err := llm.ClientFor(profile.Provider.Kind, profile.Provider.URL, profile.Provider.Model)
	if err != nil {
		return out, err
	}
	client.NoGrammar = true
	resolveAPIKey(key, profile.Provider.APIKeyEnv, client)
	client.ExtraHeaders = extraHeadersFromEnv()
	out = tui.ChatConfig{Warnings: warnings, Client: client, Settings: settings, Directory: directory, Profile: profile}
	if profile.Provider.Ownership == "managed" {
		managed, err := backend.NewLlama(profile.Provider, profile.Generation.ContextLimit, client, filepath.Join(directory, "..", "thinking"))
		if err != nil {
			return out, err
		}
		out.Client = managed
	}
	out.Stream = profile.Provider.Kind != "kobold"
	if profile.Speech != nil {
		cfg := *profile.Speech
		out.NewVoice = func(ctx context.Context, directory string) (speech.Service, error) {
			return speechservice.New(ctx, cfg, directory)
		}
	}
	out.Configure = func(next config.Profile) (tui.ChatConfig, error) { return configureChat(next, key, directory) }
	return out, nil
}

// Agent retains its operational system prompt and adds the compiled identity.
func agentIdentity(settings chat.Config) string {
	text := settings.System + "\n" + settings.PostHistory
	for _, example := range settings.Examples {
		text += "\n\nDialogue example (" + string(example.Role) + "): " + example.Content
	}
	return text
}
