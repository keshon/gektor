package prompts

import (
	"runtime"
	"strings"
	"testing"
)

func TestAllPromptsLoadNonEmpty(t *testing.T) {
	all := map[string]string{
		"System":               System,
		"Evaluation":           Evaluation,
		"RoleAddendum":         RoleAddendum,
		"Resume":               Resume,
		"LeakDetected":         LeakDetected,
		"BudgetContinue":       BudgetContinue,
		"WriteHint":            WriteHint,
		"SubagentScope":        SubagentScope,
		"OverflowRecovered":    OverflowRecovered,
		"RepeatingSteps":       RepeatingSteps,
		"Truncated":            Truncated,
		"CompactNotice":        CompactNotice,
		"PlanMode":             PlanMode,
		"ActMode":              ActMode,
		"preset-conversation":  read("preset-conversation.txt"),
		"preset-concise":       read("preset-concise.txt"),
		"preset-coding":        read("preset-coding.txt"),
		"preset-investigation": read("preset-investigation.txt"),
		"preset-explanation":   read("preset-explanation.txt"),
	}
	for name, val := range all {
		if val == "" {
			t.Errorf("%s loaded empty", name)
		}
		if strings.HasSuffix(val, "\n") || strings.HasSuffix(val, "\r") {
			t.Errorf("%s has a trailing newline, want trimmed", name)
		}
	}
}

func TestWithRole_EmptyReturnsSystemUnchanged(t *testing.T) {
	if got := WithRole(""); got != System {
		t.Fatalf("WithRole(\"\") changed the prompt, want it unchanged")
	}
}

func TestWithRole_NonEmptyAppendsFormattedAddendum(t *testing.T) {
	got := WithRole("a strict reviewer")
	if !strings.HasPrefix(got, System) {
		t.Fatal("WithRole result must start with the base System prompt")
	}
	if !strings.Contains(got, "a strict reviewer") {
		t.Fatalf("role text not found in result: %q", got)
	}
	if strings.Contains(got, "%s") {
		t.Fatalf("role addendum format string was not substituted: %q", got)
	}
}

// TestSystem_KeepsLoadBearingGuards pins the sentences that shape model
// behavior: tool discipline, anti-fake-save, background policy, asking.
// Rewording deletes the test's premise, so reword deliberately —
// update the fragments, never just delete them.
func TestPlatformLine_NamesCurrentOS(t *testing.T) {
	got := PlatformLine()
	if got == "" {
		t.Fatal("platform line must not be empty")
	}
	if !strings.Contains(strings.ToLower(got), runtime.GOOS) {
		t.Fatalf("platform line %q must name runtime.GOOS %q", got, runtime.GOOS)
	}
}

func TestSystem_KeepsLoadBearingGuards(t *testing.T) {
	for _, want := range []string{
		"does NOT save it anywhere",
		"Use move_file",
		"Use list_files",
		"grep_files",
		"For a server or watcher, use start_background",
		"use ask_user when available",
		"A tool-free response ends the run",
		"report incomplete work honestly",
		"Check tool results before claiming a change succeeded",
	} {
		if !strings.Contains(System, want) {
			t.Errorf("system prompt lost its guard %q", want)
		}
	}
}

func TestPresetLibraryIsCompleteAndIndependent(t *testing.T) {
	seen := map[string]bool{}
	for _, item := range Presets() {
		if seen[item.ID] {
			t.Fatal("duplicate preset", item.ID)
		}
		seen[item.ID] = true
		preset, err := ResponsePreset(item.ID)
		if err != nil || preset.Name == "" || (preset.ID != "custom" && preset.Instruction == "") {
			t.Fatal(preset, err)
		}
	}
	if preset, err := ResponsePreset(""); err != nil || preset.ID != "custom" || preset.Instruction != "" {
		t.Fatal(preset, err)
	}
	copy := Presets()
	copy[0].Name = "Changed"
	if Presets()[0].Name == "Changed" {
		t.Fatal("caller mutated library")
	}
}
