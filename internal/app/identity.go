package app

import (
	"fmt"
	"strings"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/prompts"
)

// Identity is the persona a profile contributes to a run: the instruction
// text, the response style the preset adds, and the generation budget.
//
// It is deliberately not a conversation. An agent run starts from a task,
// not from a transcript, so there is no greeting and no dialogue examples
// to carry here -- both used to come from a character card, and a card's
// whole purpose was to make a plain exchange feel like a scene.
type Identity struct {
	Name         string
	System       string
	PostHistory  string
	Temperature  float64
	MaxTokens    int
	ContextLimit int
}

// IdentityOptions are the profile fields Identity is compiled from.
type IdentityOptions struct {
	Preset                        string
	Name, Persona                 string
	User, System, Post            string
	MaxTokens, ContextLimit       int
	Temperature                   float64
}

// Compile resolves a preset against the profile's identity fields.
//
// The assistant name and the user's persona are not separate instructions;
// they are interpolated into the preset text, so a profile that sets one
// without the other produces a prompt that reads half-finished rather than
// one that quietly drops half of itself.
func Compile(o IdentityOptions) (Identity, []string, error) {
	preset, err := prompts.ResponsePreset(o.Preset)
	if err != nil {
		return Identity{}, nil, err
	}
	id := Identity{System: o.System, PostHistory: o.Post, Temperature: o.Temperature, MaxTokens: o.MaxTokens, ContextLimit: o.ContextLimit}
	if strings.TrimSpace(o.User) == "" {
		return id, nil, fmt.Errorf("user name cannot be blank")
	}
	name := o.Name
	if name == "" {
		name = "Assistant"
	}
	id.Name = name
	if preset.Instruction != "" {
		id.System += "\n\nResponse style (" + preset.Name + "):\n" + renderPlaceholders(preset.Instruction, name, o.User, o.Persona)
	}
	if o.Persona != "" {
		id.System += "\n\nUser persona: " + o.Persona
	}
	warnings := []string{}
	if strings.Contains(id.System, "{{char}}") {
		warnings = append(warnings, "system prompt kept a {{char}} macro with no character to fill it")
	}
	if strings.Contains(id.System, "{{user}}") || strings.Contains(id.PostHistory, "{{user}}") {
		warnings = append(warnings, "prompt kept a {{user}} macro; macros are not expanded in agent identity")
	}
	return id, warnings, nil
}

// renderPlaceholders fills the two macros a preset may use. Names are
// literal: a preset is not a template that gets to expand further.
func renderPlaceholders(text, name, user, persona string) string {
	r := strings.NewReplacer(
		"{{char}}", name,
		"{{user}}", user,
		"{{persona}}", persona,
		"{char}", name,
		"{user}", user,
	)
	return r.Replace(text)
}

// IdentityPrompt is the text a profile contributes to an agent's system
// prompt. The agent's own Plan/Act instructions are separate and are not
// part of this.
func IdentityPrompt(id Identity) string {
	text := id.System
	if id.PostHistory != "" {
		text += "\n\n" + id.PostHistory
	}
	return text
}

// identityFrom reads the profile's identity block, substituting defaults
// for a profile that carries none. The bundled agent profile has no
// identity block at all, so this is the ordinary path.
func identityFrom(p config.Profile) config.Identity {
	if p.Identity != nil {
		return *p.Identity
	}
	return config.Identity{UserName: config.DefaultUserName, SystemPrompt: config.DefaultSystemPrompt}
}