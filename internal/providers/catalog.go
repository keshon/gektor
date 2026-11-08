// Package providers declares integrations without importing their implementations.
package providers

import (
	"fmt"
	"net/url"
	"strings"
)

type Protocol string
type Auth string
type Discovery string
type Inspection string

const (
	LlamaInspection      Inspection = "llamacpp"
	KoboldInspection     Inspection = "koboldcpp"
	CompatibleInspection Inspection = "openai-compatible"
)

const (
	ChatCompletions Protocol  = "chat-completions"
	Responses       Protocol  = "responses"
	Bearer          Auth      = "bearer"
	ChatGPTAccount  Auth      = "chatgpt-account"
	LocalProbe      Discovery = "local-probe"
	ModelList       Discovery = "model-list"
	AccountModels   Discovery = "account-models"
)

type Descriptor struct {
	ID, Label, DefaultURL, FixedURL, Dialect string
	DefaultContext                           int
	Protocol                                 Protocol
	Auth                                     Auth
	Discovery                                Discovery
	Inspection                               Inspection
	// Sampling and output limits are route capabilities, not model guarantees.
	Sampling, OutputLimit, SelectModel bool
	Reasoning                          bool
}

var catalog = []Descriptor{
	{ID: "llama", Inspection: LlamaInspection, Dialect: "llama", Label: "llama.cpp", DefaultURL: "http://127.0.0.1:9931", Protocol: ChatCompletions, Auth: Bearer, Discovery: LocalProbe, Sampling: true, OutputLimit: true},
	{ID: "kobold", Inspection: KoboldInspection, Dialect: "kobold", Label: "KoboldCpp", DefaultURL: "http://127.0.0.1:5001", Protocol: ChatCompletions, Auth: Bearer, Discovery: LocalProbe, Sampling: true, OutputLimit: true},
	{ID: "openai", Inspection: CompatibleInspection, Dialect: "openai", Label: "OpenAI-compatible", DefaultURL: "http://127.0.0.1:8080/v1", Protocol: ChatCompletions, Auth: Bearer, Discovery: ModelList, Sampling: true, OutputLimit: true, SelectModel: true},
	{ID: "chatgpt", Reasoning: true, DefaultContext: 32768, Label: "ChatGPT", DefaultURL: "https://api.openai.com/v1", FixedURL: "https://api.openai.com/v1", Protocol: Responses, Auth: ChatGPTAccount, Discovery: AccountModels, SelectModel: true},
}

func Lookup(id string) (Descriptor, error) {
	for _, d := range catalog {
		if d.ID == id {
			return d, nil
		}
	}
	return Descriptor{}, fmt.Errorf("unsupported provider %q", id)
}
func Kinds() []string {
	out := make([]string, 0, len(catalog))
	for _, d := range catalog {
		out = append(out, d.ID)
	}
	return out
}
func (d Descriptor) ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("provider URL must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	if d.FixedURL != "" && strings.TrimSuffix(endpoint, "/") != d.FixedURL {
		return fmt.Errorf("%s requires %s", d.Label, d.FixedURL)
	}
	return nil
}

// SuggestedURL only replaces a recognized default. Custom endpoints survive.
func SuggestedURL(endpoint, previous, next string) string {
	n, err := Lookup(next)
	if err != nil {
		return endpoint
	}
	p, err := Lookup(previous)
	if err != nil {
		return endpoint
	}
	if n.FixedURL != "" {
		return n.FixedURL
	}
	if p.FixedURL != "" && strings.TrimSuffix(endpoint, "/") == p.FixedURL {
		return n.DefaultURL
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	old, _ := url.Parse(p.DefaultURL)
	target, _ := url.Parse(n.DefaultURL)
	host := u.Hostname()
	path := strings.TrimSuffix(u.Path, "/")
	if u.Scheme != "http" || (host != "localhost" && host != "127.0.0.1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != old.Port() || (path != "" && path != old.Path) {
		return endpoint
	}
	u.Host = host + ":" + target.Port()
	u.Path = target.Path
	return u.String()
}

func Default() Descriptor { return catalog[0] }

// ReasoningOptions comes from the selected account's live model catalog.
type ReasoningOptions struct {
	Efforts       []string
	DefaultEffort string
	Summary       bool
}

func NormalizeReasoningEffort(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "default" {
		return ""
	}
	return value
}
