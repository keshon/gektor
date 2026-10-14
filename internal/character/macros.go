package character

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/keshon/tars/internal/llm"
)

var macroPattern = regexp.MustCompile(`(?s)\{\{(.*?)\}\}|(?i:<(?:USER|BOT|CHAR)>|\{(?:user|char)\})`)

// Render expands card macros without executing scripts or altering the card.
func (c Card) Render(text, user, original string) string {
	rendered, _ := c.RenderChecked(text, user, original)
	return rendered
}

func (c Card) Greeting(index int) (string, error) {
	if index == 0 {
		return c.FirstMessage, nil
	}
	if index < 0 || index > len(c.AlternateGreetings) {
		return "", fmt.Errorf("greeting index %d is out of range (0 through %d)", index, len(c.AlternateGreetings))
	}
	return c.AlternateGreetings[index-1], nil
}

// RenderChecked bounds nested field expansion and reports unsupported macros.
func (c Card) RenderChecked(text, user, original string) (string, []string) {
	warnings := []string{}
	seen := map[string]bool{}
	remaining := 4 * maxCardBytes
	warn := func(message string) {
		if !seen[message] {
			warnings = append(warnings, message)
			seen[message] = true
		}
	}
	values := map[string]string{
		"user": user, "char": c.Name, "original": original, "persona": c.UserPersona,
		"description": c.Description, "chardescription": c.Description,
		"personality": c.Personality, "charpersonality": c.Personality,
		"scenario": c.Scenario, "charscenario": c.Scenario,
		"mesexamples": c.Examples, "mesexamplesraw": c.Examples,
		"charprompt": c.SystemPrompt, "charinstruction": c.PostHistory,
		"creatornotes": c.CreatorNotes, "charcreatornotes": c.CreatorNotes,
		"charversion": c.CharacterVersion,
	}
	var expand func(string, map[string]bool, int) string
	expand = func(input string, stack map[string]bool, depth int) string {
		return macroPattern.ReplaceAllStringFunc(input, func(raw string) string {
			key := strings.ToLower(strings.TrimSpace(raw))
			switch key {
			case "<user>", "{user}":
				key = "user"
			case "<bot>", "<char>", "{char}":
				key = "char"
			default:
				key = strings.TrimSpace(key[2 : len(key)-2])
			}
			value, ok := values[key]
			if key == "greeting" || key == "charfirstmessage" || strings.HasPrefix(key, "greeting::") || strings.HasPrefix(key, "charfirstmessage::") {
				index := 0
				if _, suffix, has := strings.Cut(key, "::"); has {
					var err error
					index, err = strconv.Atoi(strings.TrimSpace(suffix))
					if err != nil {
						warn("Invalid greeting macro: " + raw)
						return raw
					}
				}
				var err error
				value, err = c.Greeting(index)
				if err != nil {
					warn(err.Error())
					return raw
				}
				ok = true
			}
			if !ok {
				warn("Unsupported character macro: " + raw)
				return raw
			}
			if stack[key] || depth >= 8 {
				warn("Recursive character macro: " + raw)
				return raw
			}
			if len(value) > remaining {
				warn("Character macro expansion exceeds 256 KiB")
				return raw
			}
			remaining -= len(value)
			// Participant names are literal values, not macro templates.
			if key == "user" || key == "char" {
				return value
			}
			stack[key] = true
			result := expand(value, stack, depth+1)
			delete(stack, key)
			return result
		})
	}
	return expand(text, map[string]bool{}, 0), warnings
}

// ExampleMessages converts card dialogue examples into ordered speaker messages.
// Unparseable examples are left to the caller's explicit fallback with a warning.
func (c Card) ExampleMessages(user string) ([]llm.Message, error) {
	text := c.Render(c.Examples, user, "")
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	messages := []llm.Message{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "<START>") {
			continue
		}
		role := llm.Role("")
		content := ""
		for _, participant := range []struct {
			name string
			role llm.Role
		}{{user, llm.RoleUser}, {c.Name, llm.RoleAssistant}} {
			prefix := participant.name + ":"
			if strings.HasPrefix(line, prefix) {
				role = participant.role
				content = strings.TrimSpace(strings.TrimPrefix(line, prefix))
				break
			}
		}
		if role != "" {
			messages = append(messages, llm.Message{Role: role, Content: content})
		} else if len(messages) > 0 {
			messages[len(messages)-1].Content += "\n" + line
		} else if strings.TrimSpace(line) != "" {
			return nil, fmt.Errorf("dialogue examples do not start with a participant name")
		}
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("dialogue examples contain no speaker messages")
	}
	for _, message := range messages {
		if strings.TrimSpace(message.Content) == "" {
			return nil, fmt.Errorf("dialogue examples contain an empty speaker message")
		}
	}
	return messages, nil
}
