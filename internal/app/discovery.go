package app

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/providers/openai"
	"github.com/keshon/gektor/internal/providers/openaicompat"
)

func DiscoverModels(ctx context.Context, o BackendOptions) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d, err := providers.Lookup(o.Kind)
	if err != nil {
		return nil, err
	}
	if err = d.ValidateEndpoint(o.URL); err != nil {
		return nil, err
	}
	if d.Discovery == providers.AccountModels {
		m := o.Accounts
		if m == nil {
			m = openai.Default()
		}
		bound, err := m.Bind(ctx)
		if err != nil {
			return nil, err
		}
		return bound.Models(ctx)
	}
	key, err := backendAPIKey(d, o)
	if err != nil {
		return nil, err
	}
	probe, err := newInspector(d, o.URL, o.Model, connectionHTTP(o.URL, key, ExtraHeadersFromEnv()))
	if err != nil {
		return nil, err
	}
	return probe.Models(ctx)
}

// RefreshModelFacts probes only before a run. Each caller applies the result to
// its own environment copy; no running client's state is mutated.
func (b *Backend) RefreshModelFacts(ctx context.Context) llm.ModelFacts {
	facts := llm.ModelFacts{}
	local := b.descriptor.Discovery == providers.LocalProbe
	if !local && b.descriptor.Discovery != providers.AccountModels {
		return facts
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if local {
		limit, err := b.MaxContextLength(ctx)
		if err == nil && limit > 0 {
			facts.ContextLimit = contextBudget(b.contextCap, limit)
			facts.ContextWindow = limit
			facts.ContextKnown = true
		}
		name, err := b.ModelName(ctx)
		if err == nil && strings.TrimSpace(name) != "" {
			facts.Model = name
			facts.ModelKnown = true
		}
	}
	vision, err := b.SupportsVision(ctx)
	if err == nil {
		facts.Vision = vision
		facts.VisionKnown = true
	}
	return facts
}

func contextBudget(cap, window int) int {
	if cap > 0 && (window <= 0 || cap < window) {
		return cap
	}
	return window
}

// ResolveContext keeps the provider window separate from the effective working budget.
func ResolveContext(ctx context.Context, kind, endpoint, model string, configured int, probe interface {
	MaxContextLength(context.Context) (int, error)
}) (llm.ModelFacts, error) {
	d, err := providers.Lookup(kind)
	if err != nil {
		return llm.ModelFacts{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var reported int
	var probeErr error
	if d.Discovery == providers.LocalProbe || d.Discovery == providers.ModelList {
		reported, probeErr = probe.MaxContextLength(ctx)
	}
	if (d.Discovery == providers.LocalProbe && probeErr != nil) || (d.ID == "openai" && reported <= 0) {
		var hc *http.Client
		if b, ok := probe.(*Backend); ok {
			hc = b.httpClient
		}
		if actual := DetectKind(ctx, endpoint, hc); actual != "" && actual != d.ID {
			return llm.ModelFacts{}, fmt.Errorf("-backend-kind is %q but %s is answering at %s; re-run with -backend-kind %s", kind, actual, endpoint, actual)
		}
	}

	limit := contextBudget(configured, max(0, reported))
	facts := llm.ModelFacts{ContextLimit: limit, ContextWindow: max(0, reported), ContextKnown: true}
	if limit > 0 {
		return facts, nil
	}

	facts.ContextEstimated = true
	switch d.Discovery {
	case providers.AccountModels:
		facts.ContextLimit = d.DefaultContext
	case providers.ModelList:
		facts.ContextLimit = openaicompat.ContextLimit(model)
	default:
		facts.ContextLimit = 8192
	}
	return facts, nil
}

// Inspect is the startup snapshot. Strict evaluation refuses wholly unknown local
// metadata; interactive launches retain the existing best-effort fallback.
func (b *Backend) Inspect(ctx context.Context, endpoint string, configured int, strict bool) (llm.ModelFacts, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, err := ResolveContext(ctx, b.descriptor.ID, endpoint, b.model, configured, b)
	if err != nil {
		return llm.ModelFacts{}, err
	}
	if name, e := b.ModelName(ctx); e == nil && strings.TrimSpace(name) != "" {
		f.Model = name
		f.ModelKnown = true
	}
	if vision, e := b.SupportsVision(ctx); e == nil {
		f.Vision = vision
		f.VisionKnown = true
	}
	if strict && b.descriptor.Discovery == providers.LocalProbe && !f.ModelKnown {
		// A configured budget is useful but is not evidence that a backend answered.
		if actual, e := b.MaxContextLength(ctx); e != nil || actual <= 0 {
			return f, fmt.Errorf("backend metadata probes failed at %s; no baseline was run", endpoint)
		}
	}
	return f, nil
}

func DiscoverReasoning(ctx context.Context, o BackendOptions) (providers.ReasoningOptions, error) {
	d, err := providers.Lookup(o.Kind)
	if err != nil {
		return providers.ReasoningOptions{}, err
	}
	if !d.Reasoning {
		return providers.ReasoningOptions{}, fmt.Errorf("provider does not support reasoning controls")
	}
	if err = d.ValidateEndpoint(o.URL); err != nil {
		return providers.ReasoningOptions{}, err
	}
	manager := o.Accounts
	if manager == nil {
		manager = openai.Default()
	}
	bound, err := manager.Bind(ctx)
	if err != nil {
		return providers.ReasoningOptions{}, err
	}
	return boundReasoningOptions(ctx, bound, o.Model)
}
func boundReasoningOptions(ctx context.Context, bound *openai.BoundAccount, model string) (providers.ReasoningOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	catalog, err := bound.ModelCatalog(ctx)
	if err != nil {
		return providers.ReasoningOptions{}, err
	}
	for _, m := range catalog {
		if m.Slug != model {
			continue
		}
		out := providers.ReasoningOptions{DefaultEffort: m.DefaultReasoningLevel, Summary: m.SupportsReasoningSummaries && m.SupportsReasoningSummaryParameter}
		for _, level := range m.SupportedReasoningLevels {
			if level.Effort != "" {
				out.Efforts = append(out.Efforts, level.Effort)
			}
		}
		return out, nil
	}
	return providers.ReasoningOptions{}, fmt.Errorf("model %q is absent from the selected account's catalog", model)
}
func validateReasoning(options providers.ReasoningOptions, effort string, summary bool) error {
	if effort != "" && !slices.Contains(options.Efforts, effort) {
		return fmt.Errorf("reasoning effort %q is not supported by this model; available: %s", effort, strings.Join(options.Efforts, ", "))
	}
	if summary && !options.Summary {
		return fmt.Errorf("reasoning summaries are not supported by this model")
	}
	return nil
}
