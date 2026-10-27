package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

// Capability owns a launch's adapter. Activation belongs to each session.
// Load may discover tools, but must never execute a requested tool action.
type Capability struct {
	Close                                          func()
	ID, Version, Description, Instructions, Source string
	ReadOnly, Concurrent                           bool
	ResourceField                                  string
	ToolPolicies                                   map[string]ToolPolicy
	Load                                           func(context.Context, func(string)) ([]agent.Tool, error)
	mu                                             sync.Mutex
	tools                                          []agent.Tool
}

func (c *Capability) load(ctx context.Context, note func(string)) ([]agent.Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tools != nil {
		return c.tools, nil
	}
	if c.Load == nil {
		return nil, fmt.Errorf("capability %q is not configured; configure it and retry activation", c.ID)
	}
	list, err := c.Load(ctx, note)
	if err != nil {
		return nil, fmt.Errorf("capability %s: %w", c.ID, err)
	}
	if len(list) == 0 {
		if c.Close != nil {
			c.Close()
		}
		return nil, fmt.Errorf("capability %s returned no tools", c.ID)
	}
	check := agent.NewRegistry(list...)
	if err := check.Err(); err != nil {
		if c.Close != nil {
			c.Close()
		}
		return nil, err
	}
	for name := range c.ToolPolicies {
		if !check.Has(name) {
			if c.Close != nil {
				c.Close()
			}
			return nil, fmt.Errorf("capability %s policy names unknown tool %s", c.ID, name)
		}
	}
	for _, tool := range list {
		if err := agent.ValidateToolSchema(tool.Schema()); err != nil {
			if c.Close != nil {
				c.Close()
			}
			return nil, fmt.Errorf("capability %s tool %s schema: %w", c.ID, tool.Name(), err)
		}
	}
	c.tools = list
	return list, nil
}

func capabilityState(c *Capability, list []agent.Tool) session.Capability {
	defs := agent.NewRegistry(list...).Defs()
	return session.Capability{ID: c.ID, Version: c.Version, Fingerprint: catalogFingerprint(c, defs), Tools: defs, Instructions: c.Instructions}
}

func catalogFingerprint(c *Capability, defs []llm.ToolDef) string {
	data, _ := json.Marshal(struct {
		Source, Version, Instructions, Resource string
		ReadOnly, Concurrent                    bool
		Tools                                   []llm.ToolDef
		Policies                                map[string]ToolPolicy `json:"policies,omitempty"`
	}{c.Source, c.Version, c.Instructions, c.ResourceField, c.ReadOnly, c.Concurrent, defs, c.ToolPolicies})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// Capabilities is one session's catalog. Saved schemas reconnect only on use.
type Capabilities struct {
	Registry *agent.Registry
	Sources  []*Capability
	mu       sync.Mutex
	active   map[string]session.Capability
}

func (c *Capabilities) source(id string) *Capability {
	for _, source := range c.Sources {
		if source.ID == id {
			return source
		}
	}
	return &Capability{ID: id}
}

func (c *Capabilities) Enable(ctx context.Context, id string, note func(string)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := validateCapabilityID(id); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if state, ok := c.active[id]; ok {
		return activationText(state), nil
	}
	source := c.source(id)
	list, err := source.load(ctx, note)
	if err != nil {
		return "", err
	}
	state := capabilityState(source, list)
	if err := c.install(source, state); err != nil {
		return "", err
	}
	return activationText(state), nil
}

func activationText(state session.Capability) string {
	names := make([]string, 0, len(state.Tools))
	for _, def := range state.Tools {
		names = append(names, def.Name)
	}
	return fmt.Sprintf("Capability %s enabled for this session. Tools: %s.\n%s", state.ID, strings.Join(names, ", "), state.Instructions)
}

func (c *Capabilities) install(source *Capability, state session.Capability) error {
	list := make([]agent.Tool, 0, len(state.Tools))
	for _, def := range state.Tools {
		list = append(list, &capabilityTool{source: source, state: state, def: def})
	}
	if err := c.Registry.Add(list...); err != nil {
		return err
	}
	if c.active == nil {
		c.active = map[string]session.Capability{}
	}
	c.active[state.ID] = state
	return nil
}

func (c *Capabilities) Save() []session.Capability {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]session.Capability, 0, len(c.active))
	for _, state := range c.active {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (c *Capabilities) Restore(ctx context.Context, states []session.Capability) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, state := range states {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, ok := c.active[state.ID]; ok {
			continue
		}
		if err := validateCapabilityID(state.ID); err != nil {
			return err
		}
		if len(state.Tools) == 0 || len(state.Tools) > 4096 {
			return fmt.Errorf("invalid saved capability %s catalog", state.ID)
		}
		if err := c.install(c.source(state.ID), state); err != nil {
			return err
		}
	}
	return nil
}

