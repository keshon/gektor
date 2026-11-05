package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/llm/responses"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/providers/openai"
)

type BackendOptions struct {
	Kind, URL, Model, APIKey, APIKeyEnv string
	ReasoningEffort                     string
	ReasoningSummary                    bool
	NoGrammar, DRY                      bool
	Debug                               io.Writer
	Accounts                            *openai.Manager
}

type usageClient interface {
	llm.Client
	llm.Streamer
	UsageTotals() (int, int, int)
	CachedTotal() int
}

// Backend is a composed connection. Only app knows the concrete protocol client.
// Probe and client closures are replaced together on every settings relaunch.
type Backend struct {
	usageClient
	AccountLabel string
	descriptor   providers.Descriptor
	httpClient   *http.Client
	model        string
	backendName  string
	inspector    modelInspector
	visionProbe  func(context.Context) (bool, error)
	setDebug     func(io.Writer)
	setDRY       func(bool)
}

func NewBackend(ctx context.Context, o BackendOptions) (*Backend, error) {
	o.ReasoningEffort = providers.NormalizeReasoningEffort(o.ReasoningEffort)
	d, err := providers.Lookup(o.Kind)
	if err != nil {
		return nil, err
	}
	if err = d.ValidateEndpoint(o.URL); err != nil {
		return nil, err
	}
	if (o.ReasoningEffort != "" || o.ReasoningSummary) && !d.Reasoning {
		return nil, fmt.Errorf("%s does not support reasoning controls", d.Label)
	}
	b := &Backend{descriptor: d, model: o.Model, backendName: d.ID}
	switch d.Protocol {
	case providers.Responses:
		manager := o.Accounts
		if manager == nil {
			manager = openai.Default()
		}
		bound, err := manager.Bind(ctx)
		if err != nil {
			return nil, err
		}
		if o.ReasoningEffort != "" || o.ReasoningSummary {
			options, err := boundReasoningOptions(ctx, bound, o.Model)
			if err != nil {
				return nil, err
			}
			if err = validateReasoning(options, o.ReasoningEffort, o.ReasoningSummary); err != nil {
				return nil, err
			}
		}
		c := responses.New(o.URL, o.Model, bound.Scope(), bound.Token)
		c.ConfigureReasoning(o.ReasoningEffort, o.ReasoningSummary)
		b.usageClient = c
		b.AccountLabel = bound.Label
		// Replayed image history must not fetch the account catalog on every step.
		var once sync.Once
		var vision bool
		var visionErr error
		b.visionProbe = func(ctx context.Context) (bool, error) {
			once.Do(func() { vision, visionErr = bound.SupportsVision(ctx, o.Model) })
			return vision, visionErr
		}
	case providers.ChatCompletions:
		c, err := chatcompletions.ClientFor(d.Dialect, o.URL, o.Model)
		if err != nil {
			return nil, err
		}
		key := ResolvedAPIKey(o.APIKey, o.APIKeyEnv)
		c.WithAPIKey(key)
		b.httpClient = connectionHTTP(o.URL, key, ExtraHeadersFromEnv())
		c.WithHTTPClient(b.httpClient)
		c.ExtraHeaders = ExtraHeadersFromEnv()
		c.NoGrammar, c.DRY, c.Debug = o.NoGrammar, o.DRY, o.Debug
		b.usageClient = c
		b.backendName = c.Backend()
		b.inspector, err = newInspector(d, o.URL, o.Model, b.httpClient)
		if err != nil {
			return nil, err
		}
		b.setDebug = func(w io.Writer) { c.Debug = w }
		b.setDRY = func(v bool) { c.DRY = v }
	default:
		return nil, fmt.Errorf("unsupported protocol %q", d.Protocol)
	}
	return b, nil
}
func (b *Backend) Backend() string { return b.backendName }
func (b *Backend) SetDebug(w io.Writer) {
	if b.setDebug != nil {
		b.setDebug(w)
	}
}
func (b *Backend) SetDRY(v bool) {
	if b.setDRY != nil {
		b.setDRY(v)
	}
}
func (b *Backend) MaxContextLength(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if b.inspector == nil {
		return 0, fmt.Errorf("provider does not report context size")
	}
	return b.inspector.MaxContextLength(ctx)
}
func (b *Backend) ModelName(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if b.descriptor.SelectModel {
		return b.model, nil
	}
	if b.inspector == nil {
		return "", fmt.Errorf("model name is unknown")
	}
	return b.inspector.ModelName(ctx)
}
func (b *Backend) SupportsVision(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if b.visionProbe != nil {
		return b.visionProbe(ctx)
	}
	if b.inspector == nil {
		return false, fmt.Errorf("model vision support is unknown")
	}
	return b.inspector.SupportsVision(ctx)
}

func (b *Backend) checkImages(ctx context.Context, req llm.ChatRequest) error {
	for _, message := range req.Messages {
		if len(message.Images) > 0 {
			vision, err := b.SupportsVision(ctx)
			if err == nil && !vision {
				return fmt.Errorf("model %s does not support image input; select a vision model", b.model)
			}
			return nil
		}
	}
	return nil
}
func (b *Backend) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if err := b.checkImages(ctx, req); err != nil {
		return llm.ChatResponse{}, err
	}
	return b.usageClient.Chat(ctx, req)
}
func (b *Backend) Stream(ctx context.Context, req llm.ChatRequest, reasoning, text func(string)) (llm.ChatResponse, error) {
	if err := b.checkImages(ctx, req); err != nil {
		return llm.ChatResponse{}, err
	}
	return b.usageClient.Stream(ctx, req, reasoning, text)
}
