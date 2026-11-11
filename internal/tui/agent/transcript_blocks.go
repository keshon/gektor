package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// role names the kind of a transcript block. Every role maps to one
// label tag and one speaker, so each has a stable display identity.
type role int

const (
	roleAnswer role = iota
	roleUser
	roleThink
	roleTool
	roleResult
	roleGate
	roleMarker
	roleError
	// roleWarning is a language-independent content finding:
	// report-only signal, never an error, so it takes the warning's
	// unbold yellow rather than the error's bold red.
	roleWarning
	roleStatus
	roleReceipt
)

// block is one transcript entry: raw content plus its role. Styling
// happens at render time, so toggles (think expand) re-render without
// touching history.
type block struct {
	speaker string
	role    role
	text    string
	// at stamps when the block entered the transcript. A zero time means
	// "unstamped": reopened history records no display times, so inventing
	// one would claim a moment the transcript never observed.
	at time.Time
	// Tool card fields (roleTool only): callID pairs the result event,
	// result/output text lands on attach, open until it does, failed
	// when the result reports an error.
	callID      string
	result      string
	open        bool
	failed      bool
	returned    bool
	toolStatus  string
	contextText string
}

// assistantBlocks normalizes both backend reasoning formats for display only.
// Stored messages remain unchanged, including incomplete think regions.
func assistantBlocks(content, reasoning string) []block {
	var answer strings.Builder
	thoughts := []string{}
	if strings.TrimSpace(reasoning) != "" {
		thoughts = append(thoughts, reasoning)
	}
	prev := 0
	regions := llm.ThinkRegions(content)
	for _, region := range regions {
		answer.WriteString(content[prev:region[0]])
		thought := content[region[0]:region[1]]
		thought = strings.TrimPrefix(thought, "<think>")
		thought = strings.TrimSuffix(thought, "</think>")
		thought = strings.TrimSpace(thought)
		if thought != "" {
			thoughts = append(thoughts, thought)
		}
		prev = region[1]
	}
	answer.WriteString(content[prev:])
	var blocks []block
	if len(thoughts) > 0 {
		blocks = append(blocks, thinkBlock(strings.Join(thoughts, "\n\n")))
	}
	text := answer.String()
	if len(regions) > 0 {
		text = strings.TrimSpace(text)
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, answerBlock(text))
	}
	return blocks
}

func answerBlock(s string) block { return block{role: roleAnswer, text: s, at: time.Now()} }
func userBlock(s string) block   { return block{role: roleUser, text: s, at: time.Now()} }
func thinkBlock(s string) block  { return block{role: roleThink, text: s, at: time.Now()} }
func resultBlock(s string) block { return block{role: roleResult, text: s, at: time.Now()} }
func gateBlock(s string) block   { return block{role: roleGate, text: s, at: time.Now()} }
func markerBlock(s string) block {
	r := roleStatus
	if strings.HasPrefix(s, harnessPrefix) {
		r = roleMarker
	}
	return block{role: r, text: s, contextText: s, at: time.Now()}
}

func contextBlock(msg llm.Message) block {
	b := markerBlock(msg.Content)
	state := "pending next request"
	if msg.Request > 0 {
		state = fmt.Sprintf("included in request %d", msg.Request)
	}
	b.text += fmt.Sprintf("\n[role=%s · %s]", msg.Role, state)
	// Completion notifications are tool evidence even though the harness sends
	// them as context. Use the same compact/inspect presentation as tool results.
	header, _, notification := strings.Cut(strings.TrimPrefix(msg.Content, harnessPrefix), ". Historical command output:\n")
	if notification && strings.HasPrefix(header, "Background process ") {
		id, status, ok := strings.Cut(strings.TrimPrefix(header, "Background process "), " ")
		if ok && (status == "completed successfully" || strings.HasPrefix(status, "failed: ")) {
			args, _ := json.Marshal(map[string]string{"id": id})
			b.role, b.callID = roleTool, "background:"+id
			b.result, b.text = b.text, "background_process "+string(args)
			b.failed = strings.HasPrefix(status, "failed: ")
			if b.failed {
				b.result = "error: " + strings.TrimPrefix(status, "failed: ") + "\n" + b.result
			}
		}
	}
	return b
}
func errorBlock(s string) block { return block{role: roleError, text: s, at: time.Now()} }

// findingBlock renders one deterministic-check finding as plain text;
// the rule carries the meaning, the warning style the urgency.
func findingBlock(rule, path string, line int, summary string) block {
	text := "check " + rule
	if path != "" {
		text += fmt.Sprintf(" %s:%d", path, line)
	}
	return block{role: roleWarning, text: text + " " + summary, at: time.Now()}
}

// toolCardBlock opens a tool card: the header renders immediately, the
// result attaches by ID when its event lands. No arrow prefix — the role
// gutter already marks tool lines. One line always: models emit
// pretty-printed multi-line args. Compact JSON keeps string values intact;
// presentation chooses whether to summarize or wrap the complete arguments.
func toolCardBlock(callID, text string) block {
	return block{role: roleTool, text: compactToolText(text), at: time.Now(), callID: callID, open: true}
}

// harnessPrefix is the marker the agent writes into nudge content. It
// names the same thing as the role's label row, so it is stripped at the
// display boundary rather than in the prompt: stored messages keep the text
// they were written with, and the transcript shows what the label row plus
// body actually say.
//
// Deliberately a second copy of agent's marker rather than an import: this
// is display vocabulary, and the two only have to agree on the literal.
const harnessPrefix = "[harness] "

// normalizeHarnessMarker strips the redundant [harness] prefix and the
// leading space a step tag carries for inline use. On its own display row
// the body column is already the indent that space stood in for, and
// keeping both pushed the line one cell right of every other row.
//
// It runs here rather than in the renderer because a marker is literal text
// taking a different path than an answer, and both fixes must hold
// everywhere.
func normalizeHarnessMarker(b block) block {
	if b.role != roleMarker || !strings.HasPrefix(b.text, harnessPrefix) {
		return b
	}
	lines := strings.Split(strings.TrimPrefix(b.text, harnessPrefix), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "(step ") {
			lines[i] = strings.TrimLeft(line, " \t")
		}
	}
	b.text = strings.Join(lines, "\n")
	return b
}

// cleanText normalizes line endings at the display boundary. Tool results
// carry the file's own CRLF and model deltas may echo them back. A stray
// \r moves the cursor to column 0 mid-line, so anything written after it
// (alignStamp's padding) overwrites the line head and the alt-screen
// repaint desyncs into ghost frames. A lone \r becomes a newline: it was a
// line break where it came from.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
