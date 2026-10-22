// Package prompts holds every piece of text the agent sends to the model
// that isn't data (task descriptions, tool results). Kept as plain .txt
// files under text/, embedded at build time, instead of Go string
// constants — the reason is git diffs, not aesthetics: a one-sentence
// wording change shows as a one-line diff in a .txt file, not buried in
// a multi-line Go string-concatenation expression.
//
// Anything with a %s/%d placeholder is exposed as a raw format string for
// fmt.Sprintf, not pre-parsed — keeps this package boring (just an
// embed.FS reader), no templating engine for something this simple.
package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
)

//go:embed text/*.txt
var files embed.FS

// Digest identifies the exact prompt files embedded in the running binary.
func Digest() string {
	h := sha256.New()
	entries, err := files.ReadDir("text")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		data, err := files.ReadFile("text/" + entry.Name())
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", entry.Name(), len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func read(name string) string {
	data, err := files.ReadFile("text/" + name)
	if err != nil {
		// All these files are embedded at compile time from a fixed list
		// below — a missing file is a build-time mistake, not a runtime
		// condition any caller can meaningfully recover from.
		panic("prompts: missing embedded file " + name + ": " + err.Error())
	}
	return strings.TrimRight(string(data), "\r\n")
}

var (
	System            = read("system.txt")
	Evaluation        = read("evaluation.txt")
	RoleAddendum      = read("role_addendum.txt")
	SubagentScope     = read("subagent_scope.txt")
	Resume            = read("resume.txt")
	Truncated         = read("truncated.txt")
	LeakDetected      = read("leak_detected.txt")
	OverflowRecovered = read("overflow_recovered.txt")
	RepeatingSteps    = read("repeating_steps.txt")
	BudgetContinue    = read("budget_continue.txt")
	WriteHint         = read("write_hint.txt")
	CompactNotice     = read("compact_notice.txt")
	PlanMode          = read("plan_mode.txt")
	ActMode           = read("act_mode.txt")
)

// WithRole returns System with RoleAddendum appended for role, or System
// unchanged if role is empty.
func WithRole(role string) string {
	if role == "" {
		return System
	}
	return System + "\n\n" + fmt.Sprintf(RoleAddendum, role)
}

// PlatformLine names the OS the agent runs on, for shell-capable roles.
// System text is static and embedded, so it cannot say this itself —
// and a weak model on Windows otherwise emits POSIX and burns steps on
// "command not found". Attach at role construction (Interactive,
// Subagent); Planner receives the same prefix but cannot execute shell calls.
func PlatformLine() string {
	if runtime.GOOS == "windows" {
		return "You run on Windows. Shell commands execute via cmd.exe, not PowerShell or Bash. " +
			"Use && to chain successful commands; ; is not a CMD separator. Invoke PowerShell explicitly for PowerShell syntax. " +
			"Use Windows-compatible syntax for builds, tests and scripts; use the provided file tools for listings, edits and moves. " +
			"Both `\\` and `/` work in file paths."
	}
	return "You run on " + runtime.GOOS + " with /bin/sh syntax. Do not assume additional utilities are installed."
}

// Environment keeps the cached prefix small; details are queried on demand.
func Environment(root string, vision bool) string {
	return fmt.Sprintf("Workspace: %s\nImage input enabled: %t. Query system_info for OS, hardware, environment or PATH tools when needed.", root, vision)
}

var responsePresets = []Preset{
	{"custom", "Custom", ""},
	{"conversation", "Conversation", read("preset-conversation.txt")},
	{"concise", "Concise", read("preset-concise.txt")},
	{"coding", "Coding", read("preset-coding.txt")},
	{"investigation", "Investigation", read("preset-investigation.txt")},
	{"explanation", "Explanation", read("preset-explanation.txt")},
}

// Preset controls response style, independently of identity and capabilities.
type Preset struct{ ID, Name, Instruction string }

func Presets() []Preset { return append([]Preset(nil), responsePresets...) }
func ResponsePreset(id string) (Preset, error) {
	if id == "" {
		id = "custom"
	}
	for _, preset := range responsePresets {
		if preset.ID == id {
			return preset, nil
		}
	}
	return Preset{}, fmt.Errorf("unknown prompt preset %q", id)
}
