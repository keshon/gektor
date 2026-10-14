// Package character reads character-card configuration; it does not implement behavior.
package character

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Card struct {
	Warnings      []string        `json:"-"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Personality   string          `json:"personality"`
	Scenario      string          `json:"scenario"`
	FirstMessage  string          `json:"first_mes"`
	Examples      string          `json:"mes_example"`
	SystemPrompt  string          `json:"system_prompt"`
	PostHistory   string          `json:"post_history_instructions"`
	CharacterBook json.RawMessage `json:"character_book"`
}

func Load(path string) (Card, error) {
	file, err := os.Open(path)
	if err != nil {
		return Card{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	prefix, _ := reader.Peek(len(pngSignature))
	var data []byte
	var warnings []string
	if bytes.Equal(prefix, pngSignature) {
		data, err = readPNGCardWarnings(reader, &warnings)
	} else {
		data, err = io.ReadAll(io.LimitReader(reader, maxCardBytes+1))
	}
	if err != nil {
		return Card{}, err
	}
	if len(data) > maxCardBytes {
		return Card{}, fmt.Errorf("character card exceeds 64 KiB")
	}
	card, err := decodeCard(data)
	card.Warnings = warnings
	if err == nil && len(card.CharacterBook) > 0 && string(card.CharacterBook) != "null" {
		var book struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(card.CharacterBook, &book); err != nil {
			return card, fmt.Errorf("invalid character lorebook: %w", err)
		}
		if len(book.Entries) > 0 {
			return card, fmt.Errorf("character lorebook activation is not supported yet")
		}
	}
	return card, err
}

func decodeCard(data []byte) (Card, error) {
	var err error
	var envelope struct {
		Spec    string          `json:"spec"`
		Version string          `json:"spec_version"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return Card{}, err
	}
	if envelope.Spec != "" {
		if !(envelope.Spec == "chara_card_v2" && envelope.Version == "2.0" || envelope.Spec == "chara_card_v3" && envelope.Version == "3.0") {
			return Card{}, fmt.Errorf("supported cards: flat JSON, chara_card_v2 version 2.0, or basic chara_card_v3 version 3.0")
		}
		data = envelope.Data
	}
	var c Card
	if err = json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 {
		return c, fmt.Errorf("character requires a name of at most 100 bytes")
	}
	return c, nil
}
func (c Card) Render(text, user, original string) string {
	return strings.NewReplacer("{{char}}", c.Name, "{{user}}", user, "{{original}}", original).Replace(text)
}
func (c Card) Instruction(user, original string) string {
	system := c.SystemPrompt
	if strings.TrimSpace(system) == "" {
		system = original
	}
	parts := []string{c.Render(system, user, original), "Character name: " + c.Name}
	for _, field := range []struct{ name, value string }{{"Description", c.Description}, {"Personality", c.Personality}, {"Scenario", c.Scenario}, {"Dialogue examples", c.Examples}, {"Opening greeting", c.FirstMessage}} {
		if strings.TrimSpace(field.value) != "" {
			parts = append(parts, field.name+": "+c.Render(field.value, user, original))
		}
	}
	return strings.Join(parts, "\n\n")
}

// Compile keeps post-history directives separate from the leading instruction.
func (c Card) Compile(user, original string, preferMain, preferPost bool, basePost ...string) (string, string) {
	copy := c
	if !preferMain {
		copy.SystemPrompt = ""
	}
	system := copy.Instruction(user, original)
	originalPost := ""
	if len(basePost) > 0 {
		originalPost = basePost[0]
	}
	post := c.Render(originalPost, user, "")
	if preferPost && strings.TrimSpace(c.PostHistory) != "" {
		post = c.Render(c.PostHistory, user, post)
	}
	return system, post
}
