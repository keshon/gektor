package character

import (
	"github.com/keshon/tars/internal/llm"
	"strings"
	"testing"
)

func TestCardMacroCompatibilityAndRecursion(t *testing.T) {
	c := Card{Name: "Ada", Description: "Friend of {{USER}}", Personality: "Warm", Scenario: "{{description}} outdoors", UserPersona: "A developer", FirstMessage: "Hello {{user}}", AlternateGreetings: []string{"Hi <USER>"}}
	input := "{{CHAR}} {{user}} <BOT> <CHAR> <USER> {user} {char} | {{scenario}} | {{persona}} | {{greeting::1}} | {{original}}"
	got, warnings := c.RenderChecked(input, "Alex", "Base {{char}}")
	want := "Ada Alex Ada Ada Alex Alex Ada | Friend of Alex outdoors | A developer | Hi Alex | Base Ada"
	if got != want || len(warnings) > 0 {
		t.Fatal(got, warnings)
	}
	c.Description = "{{scenario}}"
	_, warnings = c.RenderChecked("{{description}} {{setvar::x::y}}", "Alex", "")
	if len(warnings) != 2 || !strings.Contains(strings.Join(warnings, " "), "Recursive") || !strings.Contains(strings.Join(warnings, " "), "Unsupported") {
		t.Fatal(warnings)
	}
	if _, err := c.Greeting(2); err == nil {
		t.Fatal("invalid alternate accepted")
	}
}

func TestDialogueExamplesPreserveRolesAndMultiline(t *testing.T) {
	c := Card{Name: "Ada", Examples: "<START>\n{{user}}: Hi\n{{char}}: Hello\n*smiles*\n<START>\n{{char}}: Welcome"}
	got, err := c.ExampleMessages("Alex")
	if err != nil || len(got) != 3 || got[0].Role != llm.RoleUser || got[1].Content != "Hello\n*smiles*" || got[2].Role != llm.RoleAssistant {
		t.Fatal(got, err)
	}
	c.Examples = "Unlabelled narrative"
	if _, err := c.ExampleMessages("Alex"); err == nil {
		t.Fatal("unlabelled example accepted")
	}
}

func TestCardMacroExpansionBudget(t *testing.T) {
	c := Card{Name: "Ada", Description: strings.Repeat("x", maxCardBytes)}
	_, warnings := c.RenderChecked(strings.Repeat("{{description}}", 5), "Alex", "")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "256 KiB") {
		t.Fatal(warnings)
	}
}
