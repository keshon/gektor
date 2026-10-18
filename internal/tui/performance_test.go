package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/llm"
)

func benchmarkSessionRoot(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	history := []llm.Message{{Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: strings.Repeat("answer ", 32768)}}
	for i := 0; i < 100; i++ {
		writeSessionState(b, root, fmt.Sprint(i), history)
	}
	return root
}

func BenchmarkSessionIndexCold(b *testing.B) {
	root := benchmarkSessionRoot(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanSessions(root, nil, false)
	}
}

func BenchmarkSessionIndexWarm(b *testing.B) {
	root := benchmarkSessionRoot(b)
	_, cache := scanSessions(root, nil, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanSessions(root, cache, true)
	}
}

func BenchmarkTranscriptRefresh(b *testing.B) {
	m := benchmarkTranscript(b)
	// A frame that reveals nothing. The renderer is handed the same parts
	// and tail every time, which is what the settling tick and any frame
	// between stream chunks look like.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.live = "fresh streamed text"
		m.refreshContent()
	}
}

// The honest counterpart: every frame carries new text, so the parts differ
// and the viewport is reassembled. This is the cost the skip in
// BenchmarkTranscriptRefresh does not remove, and it grows with transcript
// length because SetParts re-measures every rendered line.
func BenchmarkTranscriptRefreshGrowing(b *testing.B) {
	m := benchmarkTranscript(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.live = "fresh streamed text " + strings.Repeat("y", i%17)
		m.refreshContent()
	}
}

func benchmarkTranscript(b *testing.B) *model {
	b.Helper()
	m := testModel()
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.state = stRunning
	for i := 0; i < 500; i++ {
		m.transcript.blocks = append(m.transcript.blocks, answerBlock(strings.Repeat("A line of transcript text. ", 40)))
	}
	m.refreshContent()
	return m
}
