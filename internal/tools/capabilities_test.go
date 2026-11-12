package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
	toolhost "github.com/keshon/gektor/internal/tools/host"
	toolweb "github.com/keshon/gektor/internal/tools/web"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowDiscoveryMatchesNodeAndScheduleVocabulary(t *testing.T) {
	var description string
	for _, p := range builtinPackages(Dependencies{}) {
		if p.id == "native_workflows" {
			description = p.description
		}
	}
	catalog := &Capabilities{Sources: []*Capability{{ID: "native_workflows", Description: description}}}
	for _, query := range []string{"workflow schedule cron", "schedule node", "workflow nodes", "WORKFLOW   cron"} {
		args, _ := json.Marshal(map[string]string{"query": query})
		out, err := (DiscoverCapabilities{Catalog: catalog}).Run(t.Context(), args)
		if err != nil || !strings.Contains(out, "native_workflows") {
			t.Fatal(query, out, err)
		}
	}
}

func TestMCPNamesUniqueAndBoundedForAdversarialCatalog(t *testing.T) {
	prefix := strings.Repeat("a", 54)
	defs := []mcp.ToolDefinition{{Name: prefix + "one"}, {Name: prefix + "two"}, {Name: prefix[:52] + "_2"}, {Name: "read"}, {Name: "read"}, {Name: "read_2"}, {Name: "a/b"}, {Name: "a_b"}}
	for _, server := range []string{"srv", strings.Repeat("s", 70)} {
		seen := map[string]bool{}
		for _, name := range assignToolNames(server, defs) {
			if seen[name] {
				t.Fatalf("duplicate %q", name)
			}
			seen[name] = true
			if err := agent.ValidateToolName(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, spec := range []string{"one=host;one=other", "bad.name=host", strings.Repeat("s", 33) + "=host"} {
		if _, _, _, err := ParseMCPFlag(spec); err == nil {
			t.Fatalf("invalid spec accepted: %s", spec)
		}
	}
}

func TestCapabilityResumeDefersConnectionAndChecksCatalog(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	source := browser.Capability()
	first := &Capabilities{Sources: []*Capability{source}, Registry: agent.NewRegistry()}
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
	other := &Capability{ID: source.ID, Version: source.Version, Source: source.Source, Instructions: source.Instructions, Load: func(ctx context.Context, note func(string)) ([]agent.Tool, error) {
		if !available {
			return nil, errors.New("browser missing")
		}
		return browser.load(ctx, note)
	}}
	reg := agent.NewRegistry()
	resumed := &Capabilities{Registry: reg, Sources: []*Capability{other}}
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
	missing := &Capabilities{Registry: agent.NewRegistry()}
	if err := missing.Restore(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Registry.Run(t.Context(), "mcp__browser__browser_navigate", args); err == nil {
		t.Fatal("removed adapter executed")
	}
}

func TestCapabilityDiscoveryAndFailureAreSideEffectFree(t *testing.T) {
	loads := 0
	source := &Capability{ID: "reader", Description: "Read widgets", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		loads++
		return nil, errors.New("missing prerequisite")
	}}
	catalog := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{source}}
	text, err := (DiscoverCapabilities{Catalog: catalog}).Run(t.Context(), json.RawMessage(`{"query":"widgets"}`))
	if err != nil || !strings.Contains(text, "reader") || loads != 0 {
		t.Fatal(text, err, loads)
	}
	if _, err := catalog.Enable(t.Context(), "reader", nil); err == nil || len(catalog.Save()) != 0 || len(catalog.Registry.Names()) != 0 {
		t.Fatal("failed activation installed tools", err)
	}
	if _, err := catalog.Enable(t.Context(), "unknown", nil); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

func TestCapabilityConfigValidation(t *testing.T) {
	valid := `{"version":1,"skill_roots":["library"],"packs":[{"id":"reader","main_only":true,"version":"1","description":"Read widgets","type":"executable","command":["host","literal argument"],"read_only":true,"concurrent":true,"tools":[{"name":"read","description":"Read one","input_schema":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}]}]}`
	file := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(file, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadCapabilityConfig(file)
	if err != nil || !filepath.IsAbs(config.SkillRoots[0]) {
		t.Fatal(config, err)
	}
	sources, close := BuildCapabilities(config, t.TempDir())
	defer close()
	if !config.Packs[0].MainOnly || !sources[0].MainOnly {
		t.Fatal("main-only configuration lost")
	}
	cat := &Capabilities{Sources: sources, Registry: agent.NewRegistry()}
	if _, err := cat.Enable(t.Context(), "reader", nil); err != nil {
		t.Fatal("activation ran unavailable executable", err)
	}
	if cat.Registry.ModeOf("cap__reader__read") != agent.Concurrent {
		t.Fatal("trusted scheduling lost")
	}
	for _, invalid := range []string{
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"read_only":true`, `"read_only":false`, 1),
		strings.Replace(valid, `"type":"string"`, `"type":"invalid"`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"unknown":true`, 1),
		valid + ` {}`,
	} {
		if err := os.WriteFile(file, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCapabilityConfig(file); err == nil {
			t.Fatal("invalid configuration accepted", invalid)
		}
	}
}

func TestRejectedCapabilityDiscoveryClosesOnlySuccessfulLoads(t *testing.T) {
	loaderFailure := errors.New("adapter unavailable")
	invalidSchema := &MCPTool{
		toolName: "mcp__invalid__read",
		def:      mcp.ToolDefinition{Name: "read", InputSchema: json.RawMessage(`{"type":"invalid"}`)},
	}
	cases := []struct {
		name             string
		tools            []agent.Tool
		policies         map[string]ToolPolicy
		loadErr          error
		closesPerAttempt int
	}{
		{name: "empty catalog", closesPerAttempt: 1},
		{name: "duplicate tool", tools: []agent.Tool{toolweb.Check{}, toolweb.Check{}}, closesPerAttempt: 1},
		{name: "unknown policy tool", tools: []agent.Tool{toolweb.Check{}}, policies: map[string]ToolPolicy{"missing": {}}, closesPerAttempt: 1},
		{name: "invalid schema", tools: []agent.Tool{invalidSchema}, closesPerAttempt: 1},
		{name: "loader error", loadErr: loaderFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loads, closed := 0, 0
			list, discoveryErr := tc.tools, tc.loadErr
			source := &Capability{
				ID:           "invalid",
				ToolPolicies: tc.policies,
				Close:        func() { closed++ },
				Load: func(context.Context, func(string)) ([]agent.Tool, error) {
					loads++
					return list, discoveryErr
				},
			}
			catalog := &Capabilities{Sources: []*Capability{source}, Registry: agent.NewRegistry()}
			for attempt := 1; attempt <= 2; attempt++ {
				_, err := catalog.Enable(t.Context(), source.ID, nil)
				if err == nil || closed != attempt*tc.closesPerAttempt || loads != attempt || len(catalog.Registry.Names()) != 0 || len(catalog.Save()) != 0 {
					t.Fatalf("rejected discovery leaked or cached: err=%v closes=%d loads=%d", err, closed, loads)
				}
				if tc.loadErr != nil && !errors.Is(err, tc.loadErr) {
					t.Fatalf("loader error was lost: %v", err)
				}
				state, count, detail := source.ConnectionStatus()
				if state != "failed" || count != 0 || detail == "" {
					t.Fatalf("failure status = %s %d %q", state, count, detail)
				}
			}

			list, discoveryErr = []agent.Tool{toolweb.Check{}}, nil
			source.ToolPolicies = nil
			_, err := catalog.Enable(t.Context(), source.ID, nil)
			if err != nil {
				t.Fatal("retry with valid catalog", err)
			}
			other := &Capabilities{Sources: []*Capability{source}, Registry: agent.NewRegistry()}
			_, err = other.Enable(t.Context(), source.ID, nil)
			if err != nil {
				t.Fatal("reuse shared discovery", err)
			}
			if loads != 3 || closed != 2*tc.closesPerAttempt || !catalog.Registry.Has("check_url") || !other.Registry.Has("check_url") {
				t.Fatalf("valid discovery was closed or reloaded: closes=%d loads=%d", closed, loads)
			}
			state, count, detail := source.ConnectionStatus()
			if state != "connected" || count != 1 || detail != "" {
				t.Fatalf("recovered status = %s %d %q", state, count, detail)
			}
		})
	}
}

func TestSystemInfoReportsRestoredBrowserCatalogWithoutConnection(t *testing.T) {
	loads, actions := 0, 0
	browser := fakeBrowser(&loads, &actions)
	first := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{browser.Capability()}}
	if _, err := first.Enable(t.Context(), "browser", nil); err != nil {
		t.Fatal(err)
	}
	restored := &Capabilities{Registry: agent.NewRegistry()}
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
