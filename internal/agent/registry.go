package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/keshon/gektor/internal/llm"
)

// Registry holds tools looked up by name. An exclusive tool may add capabilities.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	err   error
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	r.err = r.Add(tools...)
	return r
}

// Err reports an invalid initial catalog before any model request or tool runs.
func (r *Registry) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

var toolIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ValidateToolName(name string) error {
	if !toolIdentifier.MatchString(name) {
		return fmt.Errorf("invalid tool name %q: use 1–64 ASCII letters, digits, underscores or hyphens", name)
	}
	return nil
}

// Names returns this registry's tool names, sorted. What an agent may do
// is worth asserting on rather than assuming — see internal/roles, where
// a hand-assembled agent silently ended up with a smaller tool set than
// the one that ships.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Defs returns the tool definitions to hand to the LLM client.
func (r *Registry) Defs() []llm.ToolDef {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]llm.ToolDef, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		})
	}
	return defs
}

// Add installs a complete batch atomically. Existing names cannot be replaced.
func (r *Registry) Add(tools ...Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	seen := make(map[string]bool, len(tools))
	for _, t := range tools {
		if t == nil {
			return fmt.Errorf("nil tool")
		}
		if err := ValidateToolName(t.Name()); err != nil {
			return err
		}
		if _, exists := r.tools[t.Name()]; exists || seen[t.Name()] {
			return fmt.Errorf("tool %q already registered", t.Name())
		}
		seen[t.Name()] = true
	}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return nil
}

// Remove withdraws a toolset's schemas between turns without closing its resources.
func (r *Registry) Remove(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		delete(r.tools, name)
	}
}

func (r *Registry) lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Run(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return r.RunStreaming(ctx, name, args, nil)
}

func (r *Registry) ValidateCall(name string, args json.RawMessage) error {
	if err := r.Err(); err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("unknown tool %q", name)
	}
	t, ok := r.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	return validateArguments(args, t.Schema())
}

// RunStreaming observes display-only output when the tool supports it.
// The returned result remains authoritative for model history.
func (r *Registry) RunStreaming(ctx context.Context, name string, args json.RawMessage, onOutput func(string)) (string, error) {
	if err := r.Err(); err != nil {
		return "", err
	}
	t, ok := r.lookup(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if err := validateArguments(args, t.Schema()); err != nil {
		return "", err
	}
	if streaming, ok := t.(interface {
		RunStreaming(context.Context, json.RawMessage, func(string)) (string, error)
	}); ok && onOutput != nil {
		return streaming.RunStreaming(ctx, args, onOutput)
	}
	return t.Run(ctx, args)
}

// ModeOf reports a tool's concurrency mode. An unknown name (shouldn't
// happen — the model can only call tools it was offered) is treated as
// Exclusive: the safe default when in doubt.
func (r *Registry) ModeOf(name string) ToolMode {
	if t, ok := r.lookup(name); ok {
		return t.Mode()
	}
	return Exclusive
}

// AttacherOf returns the tool's picture-returning half, or nil. Like
// other optional capabilities, this is opt-in.
func (r *Registry) AttacherOf(name string) Attacher {
	t, ok := r.lookup(name)
	if !ok {
		return nil
	}
	a, _ := t.(Attacher)
	return a
}

// Has reports whether a tool is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.lookup(name)
	return ok
}

// HasAny reports whether at least one of the named tools is registered.
func (r *Registry) HasAny(names ...string) bool {
	for _, name := range names {
		if r.Has(name) {
			return true
		}
	}
	return false
}

// Checklist returns the registry's checklist tool, or nil.
func (r *Registry) Checklist() Checklister {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		if cl, ok := t.(Checklister); ok {
			return cl
		}
	}
	return nil
}

// Questioner returns the registry's human-question tool, or nil.
func (r *Registry) Questioner() Questioner {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		if q, ok := t.(Questioner); ok {
			return q
		}
	}
	return nil
}

// TodoProgress reports the checklist tool's state: done count and
// still-open item texts. Nil-receiver safe for tool-less runs.
func (r *Registry) TodoProgress() (done int, open []string) {
	if cl := r.Checklist(); cl != nil {
		return cl.Progress()
	}
	return 0, nil
}