// RestoreArchive migrates transcripts saved before catalogs were persisted.
func (c *Capabilities) RestoreArchive(ctx context.Context, archive []llm.Message) error {
	calls := map[string]string{}
	var failures []string
	for _, m := range archive {
		if m.Role == llm.RoleAssistant {
			for _, call := range m.ToolCalls {
				if call.InputError != "" {
					continue
				}
				if call.Name == "enable_browser" || strings.HasPrefix(call.Name, "mcp__browser__browser_") {
					calls[call.ID] = "browser"
				}
				if call.Name == "enable_capability" {
					var in struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(call.Arguments, &in) == nil {
						calls[call.ID] = in.ID
					}
				}
			}
		}
		if id := calls[m.ToolCallID]; m.Role == llm.RoleTool && id != "" && m.ToolFailed != nil && !*m.ToolFailed {
			delete(calls, m.ToolCallID)
			if _, err := c.Enable(ctx, id, nil); err != nil {
				failures = append(failures, err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

type capabilityTool struct {
	source *Capability
	state  session.Capability
	def    llm.ToolDef
}

func (t *capabilityTool) Name() string            { return t.def.Name }
func (t *capabilityTool) Description() string     { return t.def.Description }
func (t *capabilityTool) Schema() json.RawMessage { return t.def.Parameters }
func (t *capabilityTool) Mode() agent.ToolMode {
	policy := t.source.ToolPolicies[t.Name()]
	concurrent := t.source.Concurrent
	if policy.Concurrent != nil {
		concurrent = *policy.Concurrent
	}
	if t.ReadOnly() && concurrent {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func (t *capabilityTool) ReadOnly() bool {
	if value := t.source.ToolPolicies[t.Name()].ReadOnly; value != nil {
		return *value
	}
	return t.source.ReadOnly
}
func (t *capabilityTool) Resources(args json.RawMessage) ([]string, error) {
	fields := t.source.ToolPolicies[t.Name()].ResourceFields
	if fields == nil && t.source.ResourceField != "" {
		fields = []string{t.source.ResourceField}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	var in map[string]json.RawMessage
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	resources := make([]string, 0, len(fields))
	for _, field := range fields {
		var value string
		if err := json.Unmarshal(in[field], &value); err != nil || value == "" {
			return nil, fmt.Errorf("capability resource %q must be a nonempty string", field)
		}
		resources = append(resources, value)
	}
	return resources, nil
}
func (t *capabilityTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}
func (t *capabilityTool) RunStreaming(ctx context.Context, args json.RawMessage, output func(string)) (string, error) {
	list, err := t.source.load(ctx, output)
	if err != nil {
		return "", err
	}
	if catalogFingerprint(t.source, agent.NewRegistry(list...).Defs()) != t.state.Fingerprint {
		return "", fmt.Errorf("capability %s configuration or catalog changed; start a new session to use its current definitions", t.source.ID)
	}
	for _, tool := range list {
		if tool.Name() != t.Name() {
			continue
		}
		if stream, ok := tool.(interface {
			RunStreaming(context.Context, json.RawMessage, func(string)) (string, error)
		}); ok && output != nil {
			return stream.RunStreaming(ctx, args, output)
		}
		return tool.Run(ctx, args)
	}
	return "", fmt.Errorf("capability %s no longer provides %s", t.source.ID, t.Name())
}
func (t *capabilityTool) Attach(args json.RawMessage) ([]string, error) {
	t.source.mu.Lock()
	defer t.source.mu.Unlock()
	for _, tool := range t.source.tools {
		if tool.Name() == t.Name() {
			if attacher, ok := tool.(agent.Attacher); ok {
				return attacher.Attach(args)
			}
		}
	}
	return nil, nil
}

type DiscoverCapabilities struct{ Catalog *Capabilities }

func (DiscoverCapabilities) ReadOnly() bool { return true }

func (DiscoverCapabilities) Name() string         { return "discover_capabilities" }
func (DiscoverCapabilities) Mode() agent.ToolMode { return agent.Concurrent }
func (DiscoverCapabilities) Description() string {
	return "Find optional capability packs by ID or description. Returns metadata only; enable a matching ID with enable_capability. Empty query lists all packs. Does not start adapters."
}
func (DiscoverCapabilities) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"additionalProperties":false}`)
}
func (t DiscoverCapabilities) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := rejectUnknownFields(args, "query"); err != nil {
		return "", err
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	type entry struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	result := []entry{}
	for _, source := range t.Catalog.Sources {
		if strings.Contains(strings.ToLower(source.ID+" "+source.Description), strings.ToLower(in.Query)) {
			result = append(result, entry{source.ID, source.Description})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	data, err := json.Marshal(result)
	return string(data), err
}

type EnableCapability struct{ Catalog *Capabilities }

func (EnableCapability) Resources(args json.RawMessage) ([]string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return []string{in.ID}, nil
}

func (EnableCapability) Name() string         { return "enable_capability" }
func (EnableCapability) Mode() agent.ToolMode { return agent.Exclusive }
func (EnableCapability) Description() string {
	return "Enable an optional capability pack for this session by its discovered ID. Loads tools and brief instructions on demand. Does not run a tool action."
}
func (EnableCapability) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`)
}
func (t EnableCapability) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}
func (t EnableCapability) RunStreaming(ctx context.Context, args json.RawMessage, output func(string)) (string, error) {
	if err := rejectUnknownFields(args, "id"); err != nil {
		return "", err
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.Catalog.Enable(ctx, in.ID, output)
}
