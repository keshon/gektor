package browser

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	toolhost "github.com/keshon/gektor/internal/tools/host"
	"github.com/keshon/gektor/internal/workspace"
)

func TestCapabilityResumeDefersConnectionAndChecksCatalog(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	source := browser.Capability()
	first := &toolextension.Capabilities{Sources: []*toolextension.Capability{source}, Registry: agent.NewRegistry()}
	if _, err := first.Enable(t.Context(), "browser", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Enable(t.Context(), "browser", nil); err != nil || connects != 1 {
		t.Fatal("activation not idempotent", err, connects)
	}
	state := first.Save()
	if len(state) != 1 || state[0].Fingerprint == "" {
		t.Fatal(state)
	}
	available := false
	other := &toolextension.Capability{ID: source.ID, Version: source.Version, Source: source.Source, Instructions: source.Instructions, Load: func(ctx context.Context, note func(string)) ([]agent.Tool, error) {
		if !available {
			return nil, errors.New("browser missing")
		}
		return browser.load(ctx, note)
	}}
	reg := agent.NewRegistry()
	resumed := &toolextension.Capabilities{Registry: reg, Sources: []*toolextension.Capability{other}}
	if err := resumed.Restore(t.Context(), state); err != nil || connects != 1 {
		t.Fatal("resume connected", err, connects)
	}
	if !reg.Has("mcp__browser__browser_navigate") {
		t.Fatal("schema lost")
	}
	args := json.RawMessage(`{"url":"https://example.org"}`)
	if _, err := reg.Run(t.Context(), "mcp__browser__browser_navigate", args); err == nil || !strings.Contains(err.Error(), "browser missing") {
		t.Fatal(err)
	}
	available = true
	if _, err := reg.Run(t.Context(), "mcp__browser__browser_navigate", args); err != nil || actions != 1 {
		t.Fatal("explicit retry failed", err, actions)
	}
	other.Source = "changed configuration"
	if _, err := reg.Run(t.Context(), "mcp__browser__browser_navigate", args); err == nil || actions != 1 {
		t.Fatal("stale definition executed", err, actions)
	}
	missing := &toolextension.Capabilities{Registry: agent.NewRegistry()}
	if err := missing.Restore(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Registry.Run(t.Context(), "mcp__browser__browser_navigate", args); err == nil {
		t.Fatal("removed adapter executed")
	}
}

func TestSystemInfoReportsRestoredBrowserCatalogWithoutConnection(t *testing.T) {
	loads, actions := 0, 0
	browser := fakeBrowser(&loads, &actions)
	first := &toolextension.Capabilities{Registry: agent.NewRegistry(), Sources: []*toolextension.Capability{browser.Capability()}}
	if _, err := first.Enable(t.Context(), "browser", nil); err != nil {
		t.Fatal(err)
	}
	restored := &toolextension.Capabilities{Registry: agent.NewRegistry()}
	if err := restored.Restore(t.Context(), first.Save()); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	output, err := (toolhost.Tool{WS: ws, Capabilities: restored}).Run(t.Context(), json.RawMessage(`{"group":"browser"}`))
	if err != nil || !strings.Contains(output, "mcp__browser__browser_navigate") || !strings.Contains(output, "first use verifies connection") || loads != 1 {
		t.Fatal(output, err, loads)
	}
}

func TestProfileSwitchInvalidatesSharedSource(t *testing.T) {
	connects, actions := 0, 0
	b := fakeBrowser(&connects, &actions)
	b.Channel = "chrome"
	defer b.Close()
	source := b.Capability()
	if source != b.Capability() {
		t.Fatal("browser created multiple sources")
	}
	catalog := &toolextension.Capabilities{Registry: agent.NewRegistry(), Sources: []*toolextension.Capability{source}}
	if _, err := catalog.Enable(t.Context(), "browser", nil); err != nil {
		t.Fatal(err)
	}
	if err := b.SelectProfile(&conversation.BrowserProfile{Channel: "chrome", Profile: "other"}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := source.ConnectionStatus(); state != "disconnected" {
		t.Fatal("profile left stale catalog", state)
	}
	if _, err := catalog.Registry.Run(t.Context(), "mcp__browser__browser_navigate", json.RawMessage(`{"url":"http://localhost"}`)); err != nil {
		t.Fatal(err)
	}
	if connects != 2 || actions != 1 {
		t.Fatal("profile reused old adapter", connects, actions)
	}
}
