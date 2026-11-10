package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
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
	if m.dialog == nil || m.dialog.table == nil {
		t.Fatal("manager did not use the shared table")
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

func TestMCPPasteImportFromEmptyManager(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	root := t.TempDir()
	manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	m.mcp, m.state = manager, stDone
	m.openMCP()
	m.tableInput(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !m.dialog.form.Fields[0].Multiline {
		t.Fatal("JSON field does not accept multiple lines")
	}
	raw := "{\n  \"mcpServers\": {\n    \"reference\": {\"url\": \"https://example.com/mcp\"}\n  }\n}"
	m.Update(tea.PasteMsg{Content: raw})
	if m.dialog.form.Values()["json"] != raw {
		t.Fatal("paste changed the configuration")
	}
	m.applyForm()
	if m.dialog != nil {
		t.Fatal("import failed:", m.dialog.form.Error)
	}
	saved, err := manager.Get("reference")
	if err != nil || saved.URL != "https://example.com/mcp" {
		t.Fatal("pasted server was not saved", err)
	}
	if manager.List()[0].Connected {
		t.Fatal("import connected without a request")
	}
	// The pasted definition must remain available after reopening the manager.
	reopened, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Get("reference"); err != nil {
		t.Fatal(err)
	}
}

func TestMCPImportValidationAndExplicitReplacement(t *testing.T) {
	for _, action := range []string{"import-json", "import"} {
		t.Run(action, func(t *testing.T) {
			m := sizeModelTo(t, testModel(), 100, 34)
			root := t.TempDir()
			manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			m.mcp, m.state = manager, stDone
			m.handleMCPAction(mcpActionMsg{action, ""})
			apply := m.dialog.form.apply
			values := map[string]string{"json": "{", "replace": "off"}
			if action == "import" {
				values["path"] = filepath.Join(root, "config.json")
			}
			setJSON := func(raw string) {
				t.Helper()
				values["json"] = raw
				if action == "import" {
					if err := os.WriteFile(values["path"], []byte(raw), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			setJSON("{")
			if err := apply(values); err == nil || len(manager.List()) != 0 {
				t.Fatal("invalid JSON was accepted")
			}
			setJSON(`{"mcpServers":{"reference":{"url":"https://example.com/original"}}}`)
			if err := apply(values); err != nil {
				t.Fatal(err)
			}
			setJSON(`{"mcpServers":{"reference":{"url":"https://example.com/updated"}}}`)
			if err := apply(values); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatal("replacement did not require opt-in", err)
			}
			saved, _ := manager.Get("reference")
			if saved.URL != "https://example.com/original" {
				t.Fatal("rejected import changed server")
			}
			values["replace"] = "on"
			if err := apply(values); err != nil {
				t.Fatal(err)
			}
			saved, _ = manager.Get("reference")
			if saved.URL != "https://example.com/updated" {
				t.Fatal("replacement was not saved")
			}
		})
	}
}

func TestMCPModalPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("optional fixture")
	}
	defer setTheme("dark")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager, e := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	if e = manager.Save(mcpmanager.Server{ID: "yougile", Description: "YouGile project tools", URL: "https://example.com/mcp", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	for _, theme := range config.ThemeNames() {
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

func TestMCPTableKeepsSelectionAndGuardsDirectActions(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	root := t.TempDir()
	manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.Save(mcpmanager.Server{ID: "reference", Description: "Reference tools", URL: "https://example.com/mcp", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	m.mcp, m.state = manager, stRunning
	m.openMCP()
	m.handleKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.mcpBusy || m.dialog.table == nil {
		t.Fatal("Connect activated while a turn was running")
	}
	m.handleKey(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.PasteMsg{Content: "reference"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.table != nil || !strings.Contains(strings.Join(m.dialog.lines, "\n"), "Server: reference") {
		t.Fatal("Details did not open for the selected server")
	}
	m.closeDialog()
	m.Update(m.pendingCommands[len(m.pendingCommands)-1]())
	if m.dialog.table == nil || m.dialog.table.model.Search.Value() != "reference" {
		t.Fatal("Details lost the parent filter")
	}
	m.state = stDone
	m.handleKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	saved, err := manager.Get("reference")
	if err != nil || saved.Enabled || manager.List()[0].Connected {
		t.Fatal("Disable did not update exactly the selected server", err)
	}
	if m.dialog.table == nil || m.dialog.table.model.Selected() != "reference" {
		t.Fatal("refresh lost selected server")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.dialog.form == nil || !m.dialog.form.Destructive || m.dialog.form.Index != 1 {
		t.Fatal("Delete bypassed the safe confirmation")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, err := manager.Get("reference"); err != nil {
		t.Fatal("Cancel deleted the server")
	}
}
