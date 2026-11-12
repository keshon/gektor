package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
	toolweb "github.com/keshon/gektor/internal/tools/web"
)

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

func TestDiscoveryRanksKnownNamesAndPagesWithoutLoading(t *testing.T) {
	loads := 0
	catalog := &Capabilities{Registry: agent.NewRegistry(), Sources: []*Capability{
		{ID: "other", Description: "Guide mentioning check_url"},
		{ID: "reader", Description: "Read URLs", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
			loads++
			return []agent.Tool{toolweb.Check{}}, nil
		}},
	}}
	tool := DiscoverCapabilities{Catalog: catalog}
	query := func(args string) string {
		t.Helper()
		out, err := tool.Run(t.Context(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := query(`{"limit":1}`)
	if !strings.Contains(before, `"next_offset":1`) || loads != 0 {
		t.Fatal(before, loads)
	}
	second := query(`{"offset":1,"limit":1}`)
	if !strings.Contains(second, `"id":"reader"`) || strings.Contains(second, "next_offset") {
		t.Fatal(second)
	}
	empty := query(`{"query":"unrelated"}`)
	if !strings.Contains(empty, `"capabilities":[]`) || !strings.Contains(empty, `"hint"`) {
		t.Fatal(empty)
	}

	_, err := catalog.Enable(t.Context(), "reader", nil)
	if err != nil {
		t.Fatal(err)
	}
	after := query(`{"query":"check_url","limit":1}`)
	if !strings.Contains(after, `"id":"reader"`) || !strings.Contains(after, `"state":"active"`) || loads != 1 {
		t.Fatal(after, loads)
	}
	for _, args := range []string{`{"offset":-1}`, `{"limit":0}`, `{"limit":101}`, `{"extra":1}`} {
		_, err = tool.Run(t.Context(), json.RawMessage(args))
		if err == nil {
			t.Fatal("invalid discovery request accepted", args)
		}
	}

	restored := &Capabilities{Registry: agent.NewRegistry()}
	err = restored.Restore(t.Context(), catalog.Save())
	if err != nil {
		t.Fatal(err)
	}
	tool = DiscoverCapabilities{Catalog: restored}
	out := query(`{"query":"check_url"}`)
	if !strings.Contains(out, `"state":"unavailable"`) {
		t.Fatal(out)
	}
}

func TestDiscoverySearchesExecutableDefinitionsBeforeActivation(t *testing.T) {
	config := CapabilityConfig{Version: 1, Packs: []CapabilityPack{{
		ID: "opaque", Type: "executable", Command: []string{"not-installed"},
		Tools: []ExecutableToolDef{{Name: "widget_read", Description: "Read widget inventory", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}}}
	sources, close := BuildCapabilities(config, t.TempDir())
	defer close()
	catalog := &Capabilities{Registry: agent.NewRegistry(), Sources: sources}
	tool := DiscoverCapabilities{Catalog: catalog}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"query":"widget_read"}`))
	if err != nil || !strings.Contains(out, `"id":"opaque"`) {
		t.Fatal(out, err)
	}
	if len(catalog.Save()) != 0 {
		t.Fatal("metadata search activated pack")
	}
}

func TestDiscoveryBoundsLargeInventories(t *testing.T) {
	catalog := &Capabilities{Registry: agent.NewRegistry()}
	for i := range 200 {
		catalog.Sources = append(catalog.Sources, &Capability{ID: fmt.Sprintf("pack-%03d", i), Description: "Read reference material"})
	}
	tool := DiscoverCapabilities{Catalog: catalog}
	out, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Capabilities []capabilityEntry `json:"capabilities"`
		Total        int               `json:"total"`
		NextOffset   int               `json:"next_offset"`
		More         bool              `json:"more"`
	}
	err = json.Unmarshal([]byte(out), &page)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Capabilities) != 50 || page.Total != 200 || page.NextOffset != 50 || !page.More {
		t.Fatal(out)
	}
	if len(out) > 8192 {
		t.Fatal("small inventory page grew beyond 8 KiB", len(out))
	}
}
