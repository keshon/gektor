package agent

import (
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui/components/transcript"
)

type transcriptState struct {
	transcript.Model
	blocks  []block
	compact bool
	render  transcriptRenderState
}

// The GEKTOR renderer receives presentation values, never the application model.
type transcriptProps struct {
	speaker, user, live string
	selectedCall        string
	// liveThink is streamed deliberation, rendered above live so the turn
	// reads THINK then RESPONSE instead of one buffer that blanks the
	// answer to make room.
	liveThink string
	styles    styles
	// hideSYS drops all scaffolding history from the rendered transcript, and
	// labels drops the [TAG] rows. Both are display-only: the blocks stay
	// in m.transcript.blocks and come back unchanged when a filter lifts.
	hideSYS, labels             bool
	hideNames, hideTools        bool
	hideThinking, toolNamesOnly bool
	prefill                     *llm.Progress
	progressFrame               int
}

func newTranscript() transcriptState {
	return transcriptState{
		Model:   transcript.New(),
		compact: true,
	}
}

// attachResult lands a tool result on the most recent open card with a
// matching non-empty call ID. Unknown or empty IDs become a standalone
// block, so a parallel or leaked result can never corrupt the wrong card.
func (m *Model) attachResult(callID, text string, failed ...bool) bool {
	if callID == "" {
		return false
	}
	for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
		b := &m.transcript.blocks[i]
		if b.role == roleTool && b.open && b.callID == callID {
			b.result = text
			b.open = false
			b.failed = strings.HasPrefix(text, "error:") || strings.Contains(text, "\nerror:")
			if len(failed) > 0 {
				b.failed = failed[0]
			}
			m.refreshContent()
			return true
		}
	}
	return false
}

// appendBlock records a block and re-renders the transcript.
func (m *Model) appendBlock(b block) {
	if b.role == roleUser {
		m.transcript.Follow = true
	}
	m.transcript.blocks = append(m.transcript.blocks, b)
	m.refreshContent()
	if b.role == roleUser {
		m.transcript.Viewport.GotoBottom()
	}
}

// refreshContent re-renders every block into the viewport. Called on
// append, on toggles, and on resize (reflow follows the width). YOffset
// survives SetContent, so a reading user is never yanked (pinned by
// TestNewBlocksRespectUnfollow).
func (m *Model) refreshContent() {
	if !m.ready {
		return
	}
	if m.transcript.render.batch > 0 {
		m.transcript.render.dirty = true
		return
	}
	props := transcriptProps{
		speaker:       m.speakerName(),
		user:          m.userName(),
		styles:        m.styles,
		live:          m.live,
		liveThink:     m.liveThink,
		hideSYS:       m.hideSYS,
		labels:        !m.hideLabels,
		hideNames:     m.hideNames,
		hideTools:     m.hideTools,
		hideThinking:  m.hideThinking,
		toolNamesOnly: m.toolNamesOnly,
	}
	if m.inspector.selecting && m.inspector.selected >= 0 && m.inspector.selected < len(m.transcript.blocks) {
		props.selectedCall = m.transcript.blocks[m.inspector.selected].callID
	}
	if m.prefillActive() {
		p := m.progress
		props.prefill = &p
		props.progressFrame = m.spinnerFrame
	}
	m.transcript.refresh(props)
}

func (m *Model) beginTranscriptUpdate() { m.transcript.render.batch++ }

func (m *Model) endTranscriptUpdate() {
	m.transcript.render.batch--
	if m.transcript.render.batch == 0 && m.transcript.render.dirty {
		m.transcript.render.dirty = false
		m.refreshContent()
	}
}

func nonEmptySpeaker(name string) string {
	if name == "" || strings.EqualFold(name, "Assistant") {
		return "GEKTOR"
	}
	return name
}

func (m *Model) speakerName() string {
	clean := cleanText(m.identityName)
	words := strings.Fields(clean)
	name := strings.Join(words, " ")
	return nonEmptySpeaker(name)
}

func (m *Model) userName() string {
	name := m.identityUser
	if m.config != nil && m.config.Profile.Identity != nil {
		name = m.config.Profile.Identity.UserName
	}
	name = strings.Join(strings.Fields(cleanText(name)), " ")
	if name == "" {
		return "You"
	}
	return name
}
