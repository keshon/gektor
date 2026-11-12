package extension

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// Capability owns a launch's adapter. Activation belongs to each session.
// Load may discover tools, but must never execute a requested tool action.
type Capability struct {
	// MainOnly excludes this source from child discovery, activation and restore.
	Eager                                          bool
	MainOnly                                       bool
	Close                                          func()
	ID, Version, Description, Instructions, Source string
	ReadOnly, Concurrent                           bool
	ResourceField                                  string
	ToolPolicies                                   map[string]ToolPolicy
	Load                                           func(context.Context, func(string)) ([]agent.Tool, error)

	mu        sync.Mutex // guards tools and lastError; serializes adapter load and close
	tools     []agent.Tool
	lastError string
}

func (c *Capability) load(ctx context.Context, note func(string)) (result []agent.Tool, loadErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		c.lastError = ""
		if loadErr != nil {
			c.lastError = loadErr.Error()
		}
	}()

	err := ctx.Err()
	if err != nil {
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

	// Only successful discovery needs cleanup when the returned catalog is rejected.
	defer func() {
		if loadErr != nil && c.Close != nil {
			c.Close()
		}
	}()

	if len(list) == 0 {
		return nil, fmt.Errorf("capability %s returned no tools", c.ID)
	}

	check := agent.NewRegistry(list...)
	err = check.Err()
	if err != nil {
		return nil, err
	}

	for name := range c.ToolPolicies {
		if !check.Has(name) {
			return nil, fmt.Errorf("capability %s policy names unknown tool %s", c.ID, name)
		}
	}

	for _, tool := range list {
		err = agent.ValidateToolSchema(tool.Schema())
		if err != nil {
			return nil, fmt.Errorf("capability %s tool %s schema: %w", c.ID, tool.Name(), err)
		}
	}

	c.tools = list
	return list, nil
}

func capabilityState(c *Capability, list []agent.Tool) conversation.Capability {
	defs := agent.NewRegistry(list...).Defs()
	return conversation.Capability{ID: c.ID, Version: c.Version, Fingerprint: catalogFingerprint(c, defs), Tools: defs, Instructions: c.Instructions}
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
	Subagent       bool
	Registry       *agent.Registry
	Sources        []*Capability
	Native         []*NativePackage
	ForbiddenTools []string
	mu             sync.Mutex
	active         map[string]conversation.Capability
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
	if err := c.Validate(); err != nil {
		return "", err
	}
	if err := validateCapabilityID(id); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !c.available(id) {
		return "", fmt.Errorf("capability %s is unavailable to this role", id)
	}
	if state, ok := c.active[id]; ok {
		return activationText(state), nil
	}
	if p := c.native(id); p != nil {
		state := p.state()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := c.publish(ctx, state, p.Tools); err != nil {
			return "", err
		}
		return activationText(state), nil
	}
	source := c.source(id)
	list, err := source.load(ctx, note)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state := capabilityState(source, list)
	if err := c.install(ctx, source, state); err != nil {
		return "", err
	}
	return activationText(state), nil
}

func activationText(state conversation.Capability) string {
	return agent.CapabilityCatalog(state)
}

func (c *Capabilities) install(ctx context.Context, source *Capability, state conversation.Capability) error {
	list := make([]agent.Tool, 0, len(state.Tools))
	for _, def := range state.Tools {
		list = append(list, &capabilityTool{source: source, state: state, def: def})
	}
	return c.publish(ctx, state, list)
}

func (c *Capabilities) Save() []conversation.Capability {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]conversation.Capability, 0, len(c.active))
	for _, state := range c.active {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (c *Capabilities) Restore(ctx context.Context, states []conversation.Capability) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Validate(); err != nil {
		return err
	}
	for _, state := range states {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !c.available(state.ID) {
			return fmt.Errorf("capability %s is unavailable to this role", state.ID)
		}
		if active, ok := c.active[state.ID]; ok {
			if !sameCapability(active, state) {
				return fmt.Errorf("capability %s conflicts with saved activation", state.ID)
			}
			continue
		}
		if err := validateCapabilityID(state.ID); err != nil {
			return err
		}
		if len(state.Tools) == 0 || len(state.Tools) > 4096 {
			return fmt.Errorf("invalid saved capability %s catalog", state.ID)
		}
		if p := c.native(state.ID); p != nil {
			if !sameCapability(p.state(), state) {
				return fmt.Errorf("native capability %s changed; use compatible code or start a new session", state.ID)
			}
			if err := c.publish(ctx, state, p.Tools); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(state.ID, "native_") {
			return fmt.Errorf("native capability %s is unavailable to this role or build", state.ID)
		}
		if err := c.install(ctx, c.source(state.ID), state); err != nil {
			return err
		}
	}
	return nil
}

type capabilityTool struct {
	source *Capability
	state  conversation.Capability
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
	if err := agent.RejectUnknownFields(args, "query"); err != nil {
		return "", err
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	result := []capabilityEntry{}
	for _, entry := range t.Catalog.entries() {
		match := true
		for _, term := range strings.Fields(strings.ToLower(in.Query)) {
			if !strings.Contains(strings.ToLower(entry.ID+" "+entry.Description), term) {
				match = false
				break
			}
		}
		if match {
			result = append(result, entry)
		}
	}
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

// ReadOnlyCall admits only native activation in Plan. Native instances are
// already constructed; this only publishes schemas. Tool execution still
// passes through the normal Plan allowlist and permission gate.
func (t EnableCapability) ReadOnlyCall(args json.RawMessage) bool {
	var in struct {
		ID string `json:"id"`
	}
	return json.Unmarshal(args, &in) == nil && t.Catalog.native(in.ID) != nil && t.Catalog.available(in.ID)
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
	if err := agent.RejectUnknownFields(args, "id"); err != nil {
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

// Connect discovers and caches the catalog without executing remote tools.
func (c *Capability) Connect(ctx context.Context) (int, error) {
	list, e := c.load(ctx, nil)
	return len(list), e
}
func (c *Capability) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Close != nil {
		c.Close()
	}
	c.tools = nil
	c.lastError = ""
}
func (c *Capability) Connected() (bool, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools != nil, len(c.tools)
}

// ConnectionStatus never waits for a discovery operation to finish.
func (c *Capability) ConnectionStatus() (string, int, string) {
	if !c.mu.TryLock() {
		return "connecting", 0, ""
	}
	defer c.mu.Unlock()
	if c.tools != nil {
		return "connected", len(c.tools), ""
	}
	if c.lastError != "" {
		return "failed", 0, c.lastError
	}
	return "disconnected", 0, ""
}

func (c *Capability) ToolNames() []string {
	if !c.mu.TryLock() {
		return nil
	}
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.tools))
	for _, tool := range c.tools {
		names = append(names, tool.Name())
	}
	sort.Strings(names)
	return names
}
