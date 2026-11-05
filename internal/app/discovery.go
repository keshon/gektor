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
	probe, err := newInspector(d, o.URL, o.Model, connectionHTTP(o.URL, ResolvedAPIKey(o.APIKey, o.APIKeyEnv), ExtraHeadersFromEnv()))
	if err != nil {
		return nil, err
	}
	return probe.Models(ctx)
}

// RefreshModelFacts probes only before a run. Each caller applies the result to
// its own environment copy; no running client's state is mutated.
func (b *Backend) RefreshModelFacts(ctx context.Context) llm.ModelFacts {
	facts := llm.ModelFacts{}
	if b.descriptor.Discovery != providers.LocalProbe {
		return facts
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if n, err := b.MaxContextLength(ctx); err == nil && n > 0 {
		facts.ContextLimit = n
		facts.ContextKnown = true
	}
	if name, err := b.ModelName(ctx); err == nil && strings.TrimSpace(name) != "" {
		facts.Model = name
		facts.ModelKnown = true
	}
	if vision, err := b.SupportsVision(ctx); err == nil {
		facts.Vision = vision
		facts.VisionKnown = true
	}
	return facts
}

// ContextLimit preserves local reported windows over configured fallbacks.
func ContextLimit(ctx context.Context, kind, endpoint, model string, configured int, probe interface {
	MaxContextLength(context.Context) (int, error)
}) (int, error) {
	d, err := providers.Lookup(kind)
	if err != nil {
		return 0, err
	}
	if d.Discovery == providers.AccountModels {
		if configured > 0 {
			return configured, nil
		}
		return d.DefaultContext, nil
	}
	var detected int
	var probeErr error
	if d.Discovery == providers.LocalProbe {
		detected, probeErr = probe.MaxContextLength(ctx)
	}
	if probeErr != nil || d.Discovery == providers.ModelList {
		var hc *http.Client
		if b, ok := probe.(*Backend); ok {
			hc = b.httpClient
		}
		if actual := DetectKind(ctx, endpoint, hc); actual != "" && actual != d.ID {
			return 0, fmt.Errorf("-backend-kind is %q but %s is answering at %s; re-run with -backend-kind %s", kind, actual, endpoint, actual)
		}
	}
	if detected > 0 {
		return detected, nil
	}
	if configured > 0 {
		return configured, nil
	}
	if d.Discovery == providers.ModelList {
		return openaicompat.ContextLimit(model), nil
	}
	return 0, nil
}

// Inspect is the startup snapshot. Strict evaluation refuses wholly unknown local
// metadata; interactive launches retain the existing best-effort fallback.
func (b *Backend) Inspect(ctx context.Context, endpoint string, configured int, strict bool) (llm.ModelFacts, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	limit, err := ContextLimit(ctx, b.descriptor.ID, endpoint, b.model, configured, b)
	if err != nil {
		return llm.ModelFacts{}, err
	}
	f := llm.ModelFacts{ContextLimit: limit, ContextKnown: limit > 0}
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
