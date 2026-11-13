package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/tools/extension"
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
	if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
		t.Fatal("manager did not use the shared table")
	}
	m.handleMCPAction(mcpActionMsg{"add", ""})
	if m.chrome.Dialog.Form == nil || len(m.chrome.Dialog.Form.Fields) < 3 {
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
	m.testKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !m.chrome.Dialog.Form.Fields[0].Multiline {
		t.Fatal("JSON field does not accept multiple lines")
	}
	raw := "{\n  \"mcpServers\": {\n    \"reference\": {\"url\": \"https://example.com/mcp\"}\n  }\n}"
	m.testUpdate(tea.PasteMsg{Content: raw})
	if m.chrome.Dialog.Form.Values()["json"] != raw {
		t.Fatal("paste changed the configuration")
	}
	m.applyTestForm()
	if m.chrome.Dialog != nil {
		t.Fatal("import failed:", m.chrome.Dialog.Form.Error)
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
			apply := func(values map[string]string) error { return m.applySubmission(m.chrome.Dialog, values) }
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
		if e = os.WriteFile(filepath.Join(dir, "mcp-"+theme+".ansi"), []byte(m.testView().Content), 0600); e != nil {
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
	m.testKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.mcpBusy || m.chrome.Dialog.Table == nil {
		t.Fatal("Connect activated while a turn was running")
	}
	m.testKey(tea.KeyPressMsg{Code: '/'})
	m.testUpdate(tea.PasteMsg{Content: "reference"})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog.Table != nil || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), "Server: reference") {
		t.Fatal("Details did not open for the selected server")
	}
	m.chrome.Close()
	m.syncControls()
	m.testUpdate(m.pendingCommands[len(m.pendingCommands)-1]())
	if m.chrome.Dialog.Table == nil || m.chrome.Dialog.Table.Model.Search.Value() != "reference" {
		t.Fatal("Details lost the parent filter")
	}
	m.state = stDone
	m.testKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	saved, err := manager.Get("reference")
	if err != nil || saved.Enabled || manager.List()[0].Connected {
		t.Fatal("Disable did not update exactly the selected server", err)
	}
	if m.chrome.Dialog.Table == nil || m.chrome.Dialog.Table.Model.Selected() != "reference" {
		t.Fatal("refresh lost selected server")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.chrome.Dialog.Form == nil || !m.chrome.Dialog.Form.Destructive || m.chrome.Dialog.Form.Index != 1 {
		t.Fatal("Delete bypassed the safe confirmation")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, err := manager.Get("reference"); err != nil {
		t.Fatal("Cancel deleted the server")
	}
}

func TestBundledMCPUsesSharedSettingsWithoutRawCommand(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	root := t.TempDir()
	manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	source := &extension.Capability{ID: "browser", Description: "Playwright browser"}
	if err = manager.RegisterBundled(source); err != nil {
		t.Fatal(err)
	}
	m.mcp, m.state = manager, stDone
	m.openMCP()
	row := m.chrome.Dialog.Table.Model.Rows[0]
	if row.Cells[2] != "Bundled" {
		t.Fatal("bundled origin missing", row)
	}
	for _, action := range m.chrome.Dialog.Table.Actions {
		if action.ID == "delete" && action.Enabled {
			t.Fatal("bundle can be deleted")
		}
	}
	m.handleMCPAction(mcpActionMsg{"edit", "browser"})
	if m.chrome.Dialog.Form == nil || len(m.chrome.Dialog.Form.Fields) != 2 {
		t.Fatal("bundle opened generic command editor")
	}
	if err = m.applySubmission(m.chrome.Dialog, map[string]string{"enabled": "on", "loading": "on load"}); err != nil {
		t.Fatal(err)
	}
	if !source.Eager || manager.List()[0].Connected {
		t.Fatal("settings failed or connected implicitly")
	}
	m.handleMCPAction(mcpActionMsg{"disable", "browser"})
	if manager.List()[0].Enabled {
		t.Fatal("bundle cannot be disabled")
	}
	m.handleMCPAction(mcpActionMsg{"details", "browser"})
	text := strings.Join(m.chrome.Dialog.Lines, "\n")
	if !strings.Contains(text, "Origin: Bundled") || !strings.Contains(text, "Loading: On load") {
		t.Fatal(text)
	}
}
