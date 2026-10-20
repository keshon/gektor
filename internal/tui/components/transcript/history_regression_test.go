package transcript

import (
	"strings"
	"testing"
)

func TestHistoryAuditUnfollowedReaderCanScrollIntoNewLiveTail(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(50, 3)
	parts := []string{"old1\nold2\nold3\nold4\nold5"}
	m.SetParts(parts, "")
	m.Follow = false
	m.Viewport.GotoTop()
	m.SetParts(parts, "LIVE_TAIL")
	m.Viewport.GotoBottom()
	if !strings.Contains(m.View(), "LIVE_TAIL") {
		t.Fatalf("live tail missing from document: %q", m.View())
	}
}
