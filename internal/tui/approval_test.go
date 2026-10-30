package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/workspace"
)

func TestApprovalInspectionAndStaleReapproval(t *testing.T) {
	for _, frame := range []string{"minimal", "outlined"} {
		t.Run(frame, func(t *testing.T) { testApprovalInspection(t, frame) })
	}
}

func testApprovalInspection(t *testing.T, frame string) {
	m := sizeModel(t, testModel())
	m.applyAppearance(config.Appearance{Frame: frame, HideNames: true})
	m.hub = run.NewGateHub(func(name string, fields map[string]any) { m.events <- run.Event{Name: name, Fields: fields} })
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	args, _ := json.Marshal(map[string]string{"path": "new.txt", "content": strings.Repeat("line\n", 65) + "FINAL_LINE"})
	done := make(chan permission.Effect, 1)
	go func() {
		effect, err := m.contextGate(m.hub, ws)(ctx, "write_file", "new.txt", args)
		if err != nil {
			effect = permission.Deny
		}
		done <- effect
	}()
	await := func() {
		t.Helper()
		for {
			select {
			case ev := <-m.events:
				m.handleEvent(ev)
				if ev.Name == "awaiting_input" {
					return
				}
			case <-ctx.Done():
				t.Fatal("gate never opened")
			}
		}
	}
	await()
	export := func(name string) {
		if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "refinement-"+frame+"-"+name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	export("approval")
	if !strings.Contains(m.gatePreview, "+FINAL_LINE") || strings.Contains(m.gate, "FINAL_LINE") {
		t.Fatal("wrong evidence boundary")
	}
	before := len(m.transcript.blocks)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF4})
	if m.dialog == nil || !m.hub.HasPending() || len(m.transcript.blocks) != before {
		t.Fatal("inspection answered gate or changed context")
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	export("proposal")
	m.handleMouse(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.dialog.offset == 0 {
		t.Fatal("wheel ignored proposal")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.dialog.offset < 2 {
		t.Fatal("page down ignored proposal")
	}
	for i := 0; i < 80; i++ {
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.dialog.offset == 0 {
		t.Fatal("full proposal does not scroll")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil || m.state != stPermission || !m.hub.HasPending() {
		t.Fatal("closing inspection lost gate")
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	m.resolveGate("a")
	await()
	if !strings.Contains(m.gate, "Source changed") || !strings.Contains(m.gatePreview, "-external edit") {
		t.Fatal("stale approval was not refreshed")
	}
	select {
	case <-done:
		t.Fatal("stale proposal authorized execution")
	default:
	}
	m.alwaysMu.Lock()
	allowed := m.always[[2]string{"write_file", "new.txt"}]
	m.alwaysMu.Unlock()
	if allowed {
		t.Fatal("stale approval saved always scope")
	}
	m.resolveGate("y")
	select {
	case effect := <-done:
		if effect != permission.Allow {
			t.Fatal(effect)
		}
	case <-ctx.Done():
		t.Fatal("fresh approval failed")
	}
}
