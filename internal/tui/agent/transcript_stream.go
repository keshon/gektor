package agent

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Streamed text is bounded independently of terminal frame pacing.
const (
	liveMaxRunes = 64 * 1024
	revealFrame  = 16 * time.Millisecond
)

// resetLive clears both live buffers at a turn boundary. One method rather
// than the field list repeated at each site: forgetting a field strands the
// previous turn's text inside the next reply.
func (m *Model) resetLive() {
	m.live, m.liveQueue, m.liveCut, m.liveRunes = "", "", false, 0
	m.liveThink, m.liveThinkQueue = "", ""
}

// Coalesce rapid network fragments for one frame, then paint all received text.
// This timer is independent of the idle/spinner heartbeat.
func (m *Model) scheduleLiveFlush() {
	if m.liveFlushPending {
		return
	}
	m.liveFlushPending = true
	m.pendingCommands = append(m.pendingCommands, tea.Tick(revealFrame, func(time.Time) tea.Msg { return liveFlushMsg{} }))
}

// revealLive paints complete received chunks without a simulated typing rate.
func (m *Model) revealLive() bool {
	pending := false
	if m.liveThinkQueue != "" {
		think := []rune(m.liveThinkQueue)
		take := len(think)
		m.liveThink += string(think[:take])
		m.liveThinkQueue = string(think[take:])
		pending = true
	}
	if m.liveQueue == "" {
		if pending {
			m.refreshContent()
		}
		return pending
	}
	queued := []rune(m.liveQueue)
	take := len(queued)
	m.live += string(queued[:take])
	m.liveQueue = string(queued[take:])
	m.refreshContent()
	return false
}

// renderLive uses the same label rows and Markdown blocks as completed
// replies. The live buffer is one assistant turn, so the name row opens
// it exactly once.
func renderLive(props transcriptProps, width int, opts renderOpts) []string {
	var parts []string
	// Streamed deliberation renders first and stays put, so the turn reads
	// as THINK then RESPONSE. Wrapping in <think> lets assistantBlocks
	// build the same block a completed turn uses.
	if !props.hideThinking && strings.TrimSpace(props.liveThink) != "" {
		for _, b := range assistantBlocks("<think>"+props.liveThink+"</think>", "") {
			b.at = time.Time{}
			b.speaker = props.speaker
			part := renderBlock(b, props.styles, opts, width)
			if len(parts) == 0 && !props.hideNames {
				part = speakerRow(speakerLabel(b, props), b.role, props.styles, width) + "\n" + part
			}
			parts = append(parts, part)
		}
	}
	for _, b := range assistantBlocks(liveContent(props.live), "") {
		if props.hideThinking && b.role == roleThink {
			continue
		}
		b.at = time.Time{}
		b.speaker = props.speaker
		part := renderBlock(b, props.styles, opts, width)
		if len(parts) == 0 && !props.hideNames {
			part = speakerRow(speakerLabel(b, props), b.role, props.styles, width) + "\n" + part
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil
	}
	return strings.Split(strings.Join(parts, "\n\n"), "\n")
}

// A tag split across SSE frames must not flash as response/thinking text.
// Hold only its unfinished suffix; the stored message is never changed.
func liveContent(text string) string {
	for _, tag := range []string{"<think>", "</think>"} {
		for n := min(len(text), len(tag)-1); n > 0; n-- {
			if strings.HasSuffix(text, tag[:n]) {
				return text[:len(text)-n]
			}
		}
	}
	return text
}
