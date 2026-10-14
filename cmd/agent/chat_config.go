package main

import (
	"context"
	"fmt"
	"github.com/keshon/tars/internal/backend"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
	speechservice "github.com/keshon/tars/internal/speech/service"
	"github.com/keshon/tars/internal/tui"
	"path/filepath"
	"strings"
)

type chatOptions struct {
	Character, User, System, Post string
	PreferMain, PreferPost        bool
	MaxTokens, ContextLimit       int
	Temperature                   float64
}

func compileChatSettings(o chatOptions) (chat.Config, []string, error) {
	cfg := chat.Config{System: o.System, PostHistory: o.Post, MaxTokens: o.MaxTokens, ContextLimit: o.ContextLimit, Temperature: o.Temperature}
	if strings.TrimSpace(o.User) == "" {
		return cfg, nil, fmt.Errorf("chat: user name cannot be blank")
	}
	if o.Character == "" {
		return cfg, nil, nil
	}
	card, err := character.Load(o.Character)
	if err != nil {
		return cfg, nil, fmt.Errorf("character: %w", err)
	}
	cfg.System, cfg.PostHistory = card.Compile(o.User, o.System, o.PreferMain, o.PreferPost, o.Post)
	cfg.Greeting = card.Render(card.FirstMessage, o.User, "")
	cfg.CharacterName = card.Name
	return cfg, card.Warnings, nil
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
	settings, warnings, err := compileChatSettings(chatOptions{Character: c.Character, User: c.UserName, System: c.SystemPrompt, Post: c.PostHistory, PreferMain: preferMain, PreferPost: preferPost, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
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
