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
	"embed"
	"fmt"
	"runtime"
	"strings"
)

//go:embed text/*.txt
var files embed.FS

func read(name string) string {
	data, err := files.ReadFile("text/" + name)
	if err != nil {
		// All these files are embedded at compile time from a fixed list
		// below — a missing file is a build-time mistake, not a runtime
		// condition any caller can meaningfully recover from.
		panic("prompts: missing embedded file " + name + ": " + err.Error())
	}
	return strings.TrimRight(string(data), "\n")
}

var (
	System            = read("system.txt")
	RoleAddendum      = read("role_addendum.txt")
	SubagentScope     = read("subagent_scope.txt")
	Resume            = read("resume.txt")
	Truncated         = read("truncated.txt")
	LeakDetected      = read("leak_detected.txt")
	OverflowRecovered = read("overflow_recovered.txt")
	BudgetContinue    = read("budget_continue.txt")
	WriteHint         = read("write_hint.txt")
	CompactNotice     = read("compact_notice.txt")
	KoboldAddendum    = read("kobold_addendum.txt")
	OpenAIAddendum    = read("openai_addendum.txt")
	PlanMode          = read("plan_mode.txt")
)

// WithRole returns System with RoleAddendum appended for role, or System
// unchanged if role is empty.
func WithRole(role string) string {
	if role == "" {
		return System
	}
	return System + "\n\n" + fmt.Sprintf(RoleAddendum, role)
}

// SystemForBackend returns System with the backend-specific addendum for
// kind ("kobold", "llama", "openai"). Unknown kinds return System unchanged.
func SystemForBackend(kind string) string {
	switch kind {
	case "kobold":
		return System + "\n\n" + KoboldAddendum
	case "openai":
		return System + "\n\n" + OpenAIAddendum
	default:
		return System
	}
}

// PlatformLine names the OS the agent runs on, for shell-capable roles.
// System text is static and embedded, so it cannot say this itself —
// and a weak model on Windows otherwise emits POSIX and burns steps on
// "command not found". Attach at role construction (Interactive,
// Subagent, Worker); Planner and Inspector are read-only and never shell.
func PlatformLine() string {
	if runtime.GOOS == "windows" {
		return "You run on Windows. Shell commands execute via cmd.exe: use `dir` (not `ls`), " +
			"`copy`/`move`/`del` (not `cp`/`mv`/`rm`/`cat`). Both `\\` and `/` work in file paths."
	}
	return "You run on " + runtime.GOOS + " with a POSIX shell and standard coreutils available."
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
