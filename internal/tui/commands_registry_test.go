package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Enter and Tab used to be the same branch in the popup, so every command
// was handed to the composer and needed a second Enter. That is right for
// "/max-steps 20" and wrong for "/help": a command that takes nothing is
// complete the moment it is named.

func TestEnterRunsACommandThatTakesNoArgument(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/hel")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.title != "Help" {
		t.Fatalf("Enter did not run /help; dialog = %+v", m.dialog)
	}
	if m.input.Value() != "" {
		t.Fatalf("composer kept %q after the command ran", m.input.Value())
	}
}

func TestEnterHandsACommandThatTakesAnArgumentToTheComposer(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/max-steps")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// The "N" in help is a placeholder, not text to type.
	if m.input.Value() != "/max-steps " {
		t.Fatalf("Enter did not complete the command: %q", m.input.Value())
	}
	if m.suggestions.Pending() {
		t.Fatal("the argument is still missing, so the popup should have closed")
	}
}

// Tab always inserts, even for a command with no argument: it is the
// "show me the text, I will press Enter when I am ready" key.
func TestTabAlwaysInserts(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/help")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.dialog != nil {
		t.Fatal("Tab ran the command")
	}
	if m.input.Value() != "/help " {
		t.Fatalf("Tab = %q", m.input.Value())
	}
}

// The registry is the only place that decides what a command takes, and
// completion, help and execution all read it.
func TestCommandRegistryAgreesWithItself(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands() {
		if !strings.HasPrefix(c.name, "/") {
			t.Fatalf("%q is not a slash command", c.name)
		}
		if seen[c.name] {
			t.Fatalf("%s is listed twice", c.name)
		}
		seen[c.name] = true
		if c.arg == "" && len(c.choices) > 0 {
			t.Fatalf("%s offers choices but takes no argument", c.name)
		}
		if lookupCommand(c.name) == nil {
			t.Fatalf("%s is not resolvable", c.name)
		}
	}
	for _, row := range commandReference() {
		name, _, _ := strings.Cut(row[0], " ")
		if !seen[name] {
			t.Fatalf("help lists %q, which no command declares", name)
		}
	}
}

// An enumerated argument completes from the registry rather than from a
// special case per command, so /nudges offers its levels the way /mode
// offers its modes.
func TestEnumeratedArgumentCompletesFromTheRegistry(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/nudges ")
	m.syncSuggestions()
	var values []string
	for _, item := range m.suggestions.Items {
		values = append(values, item.Value)
	}
	want := []string{"/nudges full", "/nudges recovery", "/nudges off"}
	if strings.Join(values, ",") != strings.Join(want, ",") {
		t.Fatalf("nudges choices = %v, want %v", values, want)
	}
	m.input.SetValue("/nudges r")
	m.syncSuggestions()
	if len(m.suggestions.Items) != 1 || m.suggestions.Items[0].Value != "/nudges recovery" {
		t.Fatalf("partial argument did not narrow: %+v", m.suggestions.Items)
	}
}

// A free-form argument is not a completion token: @paths inside it are
// still paths.
func TestFreeFormArgumentStillCompletesPaths(t *testing.T) {
	m := completionModel(t)
	writeReference(t, m, "notes.txt", []byte("hello"))
	m.input.SetValue("/new review @no")
	m.syncSuggestions()
	if len(m.suggestions.Items) == 0 || !strings.HasPrefix(m.suggestions.Items[0].Value, "notes.txt") {
		t.Fatalf("@path inside /new stopped completing: %+v", m.suggestions.Items)
	}
}
