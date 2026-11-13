package mcpmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/mcpmanager/credentials"
)

func TestImportPersistsReferencesAndLifecycle(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "credentials")
	dir := filepath.Join(root, "mcp")
	m, e := New(dir, root, vault, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte(`{"mcpServers":{"service":{"type":"http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer fixture-secret"}}}}`)
	ids, e := m.Import(raw)
	if e != nil || len(ids) != 1 {
		t.Fatal(ids, e)
	}
	data, e := os.ReadFile(filepath.Join(dir, "service.json"))
	if e != nil || strings.Contains(string(data), "fixture-secret") {
		t.Fatal("credential leaked into config", e)
	}
	s, e := m.Get("service")
	if e != nil {
		t.Fatal(e)
	}
	value, e := (credentials.Store{Directory: vault}).Resolve(s.HeaderRefs["Authorization"], s.URL, "Authorization")
	if e != nil || value != "Bearer fixture-secret" {
		t.Fatal("credential not restored", e)
	}
	if _, e = (credentials.Store{Directory: vault}).Resolve(s.HeaderRefs["Authorization"], "https://other.example/mcp", "Authorization"); e == nil {
		t.Fatal("credential crossed origin")
	}
	if _, e = m.Import(raw); e == nil {
		t.Fatal("import silently replaced existing server")
	}
	s.Enabled = false
	if e = m.Save(s); e != nil {
		t.Fatal(e)
	}
	restored, e := New(dir, root, vault, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if len(restored.Sources()) != 0 {
		t.Fatal("disabled server exposed")
	}
	if _, e = restored.Connect(context.Background(), s.ID); e == nil {
		t.Fatal("connected disabled server")
	}
	if e = restored.Delete(s.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = (credentials.Store{Directory: vault}).Resolve(s.HeaderRefs["Authorization"], s.URL, "Authorization"); e == nil {
		t.Fatal("deleted server retained its credential")
	}
	if len(restored.List()) != 0 {
		t.Fatal("delete failed")
	}
}
func TestImportValidationBeforeSaving(t *testing.T) {
	for _, raw := range []string{
		`{"mcpServers":{"../bad":{"url":"https://example.com/mcp"}}}`,
		`{"mcpServers":{"bad":{"url":"http://127.0.0.1/mcp","headers":{"Authorization":"secret"}}}}`,
		`{"mcpServers":{"bad":{"url":"https://example.com/mcp","headers":{"Host":"other.example"}}}}`,
		`{"mcpServers":{"reserved":{"url":"https://example.com/mcp"}}}`,
	} {
		root := t.TempDir()
		m, e := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), []string{"reserved"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = m.Import([]byte(raw)); e == nil {
			t.Fatal("accepted invalid import")
		}
		if len(m.List()) != 0 {
			t.Fatal("partial invalid import")
		}
	}
}

func TestImportCommonTransportTypesAndDecodeErrors(t *testing.T) {
	for _, entry := range []string{
		`{"type":"http","url":"https://example.com/mcp"}`,
		`{"type":"streamable-http","url":"https://example.com/mcp"}`,
		`{"type":"stdio","command":"fixture-command","args":["--stdio"]}`,
	} {
		root := t.TempDir()
		m, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte(`{"mcpServers":{"n8n-mcp":` + entry + `}}`)
		ids, err := m.Import(raw)
		if err != nil || len(ids) != 1 || ids[0] != "n8n-mcp" {
			t.Fatal(ids, err)
		}
		if len(m.Sources()) != 1 {
			t.Fatal("import lost its transport")
		}
		m.Close()
	}
	for _, entry := range []string{
		`{"type":"sse","url":"https://example.com/sse"}`,
		`{"type":"stdio","url":"https://example.com/mcp"}`,
		`{"type":"http","command":"fixture-command"}`,
		`{"unknown":true,"url":"https://example.com/mcp"}`,
	} {
		root := t.TempDir()
		m, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Import([]byte(`{"mcpServers":{"n8n-mcp":` + entry + `}}`))
		if err == nil || strings.Contains(err.Error(), "expected an mcpServers object") || len(m.List()) != 0 {
			t.Fatal("invalid transport or hidden decode error", err)
		}
		m.Close()
	}
}
