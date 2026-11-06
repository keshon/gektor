package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/mcpmanager"
)

func TestMCPManagementUsesSharedFormsAndGuardsRunningTurns(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	root := t.TempDir()
	manager, e := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	m.mcp = manager
	m.state = stDone
	m.openMCP()
	if m.dialog == nil || m.dialog.form == nil || len(m.dialog.form.Options("mcp-action")) == 0 {
		t.Fatal("manager did not use form choices")
	}
	m.handleMCPAction(mcpActionMsg{"add", ""})
	if m.dialog.form == nil || len(m.dialog.form.Fields) < 3 {
		t.Fatal("add form missing")
	}
	m.state = stRunning
	m.handleMCPAction(mcpActionMsg{"connect", "missing"})
	if m.mcpBusy {
		t.Fatal("connection started during a turn")
	}
}

func TestMCPModalPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("optional fixture")
	}
	defer setTheme("dark")
	root := t.TempDir()
	manager, e := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	if e = manager.Save(mcpmanager.Server{ID: "yougile", Description: "YouGile project tools", URL: "https://example.com/mcp", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	for _, theme := range []string{"light", "dark"} {
		m := sizeModelTo(t, testModel(), 100, 34)
		m.applyTheme(theme)
		m.mcp = manager
		m.state = stDone
		m.openMCP()
		if e = os.WriteFile(filepath.Join(dir, "mcp-"+theme+".ansi"), []byte(m.View().Content), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
