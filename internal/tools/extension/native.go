package extension

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// Exposure controls schemas, never execution permissions or transport lifetime.
type Exposure uint8

const (
	Always Exposure = iota
	Eager
	Lazy
)

// NativePackage binds definitions to one agent's concrete tool instances.
// Unlike adapter sources, these bindings must never be shared between agents.
type NativePackage struct {
	ID, Version, Description, Instructions string
	Exposure                               Exposure
	Tools                                  []agent.Tool
	ReadOnly                               []string
}

func (p *NativePackage) state() conversation.Capability {
	registry := agent.NewRegistry(p.Tools...)
	defs := registry.Defs()
	modes := make([]agent.ToolMode, 0, len(defs))
	for _, def := range defs {
		modes = append(modes, registry.ModeOf(def.Name))
	}
	readOnly := slices.Clone(p.ReadOnly)
	sort.Strings(readOnly)
	data, _ := json.Marshal(struct {
		ID, Version, Instructions string
		Tools                     []llm.ToolDef
		ReadOnly                  []string
		Modes                     []agent.ToolMode
	}{p.ID, p.Version, p.Instructions, defs, readOnly, modes})
	return conversation.Capability{ID: p.ID, Version: p.Version, Instructions: p.Instructions, Tools: defs, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(data))}
}

func (c *Capabilities) native(id string) *NativePackage {
	for _, p := range c.Native {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func sameCapability(a, b conversation.Capability) bool {
	// JSON round trips can alter whitespace in RawMessage schemas.
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && string(x) == string(y)
}

func (c *Capabilities) publish(ctx context.Context, state conversation.Capability, list []agent.Tool) error {
	for _, t := range list {
		if slices.Contains(c.ForbiddenTools, t.Name()) {
			return fmt.Errorf("tool %q is unavailable to this role", t.Name())
		}
		if err := agent.ValidateToolSchema(t.Schema()); err != nil {
			return fmt.Errorf("tool %s: %w", t.Name(), err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	add := c.Registry.AddOptional
	if p := c.native(state.ID); (p != nil && p.Exposure == Eager) || (p == nil && c.source(state.ID).Eager) || !c.Registry.Has("invoke_tool") {
		add = c.Registry.Add
	}
	if err := add(list...); err != nil {
		return err
	}
	if c.active == nil {
		c.active = map[string]conversation.Capability{}
	}
	c.active[state.ID] = state
	return nil
}

// Prepare publishes eager native packages after archive restoration. Native
// construction opens no network connections; adapter sources remain lazy.
func (c *Capabilities) Prepare(ctx context.Context) error {
	if err := c.Validate(); err != nil {
		return err
	}

	for _, source := range c.Sources {
		if source.Eager && (!c.Subagent || !source.MainOnly) {
			if _, err := c.Enable(ctx, source.ID, nil); err != nil {
				return err
			}
		}
	}
	for _, p := range c.Native {
		if p.Exposure == Eager {
			if _, err := c.Enable(ctx, p.ID, nil); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// Validate checks catalog source identities before composition.
func (c *Capabilities) Validate() error {
	seen := map[string]bool{}
	for _, source := range c.Sources {
		if source == nil {
			return fmt.Errorf("nil capability source")
		}
		if strings.HasPrefix(source.ID, "native_") {
			return fmt.Errorf("reserved native capability ID %q", source.ID)
		}
		if err := validateCapabilityID(source.ID); err != nil {
			return err
		}
		if seen[source.ID] {
			return fmt.Errorf("duplicate capability ID %q", source.ID)
		}
		seen[source.ID] = true
	}
	for _, p := range c.Native {
		if p == nil {
			return fmt.Errorf("nil native package")
		}
		if !strings.HasPrefix(p.ID, "native_") {
			return fmt.Errorf("native package ID %q must start with native_", p.ID)
		}
		if err := validateCapabilityID(p.ID); err != nil {
			return err
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate capability ID %q", p.ID)
		}
		seen[p.ID] = true
		if p.Exposure != Eager && p.Exposure != Lazy {
			return fmt.Errorf("invalid optional exposure for %s", p.ID)
		}
		if len(p.Tools) == 0 {
			return fmt.Errorf("empty native package %s", p.ID)
		}
		if err := agent.NewRegistry(p.Tools...).Err(); err != nil {
			return err
		}
	}
	return nil
}

// PreviewDefs includes pending eager native tools without publishing or loading
// anything. It keeps startup context reporting accurate before preparation.
func (c *Capabilities) PreviewDefs() []llm.ToolDef {
	c.mu.Lock()
	defer c.mu.Unlock()
	defs := c.Registry.ModelDefs()
	for _, p := range c.Native {
		if _, active := c.active[p.ID]; !active && p.Exposure == Eager {
			defs = append(defs, p.state().Tools...)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

type capabilityEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	State       string `json:"state"`
	names       []string
	eager       bool
}

func (c *Capabilities) entries() []capabilityEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := []capabilityEntry{}
	seen := map[string]bool{}
	for _, source := range c.Sources {
		if !c.available(source.ID) {
			continue
		}
		names := slices.Clone(source.searchNames)
		names = append(names, source.ToolNames()...)
		entry := capabilityEntry{ID: source.ID, Description: source.Description, State: "configured", names: names}
		if state, active := c.active[source.ID]; active {
			entry.State = "active"
			for _, def := range state.Tools {
				entry.names = append(entry.names, def.Name)
			}
		}
		out = append(out, entry)
		seen[source.ID] = true
	}
	for _, p := range c.Native {
		entry := capabilityEntry{ID: p.ID, Description: p.Description, State: "configured", eager: p.Exposure == Eager}
		if _, active := c.active[p.ID]; active {
			entry.State = "active"
		}
		for _, tool := range p.Tools {
			entry.names = append(entry.names, tool.Name())
		}
		out = append(out, entry)
		seen[p.ID] = true
	}
	for id, state := range c.active {
		if seen[id] {
			continue
		}
		entry := capabilityEntry{ID: id, Description: "Saved catalog; source is no longer configured", State: "unavailable"}
		for _, def := range state.Tools {
			entry.names = append(entry.names, def.Name)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Map is stable across activation and bounded independently of provider tokens.
func (c *Capabilities) Map() string {
	entries := c.entries()
	if len(entries) == 0 {
		return ""
	}
	text := "\nOptional capabilities:"
	for _, entry := range entries {
		if entry.eager {
			continue
		}
		description := []rune(strings.Join(strings.Fields(entry.Description), " "))
		if len(description) > 48 {
			description = append(description[:45], []rune("...")...)
		}
		line := "\n" + entry.ID + ": " + string(description)
		if len([]rune(text+line)) > 700 {
			text += "\nUse discover_capabilities to find more available packs."
			break
		}
		text += line
	}
	return text
}

// Role availability is independent of whether a source has connected.
func (c *Capabilities) available(id string) bool {
	for _, source := range c.Sources {
		if source.ID == id {
			return !c.Subagent || !source.MainOnly
		}
	}
	return true
}

// PackageInfo is a read-only projection of a native composition declaration.
type PackageInfo struct {
	ID, Description string
	Exposure        Exposure
	Tools           []string
}
