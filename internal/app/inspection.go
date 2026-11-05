package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/providers/koboldcpp"
	"github.com/keshon/gektor/internal/providers/llamacpp"
	"github.com/keshon/gektor/internal/providers/openaicompat"
)

// modelInspector is independent of the inference client and request dialect.
type modelInspector interface {
	MaxContextLength(context.Context) (int, error)
	ModelName(context.Context) (string, error)
	SupportsVision(context.Context) (bool, error)
	Models(context.Context) ([]string, error)
}
type inspectorFactory func(string, string, *http.Client) modelInspector

var inspectors = map[providers.Inspection]inspectorFactory{
	providers.LlamaInspection:      func(base, model string, hc *http.Client) modelInspector { return llamacpp.New(base, model, hc) },
	providers.KoboldInspection:     func(base, model string, hc *http.Client) modelInspector { return koboldcpp.New(base, model, hc) },
	providers.CompatibleInspection: func(base, model string, hc *http.Client) modelInspector { return openaicompat.New(base, model, hc) },
}

func newInspector(d providers.Descriptor, base, model string, hc *http.Client) (modelInspector, error) {
	factory, ok := inspectors[d.Inspection]
	if !ok {
		return nil, fmt.Errorf("unsupported inspection strategy %q", d.Inspection)
	}
	return factory(base, model, hc), nil
}

// DetectKind identifies local backends from their inspection endpoints.
// An unavailable backend stays unknown instead of suggesting the wrong provider.
func DetectKind(ctx context.Context, base string, clients ...*http.Client) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hc := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		hc = clients[0]
	}
	for _, kind := range providers.Kinds() {
		d, _ := providers.Lookup(kind)
		if d.Discovery != providers.LocalProbe {
			continue
		}
		probe, err := newInspector(d, base, "", hc)
		if err != nil {
			continue
		}
		if n, err := probe.MaxContextLength(ctx); err == nil && n > 0 {
			return kind
		}
	}
	return ""
}
