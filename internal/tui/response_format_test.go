package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// [RESPONSE] labels the assistant's own row. Without it the reader has to
// guess which row is the reply when a turn mixes thinking, tool output and
// prose.
func TestResponseBlockCarriesItsLabel(t *testing.T) {
	st := defaultStyles()
	if got := ansi.Strip(renderBlock(answerBlock("hello"), st, tagged(), 80)); !strings.Contains(got, "[RESPONSE]") {
		t.Fatalf("agent response label lost: %q", got)
	}
	m := sizeModel(t, testModel())
	m.transcript.blocks = []block{answerBlock("hi")}
	m.refreshContent()
	if got := ansi.Strip(m.transcript.Viewport.View()); !strings.Contains(got, "[RESPONSE]") {
		t.Fatalf("rendered block lost its label: %q", got)
	}
}

func TestAgentIdentityAndLongNameLabel(t *testing.T) {
	m := sizeModel(t, testModel())
	m.identityName = "Ada"
	m.transcript.blocks = []block{answerBlock("Reply"), {role: roleThink, text: "Thinking"}, {role: roleTool, text: "read_file"}}
	m.refreshContent()
	// One name row opens the run; thinking and the tool card follow under
	// it with label rows only.
	if got := ansi.Strip(m.transcript.Viewport.View()); strings.Contains(got, "GEKTOR") || strings.Count(got, "ADA") != 1 {
		t.Fatal(got)
	}
	row := speakerRow("AN EXTREMELY LONG ASSISTANT NAME", roleAnswer, defaultStyles(), 24)
	if n := ansi.StringWidth(row); n > 24 {
		t.Fatalf("overflow: %q (%d cols)", row, n)
	}
}

// A speaker run opens with one name row. Repeating it per block spent a
// row on every [THINKING]/[RESPONSE]/[TOOL CALL] restating the row above.
func TestSpeakerRowOpensRunOnce(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 40)
	m.identityName = "Ada"
	m.transcript.blocks = []block{
		answerBlock("one"), {role: roleThink, text: "two"}, {role: roleTool, text: "three"},
		userBlock("four"), answerBlock("five"),
	}
	m.refreshContent()
	view := ansi.Strip(m.transcript.Viewport.View())
	if n := strings.Count(view, "ADA"); n != 2 {
		t.Fatalf("expected two ADA rows (one per run), got %d:\n%s", n, view)
	}
	if n := strings.Count(view, "YOU"); n != 1 {
		t.Fatalf("expected one YOU row, got %d:\n%s", n, view)
	}
}

// Every role resolves to a distinct uppercase tag. A duplicate would make
// two kinds of block indistinguishable at a glance.
func TestEveryRoleHasItsOwnLabel(t *testing.T) {
	allRoles := []role{roleAnswer, roleUser, roleThink, roleTool, roleResult,
		roleGate, roleMarker, roleError, roleWarning}
	seen := map[string]role{}
	for _, r := range allRoles {
		label := roleLabel(r)
		if label == "" || label != strings.ToUpper(label) {
			t.Fatalf("role %v label %q is not an uppercase tag", r, label)
		}
		if prev, dup := seen[label]; dup {
			t.Fatalf("roles %v and %v share label %q", prev, r, label)
		}
		seen[label] = r
	}
	for _, want := range []struct {
		r role
		s string
	}{
		{roleUser, "INPUT"}, {roleAnswer, "RESPONSE"}, {roleThink, "THINKING"},
		{roleTool, "TOOL CALL"}, {roleResult, "TOOL RESULT"}, {roleMarker, "HARNESS"},
		{roleError, "ERROR"}, {roleWarning, "WARNING"}, {roleGate, "QUESTION"},
	} {
		if got := roleLabel(want.r); got != want.s {
			t.Fatalf("role %v label = %q, want %q", want.r, got, want.s)
		}
	}
}

// Machine chrome is SYSTEM regardless of which subsystem emitted it. A
// reader asking "who said that" should not have to tell four shades of
// application apart.
func TestMachineRolesShareSystemSpeaker(t *testing.T) {
	props := transcriptProps{speaker: "Ada", user: "Alex"}
	for _, r := range []role{roleMarker, roleGate, roleMarker, roleError, roleWarning} {
		if got := speakerLabel(block{role: r}, props); got != systemSpeaker {
			t.Fatalf("role %v speaker = %q, want %q", r, got, systemSpeaker)
		}
	}
	if got := speakerLabel(block{role: roleUser}, props); got != "ALEX" {
		t.Fatalf("user speaker = %q", got)
	}
	for _, r := range []role{roleAnswer, roleThink, roleTool, roleResult} {
		if got := speakerLabel(block{role: r}, props); got != "ADA" {
			t.Fatalf("role %v speaker = %q, want ADA", r, got)
		}
	}
}

func TestEmphasisUsesThemeSecondaryGrey(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		setTheme(theme)
		got := renderBlock(answerBlock("Normal *gesture* normal"), defaultStyles(), tagged(), 80)
		expected := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorSecondaryText))).Italic(true).Render("gesture")
		if !strings.Contains(got, expected) {
			t.Fatal(theme, got)
		}
	}
}
