// Package app composes and owns profile-driven services independently of any
// frontend. Frontends borrow a composed set for the length of one launch.
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/llm/responses"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/providers/openai"
)

// PreparedProfile contains a validated launch profile and compiled identity.
type PreparedProfile struct {
	Identity  Identity
	Warnings  []string
	Profile   config.Profile
	Directory string
}

// PrepareProfile clones and validates a profile and compiles its identity.
func PrepareProfile(profile config.Profile, directory string) (PreparedProfile, error) {
	profile = profile.Clone()
	out := PreparedProfile{Profile: profile, Directory: directory}
	if err := profile.Validate(); err != nil {
		return out, err
	}

	id := identityFrom(profile)
	identity, warnings, err := CompileIdentity(IdentityOptions{
		Preset:       id.PromptPreset,
		Name:         id.Name,
		Persona:      id.UserPersona,
		User:         id.UserName,
		System:       id.SystemPrompt,
		Post:         id.PostHistory,
		MaxTokens:    profile.Generation.MaxTokens,
		ContextLimit: profile.Generation.ContextLimit,
		Temperature:  profile.Generation.Temperature,
	})
	if err != nil {
		return out, err
	}

	out.Identity = identity
	out.Warnings = warnings

	return out, nil
}

// BackendOptions supplies the selected route and its launch-scoped settings.
type BackendOptions struct {
	Kind, URL, Model, APIKey, APIKeyEnv string
	ReasoningEffort                     string
	ReasoningSummary                    bool
	NoGrammar, DRY                      bool
	Debug                               io.Writer
	Accounts                            *openai.Manager
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

// NewBackend validates the provider route and composes its client and inspector.
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

	b := &Backend{
		descriptor:  d,
		model:       o.Model,
		backendName: d.ID,
	}

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
